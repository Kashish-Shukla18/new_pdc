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
// Saving readings to Postgres/Timescale stays parked in output/ (kept, not wired yet).
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
	"pdc/internal/envfile"
	"pdc/internal/instance"
	"pdc/manager"
	"pdc/monitoring"
	"pdc/output"
	"pdc/output/history"
	"pdc/parser"
	"pdc/store"
)

// pipeline is the in-memory path from "raw bytes" → "dashboard".
type pipeline struct {
	checker             *aligner.Checker   // quality gate
	buffers             *aligner.Bank      // per-PMU timestamp tables
	publisher           *aligner.Publisher // tick-grid align emit
	dropQualityRejected bool
	history             *history.Recorder
	timeLocMu           sync.RWMutex
	timeLoc             map[string]*time.Location // per-PMU SOC interpretation
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

func validatePMUEndpoints(pmus []config.PMUConfig) error {
	seen := make(map[string]string, len(pmus))
	for i := range pmus {
		p := &pmus[i]
		p.Normalize()
		if p.Name == "" {
			return fmt.Errorf("PMU missing ip/port")
		}
		key := p.EndpointKey()
		if other, ok := seen[key]; ok {
			return fmt.Errorf("duplicate endpoint %s: %s and %s", p.Name, other, p.Name)
		}
		seen[key] = p.Name
	}
	return nil
}

func newPipeline() *pipeline {
	maxClockSkew := envDuration("MAX_CLOCK_SKEW", 5*time.Second)
	dropQualityRejected := envBool("DROP_QUALITY_REJECTED", true)
	log.Printf("quality gate: max_clock_skew=%s drop_rejected=%t", maxClockSkew, dropQualityRejected)
	log.Printf("timestamp: fleet default TZ=%s (field SOC reinterpreted → UTC; loopback sims stay UTC)", config.DefaultTimestampTZ())

	// Keep a short in-memory tape of recent frames (for dump-frames tool).
	output.ConfigureFrameCapture(envInt("FRAME_CAPTURE_SIZE", 1500))

	cap := envInt("ALIGN_BUFFER_CAPACITY", aligner.DefaultCapacity)
	periodUs := int64(envInt("ALIGN_PERIOD_US", 0))
	if periodUs < 1 {
		periodUs = int64(envInt("ALIGN_PERIOD_MS", int(aligner.DefaultPeriodMs))) * 1000
	}
	buf := aligner.NewBank(cap)
	pub := aligner.NewPublisher(buf, periodUs, 0, monitoring.RecordAlignedFrame)
	buf.OnReset(func() {
		monitoring.ClearAlignedBatches()
	})
	p := &pipeline{
		checker:             aligner.NewChecker(maxClockSkew),
		buffers:             buf,
		publisher:           pub,
		dropQualityRejected: dropQualityRejected,
		timeLoc:             make(map[string]*time.Location),
		traceCounts:         make(map[string]int),
	}
	log.Printf("aligner: capacity=%d period=%dµs (≈%.3fms, no wait, head=MinNewest)",
		cap, periodUs, float64(periodUs)/1000.0)
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

// RegisterPMUTimeZone caches how this endpoint's SOC should be interpreted.
func (p *pipeline) RegisterPMUTimeZone(cfg config.PMUConfig) {
	if p == nil {
		return
	}
	cfg.Normalize()
	tz := cfg.EffectiveTimestampTZ()
	loc, err := config.LoadLocation(tz)
	if err != nil {
		log.Printf("[%s] timestamp_tz %q invalid (%v) — using UTC", cfg.Name, tz, err)
		loc = time.UTC
		tz = "UTC"
	}
	p.timeLocMu.Lock()
	p.timeLoc[cfg.Name] = loc
	p.timeLocMu.Unlock()
	log.Printf("[%s] measurement time zone: %s (fleet default %s)", cfg.Name, tz, config.DefaultTimestampTZ())
}

func (p *pipeline) correctTimestamp(pmuName string, ts time.Time) time.Time {
	if p == nil || ts.IsZero() {
		return ts
	}
	p.timeLocMu.RLock()
	loc := p.timeLoc[pmuName]
	p.timeLocMu.RUnlock()
	if loc == nil {
		// Unknown PMU: apply fleet default (not loopback heuristic without IP).
		var err error
		loc, err = config.LoadLocation(config.DefaultTimestampTZ())
		if err != nil {
			return ts.UTC()
		}
	}
	return config.ReinterpretAsTimezone(ts, loc)
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
	// Fleet IST (or per-PMU TZ) → UTC before quality / align / history.
	reading.Timestamp = p.correctTimestamp(pmuName, reading.Timestamp)

	monitoring.IncFramesParsed()
	monitoring.NoteFrameParseOK(pmuName)

	if idx, ok := p.nextTraceIndex(pmuName); ok {
		logFirstFrames(pmuName, idx, reading)
	}

	qerr := p.checker.Validate(reading)

	// receivedAt = wire arrival (from receiver), not "after parse".
	// Set before history tap so rejected frames still get received_at.
	reading.Trace = parser.LatencyTrace{
		ReceivedAtUnixNano: receivedAt.UnixNano(),
		ParseMs:            monitoring.Ms(parseDur),
	}

	qualityOK := qerr == nil
	rejectReason := ""
	if qerr != nil {
		rejectReason = qerr.Error()
		monitoring.IncQualityRejected()
		monitoring.IncQualityRejectForPMU(pmuName)
		monitoring.NoteFrameQualityFlag(pmuName, rejectReason, reading)
		log.Printf("[%s] quality reject: %v", pmuName, qerr)
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "rejected", rejectReason)
	} else {
		monitoring.NoteFrameQualityOK(pmuName)
	}

	// History tap: always enqueue after parse+Validate, independent of live drop flag.
	if p.history != nil {
		p.history.NoteQuality(pmuName, qualityOK, rejectReason)
		p.history.NoteFrame(reading, qualityOK, rejectReason)
	}

	if !qualityOK && p.dropQualityRejected {
		monitoring.NoteFrameQualityDrop(pmuName, rejectReason, reading)
		return
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

// SyncAlignerExpected updates aligner buffers to match the live PMU set.
// Uses Add/Remove so peers keep their samples when one PMU joins or leaves.
func (p *pipeline) SyncAlignerExpected(names []string) {
	if p == nil || p.buffers == nil {
		return
	}
	changed := p.buffers.SyncExpected(names)
	periodUs := aligner.DefaultPeriodUs
	if p.publisher != nil {
		periodUs = aligner.DerivePeriodUs(names, p.publisher.PeriodUs())
		p.publisher.SetPeriodUs(periodUs)
	}
	fps := 0.0
	if periodUs > 0 {
		fps = 1_000_000.0 / float64(periodUs)
	}
	monitoring.SetAlignerTune(len(names), fps, time.Duration(periodUs)*time.Microsecond, 0, 0, p.buffers.Capacity(), names)
	if changed {
		monitoring.ClearAllClockOffsets()
		log.Printf("[aligner] live set → %d PMUs period=%dµs (≈%.3fms): %v",
			len(names), periodUs, float64(periodUs)/1000.0, names)
	}
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
	mux.HandleFunc("/conversation/aligner-buffer-capacity", p.handleBufferCapacity)
}

type bufferCapacityBody struct {
	Capacity int `json:"capacity"`
}

func (p *pipeline) handleBufferCapacity(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.buffers == nil {
		http.Error(w, "aligner buffers unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	write := func(capacity int) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"capacity": capacity,
			"default":  aligner.DefaultCapacity,
			"min":      aligner.MinCapacity,
			"max":      aligner.MaxCapacity,
		})
	}

	switch r.Method {
	case http.MethodGet:
		write(p.buffers.Capacity())
	case http.MethodPut, http.MethodPost:
		var body bufferCapacityBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		// Bounds live only in aligner.Min/MaxCapacity (buffer.go); SetCapacity clamps.
		applied := p.buffers.SetCapacity(body.Capacity)
		monitoring.SetAlignerBufferCapacity(applied)
		log.Printf("aligner: buffer capacity set to %d (requested %d)", applied, body.Capacity)
		write(applied)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
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
	// Local .env fills unset knobs; real OS env always wins.
	if err := envfile.Load(".env"); err != nil {
		log.Printf("load .env: %v", err)
	}

	metricsAddr := flag.String("metrics-addr", ":2112", "dashboard + metrics port")
	apiAddr := flag.String("api-addr", ":8081", "PMU config REST API port")
	flag.Parse()

	// Address book of PMUs (Postgres). Soft-fail: PDC starts even if DB is down.
	dbStore := store.NewStore()
	defer dbStore.Close()
	if !dbStore.Ready() {
		log.Printf("postgres (PMU config): degraded — empty address book until DB is up")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Only one PDC at a time — otherwise two processes fight over the same PMU sockets.
	release, err := instance.Acquire()
	if err != nil {
		log.Fatal(err)
	}
	defer release()

	pl := newPipeline()

	var histRec *history.Recorder
	histSink, err := output.NewSinkFromEnv(ctx)
	if err != nil {
		log.Printf("history sink: %v (continuing without history)", err)
	}
	if histSink != nil {
		defer histSink.Close()
		histRec = history.NewRecorder(histSink.Writer())
		pl.history = histRec
		log.Printf("history: CFG/lifecycle events + frame ingest enabled")
	} else {
		log.Printf("history: disabled (set ENABLE_HISTORY=true in .env to turn on)")
	}

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
	log.Printf("inspect aligned samples: http://127.0.0.1%s/conversation/aligned-samples/sheets", *metricsAddr)
	log.Printf("CFG-2 layouts: learned live from each PMU handshake (in RAM only)")

	pmuManager := manager.NewPMUManager(func(pmuName string, raw []byte, receivedAt time.Time) {
		pl.HandleFrame(pmuName, raw, receivedAt)
	})
	pmuManager.SetAlignerHook(pl.SyncAlignerExpected)
	pmuManager.SetStationHook(func(name, station string) {
		if err := dbStore.UpdateStation(context.Background(), name, station); err != nil {
			log.Printf("[Manager] update station for %s: %v", name, err)
		}
	})
	pmuManager.SetPMUConfigHook(pl.RegisterPMUTimeZone)
	if histRec != nil {
		pmuManager.SetHistoryHooks(histRec)
	}

	if err := api.StartServer(ctx, *apiAddr, dbStore, pmuManager); err != nil {
		log.Fatalf("REST API: %v", err)
	}

	pmus, err := dbStore.GetActivePMUs(ctx)
	if err != nil {
		log.Printf("load PMUs: %v — starting with none (will use API once DB is up)", err)
		pmus = nil
	}
	if err := validatePMUEndpoints(pmus); err != nil {
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
