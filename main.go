// PDC = Phasor Data Concentrator.
//
// What this program does (in order):
//  1. Connect to each PMU (power-grid sensor)
//  2. Learn channel layout from the live CFG-2 handshake
//  3. Parse DATA frames into numbers we understand
//  4. Quality-check those numbers (is the clock crazy? etc.)
//  5. Time-align frames that share the same timestamp
//  6. Show everything on the live dashboard
//
// Layout (CFG-2) always comes from the PMU connection — never from disk files.
// Saving readings to Redis/Postgres stays parked in output/ (kept, not wired yet).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pdc/aligner"
	"pdc/api"
	"pdc/config"
	"pdc/internal/instance"
	"pdc/manager"
	"pdc/monitoring"
	"pdc/output"
	"pdc/parser"
	"pdc/store"
)

// pipeline is the in-memory path from "raw bytes" → "dashboard".
type pipeline struct {
	checker             *aligner.Checker   // quality gate
	buffers             *aligner.Bank      // per-PMU timestamp tables
	publisher           *aligner.Publisher // tick-grid align emit
	dropQualityRejected bool
	traceMu             sync.Mutex
	traceCounts         map[string]int // print first few frames in detail
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func validatePMUPorts(pmus []config.PMUConfig) error {
	byPort := make(map[int]string, len(pmus))
	for _, p := range pmus {
		if p.Port <= 0 {
			continue
		}
		if other, ok := byPort[p.Port]; ok {
			return fmt.Errorf("duplicate TCP port %d: %s and %s (each PMU needs its own device port)", p.Port, p.Name, other)
		}
		byPort[p.Port] = p.Name
	}
	return nil
}

func newPipeline() *pipeline {
	maxClockSkew := envDuration("MAX_CLOCK_SKEW", 24*time.Hour)
	dropQualityRejected := envBool("DROP_QUALITY_REJECTED", false)
	log.Printf("quality gate: max_clock_skew=%s drop_rejected=%t", maxClockSkew, dropQualityRejected)

	// Keep a short in-memory tape of recent frames (for dump-frames tool).
	output.ConfigureFrameCapture(envInt("FRAME_CAPTURE_SIZE", 1500))

	cap := envInt("ALIGN_BUFFER_CAPACITY", aligner.DefaultCapacity)
	periodMs := int64(envInt("ALIGN_PERIOD_MS", int(aligner.DefaultPeriodMs)))
	buf := aligner.NewBank(cap)
	pub := aligner.NewPublisher(buf, periodMs, 0, monitoring.RecordAlignedFrame)
	buf.OnReset(func() {
		monitoring.ClearAlignedBatches()
	})
	p := &pipeline{
		checker:             aligner.NewChecker(maxClockSkew),
		buffers:             buf,
		publisher:           pub,
		dropQualityRejected: dropQualityRejected,
		traceCounts:         make(map[string]int),
	}
	log.Printf("aligner: capacity=%d period=%dms (no wait, head=MinNewest)", cap, periodMs)
	return p
}

func (p *pipeline) nextTraceIndex(pmuName string) (int, bool) {
	p.traceMu.Lock()
	defer p.traceMu.Unlock()
	count := p.traceCounts[pmuName]
	if count >= 3 {
		return 0, false
	}
	count++
	p.traceCounts[pmuName] = count
	return count, true
}

func (p *pipeline) HandleFrame(pmuName string, raw []byte, receivedAt time.Time) {
	monitoring.IncFramesReceived()
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	parseStart := time.Now()
	reading, err := parser.ParseDataFrame(pmuName, raw)
	parseDur := time.Since(parseStart)
	monitoring.ObserveStage(pmuName, monitoring.StageParse, parseDur)
	if err != nil {
		monitoring.IncParseErrors()
		monitoring.NoteFrameParseFail(pmuName, err.Error(), raw)
		log.Printf("[%s] parse error: %v", pmuName, err)
		monitoring.RecordConversation(pmuName, "PMU", "PDC", "parse", "error", err.Error())
		return
	}
	monitoring.IncFramesParsed()
	monitoring.NoteFrameParseOK(pmuName)

	if idx, ok := p.nextTraceIndex(pmuName); ok {
		logFirstFrames(pmuName, idx, reading)
	}

	qualityStart := time.Now()
	qerr := p.checker.Validate(reading)
	qualityDur := time.Since(qualityStart)
	monitoring.ObserveStage(pmuName, monitoring.StageQuality, qualityDur)
	if qerr != nil {
		monitoring.IncQualityRejected()
		monitoring.IncQualityRejectForPMU(pmuName)
		monitoring.NoteFrameQualityFlag(pmuName, qerr.Error(), reading)
		log.Printf("[%s] quality reject: %v", pmuName, qerr)
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "rejected", qerr.Error())
		if p.dropQualityRejected {
			monitoring.NoteFrameQualityDrop(pmuName, qerr.Error(), reading)
			return
		}
	} else {
		monitoring.NoteFrameQualityOK(pmuName)
	}

	// receivedAt = wire arrival (from receiver), not "after parse".
	reading.Trace = parser.LatencyTrace{
		ReceivedAtUnixNano: receivedAt.UnixNano(),
		ParseMs:            monitoring.Ms(parseDur),
		QualityMs:          monitoring.Ms(qualityDur),
	}
	if !reading.Timestamp.IsZero() {
		monitoring.UpdateClockOffset(pmuName, receivedAt, reading.Timestamp)
	}

	// Remember this frame in RAM so we can dump it later (not durable storage).
	output.RecordFrameAsync(pmuName, raw, reading)

	// Live inventory for the dashboard. Aligner buffers feed the publish loop.
	p.deliverLocal(reading)
}

func (p *pipeline) deliverLocal(r parser.Reading) {
	t0 := time.Now()
	monitoring.RecordReading(r)
	monitoring.ObserveStage(r.PMUName, monitoring.StageDashboardRecord, time.Since(t0))
	if r.Trace.ReceivedAtUnixNano > 0 {
		recv := time.Unix(0, r.Trace.ReceivedAtUnixNano)
		if lag := time.Since(recv); lag >= 0 && lag < 5*time.Minute {
			monitoring.ObserveStage(r.PMUName, monitoring.StageE2ERecvToDash, lag)
		}
		monitoring.ObservePMUClockMetrics(r.PMUName, recv, r.Timestamp)
	} else if !r.Timestamp.IsZero() {
		monitoring.ObservePMUClockMetrics(r.PMUName, time.Time{}, r.Timestamp)
	}

	// Fill aligner buffers; publisher emits aligned ticks for analytics charts.
	if p.buffers != nil {
		p.buffers.Ingest(r)
	}
}

// SyncAlignerExpected rebuilds buffers when the PMU set changes and restarts the chart ruler.
func (p *pipeline) SyncAlignerExpected(names []string) {
	if p == nil || p.buffers == nil {
		return
	}
	monitoring.ClearAllClockOffsets() // latency skew estimates; aligner keys are raw SOC
	p.buffers.SetExpected(names) // also resets publisher + clears aligned chart history
	period := aligner.DefaultPeriodMs
	if p.publisher != nil {
		period = aligner.DerivePeriodMs(names, p.publisher.PeriodMs())
		p.publisher.SetPeriodMs(period)
	}
	fps := 0.0
	if period > 0 {
		fps = 1000.0 / float64(period)
	}
	monitoring.SetAlignerTune(len(names), fps, time.Duration(period)*time.Millisecond, 0, 0, p.buffers.Capacity(), names)
	log.Printf("[aligner] buffers reset for %d PMUs period=%dms (no wait): %v", len(names), period, names)
}

// registerAlignerHandlers exposes buffer dumps so you can SEE what landed.
func (p *pipeline) registerAlignerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/conversation/aligner-buffers/sheets.xls", func(w http.ResponseWriter, r *http.Request) {
		snap := p.buffers.Snapshot()
		w.Header().Set("Content-Type", "application/vnd.ms-excel")
		w.Header().Set("Content-Disposition", `attachment; filename="aligner-buffers.xls"`)
		_ = aligner.WriteSheetsExcel(w, snap)
	})
	mux.HandleFunc("/conversation/aligner-buffers/sheets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = aligner.WriteSheetsHTML(w, p.buffers.Snapshot())
	})
	mux.HandleFunc("/conversation/aligner-buffers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.buffers.Snapshot())
	})
}

// logFirstFrames prints a friendly decode of the first few frames per PMU.
func logFirstFrames(pmuName string, traceIndex int, reading parser.Reading) {
	log.Printf("[%s] FRAME #%d decode @ %s freq=%.4f Hz",
		pmuName, traceIndex, reading.Timestamp.Format(time.RFC3339Nano), reading.Frequency)
	log.Printf("[%s]   STAT=0x%04X CRC=%s VA=%.1f∠%.1f°",
		pmuName, reading.Stat,
		map[bool]string{true: "ok", false: "BAD"}[reading.ChecksumValid],
		reading.VA.Magnitude, reading.VA.PhaseDegrees)
}

func main() {
	metricsAddr := flag.String("metrics-addr", ":2112", "dashboard + metrics port")
	apiAddr := flag.String("api-addr", ":8081", "PMU config REST API port")
	flag.Parse()

	// Address book of PMUs (Postgres). Needed even with readings-storage off.
	dbStore, err := store.NewStore()
	if err != nil {
		log.Fatalf("postgres (PMU config): %v", err)
	}
	defer dbStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Only one PDC at a time — otherwise two processes fight over the same PMU sockets.
	release, err := instance.Acquire()
	if err != nil {
		log.Fatal(err)
	}
	defer release()

	pl := newPipeline()

	if err := monitoring.StartServer(ctx, *metricsAddr,
		output.RegisterFrameCaptureHandler,
		pl.registerAlignerHandlers,
	); err != nil {
		log.Fatalf("metrics/dashboard server: %v", err)
	}
	monitoring.StartDashboardWorkers(ctx, envInt("DASHBOARD_QUEUE_SIZE", 16384), envInt("DASHBOARD_WORKERS", 4))
	if pl.publisher != nil {
		go pl.publisher.Run(ctx)
	}
	log.Printf("dashboard API on %s  |  PMU config API on %s", *metricsAddr, *apiAddr)
	log.Printf("pipeline: connect → parse → quality → inventory + align-publish → dashboard")
	log.Printf("inspect aligner buffers: http://127.0.0.1%s/conversation/aligner-buffers/sheets", *metricsAddr)
	log.Printf("CFG-2 layouts: learned live from each PMU handshake (in RAM only)")

	pmuManager := manager.NewPMUManager(func(pmuName string, raw []byte, receivedAt time.Time) {
		pl.HandleFrame(pmuName, raw, receivedAt)
	})
	pmuManager.SetAlignerHook(pl.SyncAlignerExpected)

	if err := api.StartServer(ctx, *apiAddr, dbStore, pmuManager); err != nil {
		log.Fatalf("REST API: %v", err)
	}

	pmus, err := dbStore.GetAllPMUs(ctx)
	if err != nil {
		log.Fatalf("load PMUs: %v", err)
	}
	if err := validatePMUPorts(pmus); err != nil {
		log.Fatalf("invalid PMU config: %v", err)
	}

	stagger := envDuration("PMU_STARTUP_STAGGER", 2*time.Second)
	for i := range pmus {
		pmus[i].Normalize()
		pmu := pmus[i]
		if i > 0 && stagger > 0 {
			log.Printf("waiting %s before starting %s", stagger, pmu.Name)
			time.Sleep(stagger)
		}
		if err := pmuManager.StartPMU(ctx, pmu); err != nil {
			log.Fatalf("start %s: %v", pmu.Name, err)
		}
	}

	monitoring.RecordConversation("SYSTEM", "PDC", "SYSTEM", "startup", "ok",
		"connect → parse → quality → inventory + time-align publish")

	<-ctx.Done()
	pmuManager.StopAll()
	log.Println("PDC shut down cleanly")
}
