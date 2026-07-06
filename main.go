package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pdc/aligner"
	"pdc/api"
	"pdc/manager"
	"pdc/monitoring"
	"pdc/output"
	"pdc/parser"
	"pdc/store"
)

type pipeline struct {
	checker             *aligner.Checker
	publisher           *output.Publisher
	sink                *output.Sink
	kafkaSpool          *output.ReadingSpool
	sinkSpool           *output.ReadingSpool
	dropQualityRejected bool
	traceMu             sync.Mutex
	traceCounts         map[string]int
	// sinkCh decouples the sink (Redis + InfluxDB) from the Kafka hot path.
	// Frames are enqueued here and drained by sinkWorkers in the background,
	// so a slow InfluxDB flush never blocks Kafka publishing or the frame loop.
	sinkCh chan parser.Reading
}

func envOrFallback(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
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

func newPipeline(ctx context.Context) *pipeline {
	maxClockSkew := envDuration("MAX_CLOCK_SKEW", 24*time.Hour)
	checker := aligner.NewChecker(maxClockSkew)
	publisher := output.NewPublisherFromEnv()
	kafkaSpool := output.NewReadingSpool(envOrFallback("KAFKA_SPOOL_FILE", "data/spool/kafka_failed.jsonl"))
	sinkSpool := output.NewReadingSpool(envOrFallback("SINK_SPOOL_FILE", "data/spool/sink_failed.jsonl"))
	dropQualityRejected := envBool("DROP_QUALITY_REJECTED", false)
	log.Printf("quality gate config: max_clock_skew=%s drop_quality_rejected=%t", maxClockSkew, dropQualityRejected)

	sink, err := output.NewSinkFromEnv(ctx)
	if err != nil {
		log.Printf("storage sink unavailable (continuing with parse+queue only): %v", err)
		monitoring.RecordConversation("SYSTEM", "PDC", "SINK", "init", "warn", err.Error())
	}

	primeSpoolBacklog := func(name string, spool *output.ReadingSpool) {
		if spool == nil {
			return
		}
		counts, countErr := spool.CountsByPMU()
		if countErr != nil {
			log.Printf("[%s spool] count error: %v", name, countErr)
			return
		}
		total := 0
		for pmu, n := range counts {
			total += n
			monitoring.AddSpoolQueuedForPMU(pmu, int64(n))
		}
		if total > 0 {
			log.Printf("[%s spool] restored pending backlog=%d", name, total)
		}
	}

	primeSpoolBacklog("kafka", kafkaSpool)
	primeSpoolBacklog("sink", sinkSpool)

	// Sink channel: buffer enough for several seconds of all-PMU traffic.
	// At 50fps × 20 PMUs = 1000 frames/s; 8192 gives ~8 s of headroom.
	sinkBufSize := envInt("SINK_CHANNEL_SIZE", 8192)
	if sinkBufSize < 256 {
		sinkBufSize = 256
	}

	return &pipeline{
		checker:             checker,
		publisher:           publisher,
		sink:                sink,
		kafkaSpool:          kafkaSpool,
		sinkSpool:           sinkSpool,
		dropQualityRejected: dropQualityRejected,
		traceCounts:         make(map[string]int),
		sinkCh:              make(chan parser.Reading, sinkBufSize),
	}
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

func (p *pipeline) StartReplay(ctx context.Context) {
	kafkaReplayEnabled := envBool("KAFKA_REPLAY_ENABLED", true)
	kafkaReplayInterval := envDuration("KAFKA_REPLAY_INTERVAL", 1*time.Second)
	kafkaReplayBatch := envInt("KAFKA_REPLAY_BATCH", 2000)

	// Sink replay is intentionally conservative to avoid overloading Influx.
	sinkReplayEnabled := envBool("SINK_REPLAY_ENABLED", true)
	sinkReplayInterval := envDuration("SINK_REPLAY_INTERVAL", 2*time.Second)
	sinkReplayBatch := envInt("SINK_REPLAY_BATCH", 200)

	p.startReplayLoop(ctx, "kafka", p.kafkaSpool, kafkaReplayEnabled, kafkaReplayInterval, kafkaReplayBatch, func(replayCtx context.Context, r parser.Reading) error {
		return p.publisher.Publish(replayCtx, r)
	})

	p.startReplayLoop(ctx, "sink", p.sinkSpool, sinkReplayEnabled, sinkReplayInterval, sinkReplayBatch, func(replayCtx context.Context, r parser.Reading) error {
		if p.sink == nil {
			return nil
		}
		return p.sink.Store(replayCtx, r)
	})
}

func (p *pipeline) startReplayLoop(
	ctx context.Context,
	name string,
	spool *output.ReadingSpool,
	enabled bool,
	interval time.Duration,
	maxBatch int,
	fn func(context.Context, parser.Reading) error,
) {
	if spool == nil {
		return
	}
	if !enabled {
		log.Printf("[%s spool] replay disabled", name)
		return
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if maxBatch <= 0 {
		maxBatch = 1
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := spool.Replay(ctx, fn, maxBatch)
				if err != nil {
					log.Printf("[%s spool] replay error: %v", name, err)
					monitoring.RecordConversation("SYSTEM", "PDC", strings.ToUpper(name), "spool-replay", "error", err.Error())
					continue
				}

				if stats.Replayed > 0 {
					monitoring.IncSpoolReplayed(stats.Replayed)
					for pmu, n := range stats.ReplayedByPMU {
						monitoring.DecSpoolQueuedForPMU(pmu, int64(n))
					}
					msg := fmt.Sprintf("replayed=%d pending=%d", stats.Replayed, stats.Pending)
					log.Printf("[%s spool] %s", name, msg)
					monitoring.RecordConversation("SYSTEM", "PDC", strings.ToUpper(name), "spool-replay", "ok", msg)
				}
			}
		}
	}()
}

// StartSinkWorkers drains the sinkCh and writes each reading to Redis + InfluxDB.
// nWorkers run in parallel so a single slow write doesn't block others.
func (p *pipeline) StartSinkWorkers(ctx context.Context) {
	if p.sink == nil {
		// No sink configured: drain channel to avoid goroutine leak.
		go func() {
			for range p.sinkCh {
			}
		}()
		return
	}

	nWorkers := envInt("SINK_WORKERS", 4)
	if nWorkers < 1 {
		nWorkers = 1
	}

	for i := 0; i < nWorkers; i++ {
		go func() {
			for r := range p.sinkCh {
				monitoring.DecSinkInflight()
				if err := p.sink.Store(ctx, r); err != nil {
					monitoring.IncStoreErrors()
					monitoring.IncSinkErrorForPMU(r.PMUName)
					log.Printf("[%s] sink store error: %v", r.PMUName, err)
					monitoring.RecordConversation(r.PMUName, "PDC", "SINK", "store", "error", err.Error())
					if p.sinkSpool != nil {
						if spoolErr := p.sinkSpool.Append(r); spoolErr != nil {
							log.Printf("[%s] sink spool append error: %v", r.PMUName, spoolErr)
							monitoring.RecordConversation(r.PMUName, "PDC", "SINK", "spool", "error", spoolErr.Error())
						} else {
							monitoring.IncSpoolQueued()
							monitoring.IncSpoolQueuedForPMU(r.PMUName)
							monitoring.RecordConversation(r.PMUName, "PDC", "SINK", "spool", "queued", "store failed; buffered to disk")
						}
					}
				}
			}
		}()
	}
}

func (p *pipeline) Close() {
	if p == nil {
		return
	}
	// Close sink channel so workers drain cleanly.
	if p.sinkCh != nil {
		close(p.sinkCh)
	}
	if p.publisher != nil {
		_ = p.publisher.Close()
	}
	if p.sink != nil {
		p.sink.Flush()
		p.sink.Close()
	}
}

func (p *pipeline) HandleFrame(ctx context.Context, pmuName string, raw []byte) {
	monitoring.IncFramesReceived()

	reading, err := parser.ParseDataFrame(pmuName, raw)
	if err != nil {
		monitoring.IncParseErrors()
		log.Printf("[%s] parse error: %v", pmuName, err)
		monitoring.RecordConversation(pmuName, "PMU", "PDC", "parse", "error", err.Error())
		return
	}
	monitoring.IncFramesParsed()
	monitoring.RecordReading(reading)

	if traceIndex, ok := p.nextTraceIndex(pmuName); ok {
		log.Printf("[%s] ================================================================================", pmuName)
		log.Printf("[%s] FRAME #%d - IEEE C37.118.2-2011 DATA FRAME COMPLETE DECODE", pmuName, traceIndex)
		log.Printf("[%s] ================================================================================", pmuName)

		// Identity and timing
		log.Printf("[%s] > TIMING & IDENTITY", pmuName)
		log.Printf("[%s]   Timestamp:        %s (us precision)", pmuName, reading.Timestamp.Format(time.RFC3339Nano))
		log.Printf("[%s]   SOC (Unix):       %d | FRACSEC: 0x%08X", pmuName, reading.SOC, reading.FracSecRaw)
		log.Printf("[%s]   Frac Count:       %d / 1,000,000 (microseconds)", pmuName, reading.FracSecCount)
		log.Printf("[%s]   PMU/Stream ID:    0x%04X", pmuName, reading.IDCode)
		log.Printf("[%s]   Frame Sync:       0x%04X | Size: %d bytes", pmuName, reading.SyncWord, reading.FrameSize)
		log.Printf("[%s]   CRC-CCITT:        0x%04X [%s]", pmuName, reading.Checksum, map[bool]string{true: "VALID", false: "FAILED"}[reading.ChecksumValid])

		// STAT word decoding
		log.Printf("[%s] > STATUS WORD (0x%04X) - DECODED FLAGS", pmuName, reading.Stat)
		log.Printf("[%s]   Data Validity:    %v (0=OK, 1=Out of sync)", pmuName, reading.StatDetail.SyncPMU)
		log.Printf("[%s]   Data Error:       %v (1=error detected)", pmuName, reading.StatDetail.DataErr)
		log.Printf("[%s]   GPS Lock Status:  %v (1=NOT locked)", pmuName, reading.StatDetail.PMUSyncStatus)
		log.Printf("[%s]   Time Quality:     %d (0=UTC locked, 1-7=unlocked levels)", pmuName, reading.StatDetail.PMUTimeQuality)
		log.Printf("[%s]   Unlocked Time:    %v (0=<5s, 1=5-10s, 2=10-60s, 3=>60s)", pmuName, reading.StatDetail.UnlockedDuration)
		log.Printf("[%s]   Sort Method:      %v (0=sample time, 1=arrival time)", pmuName, reading.StatDetail.SortMethod)
		log.Printf("[%s]   Config Change:    %v (1=change pending)", pmuName, reading.StatDetail.CFGChange)
		log.Printf("[%s]   Trigger Event:    %v (1=detected)", pmuName, reading.StatDetail.TriggerDetected)
		log.Printf("[%s]   Trigger Reason:   %d (0=manual, 1-15=fault types)", pmuName, reading.StatDetail.PMUTriggerReason)
		log.Printf("[%s]   Post-Processed:   %v (DSO flag)", pmuName, reading.StatDetail.DSO)

		// Phasors (raw + derived)
		log.Printf("[%s] > PHASOR MEASUREMENTS (Rectangular -> Polar)", pmuName)

		fmtPhasor := func(name string, p parser.Phasor) {
			log.Printf("[%s]   %s:", pmuName, name)
			log.Printf("[%s]     Rectangular: R=%.4f V/A, I=%.4f V/A", pmuName, p.Real, p.Imag)
			log.Printf("[%s]     Polar:       Mag=%.4f V/A RMS, Phase=%.4f deg (%.6f rad)", pmuName, p.Magnitude, p.PhaseDegrees, p.PhaseRadians)
		}

		fmtPhasor("Voltage Phase A (VA)", reading.VA)
		fmtPhasor("Voltage Phase B (VB)", reading.VB)
		fmtPhasor("Voltage Phase C (VC)", reading.VC)
		fmtPhasor("Current Phase A (IA)", reading.IA)

		// Sequence components and imbalance
		log.Printf("[%s] > DERIVED PHASOR ANALYSIS", pmuName)
		log.Printf("[%s]   Sequence Components: V+=%.3f V RMS, V-=%.3f V RMS, V0=%.3f V RMS", pmuName, reading.SequencePos, reading.SequenceNeg, reading.SequenceZero)
		log.Printf("[%s]   Voltage Imbalance: %.3f %% (V-/V+ ratio)", pmuName, reading.VoltageImbalancePercent)
		log.Printf("[%s]   Phase Separations:", pmuName)
		log.Printf("[%s]     VAB (B-A): %.3f deg | VBC (C-B): %.3f deg | VCA (A-C): %.3f deg", pmuName,
			reading.VAB_PhaseAngleDifference,
			reading.VBC_PhaseAngleDifference,
			reading.VCA_PhaseAngleDifference)

		// Frequency
		log.Printf("[%s] > FREQUENCY & DYNAMICS", pmuName)
		log.Printf("[%s]   Frequency:        %.6f Hz (nominal 50 Hz)", pmuName, reading.Frequency)
		log.Printf("[%s]   Deviation:        %+.6f Hz (from 50 Hz)", pmuName, reading.FrequencyDeviation)
		log.Printf("[%s]   ROCOF (dF/dt):    %+.6f Hz/s (inertia & stability indicator)", pmuName, reading.ROCOF)

		// Power
		log.Printf("[%s] > POWER QUANTITIES", pmuName)
		log.Printf("[%s]   Active Power (P):      %.3f MW", pmuName, reading.MW)
		log.Printf("[%s]   Reactive Power (Q):    %.3f MVAR", pmuName, reading.MVAR)
		log.Printf("[%s]   Apparent Power (S):    %.3f MVA", pmuName, reading.MVA)
		log.Printf("[%s]   Power Factor:          %.6f (%s)", pmuName, reading.PowerFactor, reading.PowerFactorLeadLag)
		log.Printf("[%s]   Total Power (VA phasor):", pmuName)
		log.Printf("[%s]     Real:  %.3f W", pmuName, reading.TotalPowerReal)
		log.Printf("[%s]     Imag:  %.3f VAR", pmuName, reading.TotalPowerImag)

		// Analog and digital
		log.Printf("[%s] > ANALOG & DIGITAL I/O", pmuName)
		log.Printf("[%s]   Digital Word:     0x%04X (16-bit status/control word)", pmuName, reading.Digital)
		for i := 0; i < 16; i++ {
			bit := (reading.Digital >> uint(i)) & 1
			log.Printf("[%s]     BIT[%02d]:        %d", pmuName, i, bit)
		}

		log.Printf("[%s] ================================================================================", pmuName)
	}

	if err := p.checker.Validate(reading); err != nil {
		monitoring.IncQualityRejected()
		monitoring.IncQualityRejectForPMU(pmuName)
		log.Printf("[%s] quality reject: %v", pmuName, err)
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "rejected", err.Error())
		if p.dropQualityRejected {
			return
		}
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "warn", "continuing despite quality reject to avoid data loss")
	}

	if err := p.publisher.Publish(ctx, reading); err != nil {
		monitoring.IncQueuePublishErrors()
		monitoring.IncKafkaErrorForPMU(pmuName)
		log.Printf("[%s] kafka publish error: %v", pmuName, err)
		monitoring.RecordConversation(pmuName, "PDC", "KAFKA", "publish", "error", err.Error())
		if p.kafkaSpool != nil {
			if spoolErr := p.kafkaSpool.Append(reading); spoolErr != nil {
				log.Printf("[%s] kafka spool append error: %v", pmuName, spoolErr)
				monitoring.RecordConversation(pmuName, "PDC", "KAFKA", "spool", "error", spoolErr.Error())
			} else {
				monitoring.IncSpoolQueued()
				monitoring.IncSpoolQueuedForPMU(pmuName)
				monitoring.RecordConversation(pmuName, "PDC", "KAFKA", "spool", "queued", "publish failed; buffered to disk")
			}
		}
	}

	// Enqueue to sink channel (non-blocking): if channel is full, spool directly
	// rather than blocking Kafka or the frame goroutine.
	if p.sink != nil {
		select {
		case p.sinkCh <- reading:
			monitoring.IncSinkInflight()
		default:
			monitoring.IncStoreErrors()
			monitoring.IncSinkErrorForPMU(pmuName)
			log.Printf("[%s] sink channel full – spooling directly", pmuName)
			monitoring.RecordConversation(pmuName, "PDC", "SINK", "overload", "warn", "sink channel full; spooled")
			if p.sinkSpool != nil {
				if spoolErr := p.sinkSpool.Append(reading); spoolErr != nil {
					log.Printf("[%s] sink spool append error: %v", pmuName, spoolErr)
				} else {
					monitoring.IncSpoolQueued()
					monitoring.IncSpoolQueuedForPMU(pmuName)
				}
			}
		}
	}

	monitoring.ObserveLatency(time.Since(reading.Timestamp))
}

func main() {
	metricsAddr := flag.String("metrics-addr", ":2112", "prometheus metrics listen address")
	apiAddr := flag.String("api-addr", ":8081", "REST API listen address")
	flag.Parse()

	dbStore, err := store.NewStore()
	if err != nil {
		log.Fatalf("failed to initialize influx db store: %v", err)
	}
	defer dbStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoring.StartServer(ctx, *metricsAddr)
	log.Printf("prometheus metrics listening on %s/metrics", *metricsAddr)

	pl := newPipeline(ctx)
	defer pl.Close()
	pl.StartReplay(ctx)
	pl.StartSinkWorkers(ctx)

	monitoring.RecordConversation("SYSTEM", "PDC", "SYSTEM", "startup", "ok", "pipeline started")

	pmuManager := manager.NewPMUManager(func(pmuName string, raw []byte) {
		pl.HandleFrame(ctx, pmuName, raw)
	})

	api.StartServer(*apiAddr, dbStore, pmuManager)
	log.Printf("REST API listening on %s", *apiAddr)

	pmus, err := dbStore.GetAllPMUs(ctx)
	if err != nil {
		log.Printf("failed to load PMUs from DB: %v", err)
	} else {
		for _, pmu := range pmus {
			if err := pmuManager.StartPMU(ctx, pmu); err != nil {
				log.Printf("failed to start PMU %s: %v", pmu.Name, err)
			}
		}
	}

	<-ctx.Done()
	pmuManager.StopAll()
	log.Println("PDC shut down cleanly")
}
