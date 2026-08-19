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
	"pdc/receiver"
	"pdc/store"
)

type pipeline struct {
	checker             *aligner.Checker
	publisher           *output.Publisher
	sink                *output.Sink
	kafkaSpool          *output.ReadingSpool
	sinkSpool           *output.ReadingSpool
	dropQualityRejected bool
	// fanOutViaKafka: when true, dashboard + Redis/Influx are fed by Kafka
	// readings consumers (separate consumer groups). HandleFrame only publishes.
	fanOutViaKafka bool
	traceMu        sync.Mutex
	traceCounts    map[string]int
	// sinkCh decouples Redis/Influx writes from the consumer hot path.
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

	fanOutViaKafka := publisher != nil && envBool("KAFKA_READINGS_FANOUT", true)
	return &pipeline{
		checker:             checker,
		publisher:           publisher,
		sink:                sink,
		kafkaSpool:          kafkaSpool,
		sinkSpool:           sinkSpool,
		dropQualityRejected: dropQualityRejected,
		fanOutViaKafka:      fanOutViaKafka,
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
				t0 := time.Now()
				err := p.sink.Store(ctx, r)
				monitoring.ObserveStage(r.PMUName, monitoring.StageSinkStore, time.Since(t0))
				if err != nil {
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
	p.handleFrame(ctx, pmuName, raw, ingestMeta{receivedAt: time.Now()})
}

type ingestMeta struct {
	receivedAt time.Time
	tcpWait    time.Duration
	tcpCopy    time.Duration
	tcpRead    time.Duration
	kafkaLag   time.Duration
}

func (p *pipeline) HandleRawData(ctx context.Context, frame output.RawFrame) {
	meta := ingestMeta{
		receivedAt: frame.ReceivedAt,
		tcpWait:    frame.TCPWait,
		tcpCopy:    frame.TCPCopy,
		tcpRead:    frame.TCPRead,
	}
	if meta.tcpRead == 0 {
		meta.tcpRead = meta.tcpWait + meta.tcpCopy
	}
	parseGate := time.Now()
	if meta.receivedAt.IsZero() {
		meta.receivedAt = parseGate
	} else if lag := parseGate.Sub(meta.receivedAt); lag >= 0 {
		meta.kafkaLag = lag
		monitoring.ObserveStage(frame.PMUName, monitoring.StageRawKafkaLag, lag)
		monitoring.ObserveStage(frame.PMUName, monitoring.StageFrameToParse, lag)
	}
	p.handleFrame(ctx, frame.PMUName, frame.Payload, meta)
}

func (p *pipeline) handleFrame(ctx context.Context, pmuName string, raw []byte, meta ingestMeta) {
	monitoring.IncFramesReceived()

	parseStart := time.Now()
	reading, err := parser.ParseDataFrame(pmuName, raw)
	parseDur := time.Since(parseStart)
	monitoring.ObserveStage(pmuName, monitoring.StageParse, parseDur)
	if err != nil {
		monitoring.IncParseErrors()
		log.Printf("[%s] parse error: %v", pmuName, err)
		monitoring.RecordConversation(pmuName, "PMU", "PDC", "parse", "error", err.Error())
		return
	}
	monitoring.IncFramesParsed()

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

		// STAT word decoding (IEEE C37.118.2-2011 Table 7)
		log.Printf("[%s] > STATUS WORD (0x%04X) - DECODED FLAGS", pmuName, reading.Stat)
		log.Printf("[%s]   Data Error Code:  %d (00=good, 01=PMU error, 10=test, 11=invalid)", pmuName, reading.StatDetail.DataErrorCode)
		log.Printf("[%s]   Data Error:       %v", pmuName, reading.StatDetail.DataError)
		log.Printf("[%s]   Config Change:    %v (bit 13)", pmuName, reading.StatDetail.CFGChange)
		log.Printf("[%s]   Trigger Event:    %v (bit 12)", pmuName, reading.StatDetail.TriggerDetected)
		log.Printf("[%s]   Sort Method:      %v (0=timestamp, 1=arrival)", pmuName, reading.StatDetail.SortMethod)
		log.Printf("[%s]   Time Sync:        %v (0=UTC locked, 1=unlocked)", pmuName, reading.StatDetail.PMUSyncStatus)
		log.Printf("[%s]   PMU Time Quality: %d (bits 9-6)", pmuName, reading.StatDetail.PMUTimeQuality)
		log.Printf("[%s]   Unlocked Time:    %d (0=<10s, 1=10-100s, 2=100-1000s, 3=>1000s)", pmuName, reading.StatDetail.UnlockedDuration)
		log.Printf("[%s]   Trigger Reason:   %d (bits 3-0)", pmuName, reading.StatDetail.PMUTriggerReason)
		log.Printf("[%s]   MSG_TQ:           code=%d leap_dir=%v leap_occ=%v leap_pend=%v", pmuName,
			reading.MsgTQ.TimeQualityCode, reading.MsgTQ.LeapSecondDirection, reading.MsgTQ.LeapSecondOccurred, reading.MsgTQ.LeapSecondPending)

		// Phasors (raw + derived)
		log.Printf("[%s] > PHASOR MEASUREMENTS (Rectangular -> Polar)", pmuName)

		fmtPhasor := func(name string, ph parser.Phasor) {
			log.Printf("[%s]   %s:", pmuName, name)
			log.Printf("[%s]     Rectangular: R=%.4f V/A, I=%.4f V/A", pmuName, ph.Real, ph.Imag)
			log.Printf("[%s]     Polar:       Mag=%.4f V/A RMS, Phase=%.4f deg (%.6f rad)", pmuName, ph.Magnitude, ph.PhaseDegrees, ph.PhaseRadians)
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

	qualityStart := time.Now()
	qerr := p.checker.Validate(reading)
	qualityDur := time.Since(qualityStart)
	monitoring.ObserveStage(pmuName, monitoring.StageQuality, qualityDur)
	if qerr != nil {
		monitoring.IncQualityRejected()
		monitoring.IncQualityRejectForPMU(pmuName)
		log.Printf("[%s] quality reject: %v", pmuName, qerr)
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "rejected", qerr.Error())
		if p.dropQualityRejected {
			return
		}
		monitoring.RecordConversation(pmuName, "PDC", "PDC", "quality", "warn", "continuing despite quality reject to avoid data loss")
	}

	if meta.receivedAt.IsZero() {
		meta.receivedAt = time.Now()
	}
	reading.Trace = parser.LatencyTrace{
		ReceivedAtUnixNano: meta.receivedAt.UnixNano(),
		TcpWaitMs:          monitoring.Ms(meta.tcpWait),
		TcpCopyMs:          monitoring.Ms(meta.tcpCopy),
		TcpReadMs:          monitoring.Ms(meta.tcpRead),
		KafkaLagMs:         monitoring.Ms(meta.kafkaLag),
		FrameToParseMs:     monitoring.Ms(meta.kafkaLag),
		ParseMs:            monitoring.Ms(parseDur),
		QualityMs:          monitoring.Ms(qualityDur),
	}
	if !meta.receivedAt.IsZero() && !reading.Timestamp.IsZero() {
		monitoring.UpdateClockOffset(pmuName, meta.receivedAt, reading.Timestamp)
	}

	if p.fanOutViaKafka {
		// Split processor: dashboard/sink come from Kafka consumer groups.
		if p.publisher != nil {
			pubStart := time.Now()
			if err := p.publisher.Publish(ctx, reading); err != nil {
				monitoring.ObserveStage(pmuName, monitoring.StageReadingsPublish, time.Since(pubStart))
				monitoring.IncQueuePublishErrors()
				monitoring.IncKafkaErrorForPMU(pmuName)
				log.Printf("[%s] kafka publish error: %v", pmuName, err)
				monitoring.RecordConversation(pmuName, "PDC", "KAFKA", "publish", "error", err.Error())
				if p.kafkaSpool != nil {
					if spoolErr := p.kafkaSpool.Append(reading); spoolErr != nil {
						log.Printf("[%s] kafka spool append error: %v", pmuName, spoolErr)
					} else {
						monitoring.IncSpoolQueued()
						monitoring.IncSpoolQueuedForPMU(pmuName)
					}
				}
				p.deliverLocal(reading)
				return
			}
			monitoring.ObserveStage(pmuName, monitoring.StageReadingsPublish, time.Since(pubStart))
		}
		return
	}

	// Live path: dashboard + sink in this process, then Kafka for durability.
	p.deliverLocal(reading)
	if p.publisher != nil {
		pubStart := time.Now()
		if err := p.publisher.Publish(ctx, reading); err != nil {
			monitoring.ObserveStage(pmuName, monitoring.StageReadingsPublish, time.Since(pubStart))
			monitoring.IncQueuePublishErrors()
			monitoring.IncKafkaErrorForPMU(pmuName)
			log.Printf("[%s] kafka publish error: %v", pmuName, err)
			if p.kafkaSpool != nil {
				if spoolErr := p.kafkaSpool.Append(reading); spoolErr != nil {
					log.Printf("[%s] kafka spool append error: %v", pmuName, spoolErr)
				} else {
					monitoring.IncSpoolQueued()
					monitoring.IncSpoolQueuedForPMU(pmuName)
				}
			}
			return
		}
		monitoring.ObserveStage(pmuName, monitoring.StageReadingsPublish, time.Since(pubStart))
	}
}

// OnDashboardReading feeds the live SSE dashboard from the pdc-dashboard consumer group.
func (p *pipeline) OnDashboardReading(_ context.Context, r parser.Reading) error {
	monitoring.IncReadingsConsumed()
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
	return nil
}

// OnSinkReading enqueues Redis/Influx work from the pdc-sink consumer group.
func (p *pipeline) OnSinkReading(_ context.Context, r parser.Reading) error {
	monitoring.IncReadingsConsumed()
	p.enqueueSink(r)
	monitoring.ObserveLatency(monitoring.CorrectedPMULag(r.PMUName, r.Timestamp))
	return nil
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
	p.enqueueSink(r)
	monitoring.ObserveLatency(monitoring.CorrectedPMULag(r.PMUName, r.Timestamp))
}

func (p *pipeline) enqueueSink(r parser.Reading) {
	if p.sink == nil {
		return
	}
	select {
	case p.sinkCh <- r:
		monitoring.IncSinkInflight()
	default:
		monitoring.IncStoreErrors()
		monitoring.IncSinkErrorForPMU(r.PMUName)
		log.Printf("[%s] sink channel full – spooling directly", r.PMUName)
		monitoring.RecordConversation(r.PMUName, "PDC", "SINK", "overload", "warn", "sink channel full; spooled")
		if p.sinkSpool != nil {
			if spoolErr := p.sinkSpool.Append(r); spoolErr != nil {
				log.Printf("[%s] sink spool append error: %v", r.PMUName, spoolErr)
			} else {
				monitoring.IncSpoolQueued()
				monitoring.IncSpoolQueuedForPMU(r.PMUName)
			}
		}
	}
}

// StartReadingsConsumers starts independent Kafka consumer groups for dashboard and sink fan-out.
func (p *pipeline) StartReadingsConsumers(ctx context.Context) (cleanup func()) {
	noop := func() {}
	if p == nil || !p.fanOutViaKafka || p.publisher == nil {
		log.Printf("readings kafka fan-out disabled (local dashboard+sink delivery)")
		return noop
	}

	dash := output.NewReadingsConsumerFromEnv("KAFKA_DASHBOARD_GROUP", "pdc-dashboard")
	sinkC := output.NewReadingsConsumerFromEnv("KAFKA_SINK_GROUP", "pdc-sink")
	if dash == nil || sinkC == nil {
		log.Printf("readings consumers unavailable – falling back to local delivery")
		p.fanOutViaKafka = false
		return noop
	}

	go func() {
		if err := dash.Run(ctx, p.OnDashboardReading); err != nil && ctx.Err() == nil {
			log.Printf("dashboard readings consumer stopped: %v", err)
		}
	}()
	go func() {
		if err := sinkC.Run(ctx, p.OnSinkReading); err != nil && ctx.Err() == nil {
			log.Printf("sink readings consumer stopped: %v", err)
		}
	}()

	log.Printf("readings fan-out consumers started: topic=%s groups=[%s, %s]",
		dash.Topic(), dash.Group(), sinkC.Group())
	monitoring.RecordConversation("SYSTEM", "PDC", "KAFKA", "readings-fanout", "ok",
		fmt.Sprintf("groups=%s,%s", dash.Group(), sinkC.Group()))

	return func() {
		_ = dash.Close()
		_ = sinkC.Close()
	}
}

// HydrateFromRedis seeds the in-memory dashboard bus from Redis :latest keys.
// This recovers live UI state after restart and supports shared state across instances.
func (p *pipeline) HydrateFromRedis(ctx context.Context) {
	if p == nil || p.sink == nil {
		return
	}
	readings, err := p.sink.ListLatestReadings(ctx)
	if err != nil {
		log.Printf("redis hydrate warning: %v", err)
		monitoring.RecordConversation("SYSTEM", "PDC", "REDIS", "hydrate", "warn", err.Error())
		return
	}
	for _, r := range readings {
		monitoring.RecordReading(r)
	}
	log.Printf("redis live-state hydrate: loaded %d latest reading(s)", len(readings))
	if len(readings) > 0 {
		monitoring.RecordConversation("SYSTEM", "PDC", "REDIS", "hydrate", "ok",
			fmt.Sprintf("loaded %d latest reading(s)", len(readings)))
	}
}

func main() {
	metricsAddr := flag.String("metrics-addr", ":2112", "prometheus metrics listen address")
	apiAddr := flag.String("api-addr", ":8081", "REST API listen address")
	modeFlag := flag.String("mode", envOrFallback("PDC_MODE", "all"),
		"run mode: all (ingress+processor via Kafka), ingress (TCP→raw Kafka), processor (raw Kafka→parse/sink), direct (TCP→parse, bypass raw Kafka)")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	switch mode {
	case "all", "ingress", "processor", "direct":
	default:
		log.Fatalf("invalid -mode %q (want all|ingress|processor|direct)", mode)
	}

	dbStore, err := store.NewStore()
	if err != nil {
		log.Fatalf("failed to initialize influx db store: %v", err)
	}
	defer dbStore.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoring.StartServer(ctx, *metricsAddr)
	log.Printf("prometheus metrics listening on %s/metrics", *metricsAddr)
	log.Printf("PDC mode=%s", mode)

	runProcessor := mode == "all" || mode == "processor" || mode == "direct"
	runIngress := mode == "all" || mode == "ingress" || mode == "direct"

	// Persist CFG2 profiles so DATA can be parsed after a processor restart
	// without waiting for ingress to re-handshake and republish CFG2.
	profileDir := ""
	if envBool("CFG2_PROFILE_PERSIST", true) {
		profileDir = envOrFallback("CFG2_PROFILE_DIR", "data/profiles")
	}
	parser.ConfigureProfileStore(profileDir)
	if profileDir != "" {
		n, err := parser.LoadPersistedProfiles(profileDir)
		if err != nil {
			log.Printf("cfg2 profile hydrate warning: %v", err)
		} else {
			log.Printf("cfg2 profile store: dir=%s hydrated=%d", profileDir, n)
			if n > 0 {
				monitoring.RecordConversation("SYSTEM", "PDC", "CFG2", "hydrate", "ok",
					fmt.Sprintf("loaded %d persisted profile(s) from %s", n, profileDir))
			}
		}
	} else {
		log.Printf("cfg2 profile persistence disabled")
	}

	var pl *pipeline
	var rawPub *output.RawFramePublisher
	var rawConsumer *output.RawFrameConsumer

	if runProcessor {
		pl = newPipeline(ctx)
		defer pl.Close()
		// Same-process live path: parse → dashboard/sink without Kafka round-trips.
		// Kafka remains for durability and split processor/ingress deployments.
		if mode == "all" || mode == "direct" {
			pl.fanOutViaKafka = envBool("KAFKA_READINGS_FANOUT", false)
		}
		pl.HydrateFromRedis(ctx)
		pl.StartReplay(ctx)
		pl.StartSinkWorkers(ctx)
		cleanupReadings := pl.StartReadingsConsumers(ctx)
		defer cleanupReadings()
		monitoring.RecordConversation("SYSTEM", "PDC", "SYSTEM", "startup", "ok",
			fmt.Sprintf("processor pipeline started (mode=%s fanout_kafka=%t)", mode, pl.fanOutViaKafka))
	}

	if mode == "all" || mode == "ingress" {
		rawPub = output.NewRawFramePublisherFromEnv()
		if rawPub == nil {
			log.Fatalf("mode=%s requires Kafka brokers (set KAFKA_BROKERS)", mode)
		}
		defer func() { _ = rawPub.Close() }()
	}

	if mode == "processor" {
		rawConsumer = output.NewRawFrameConsumerFromEnv()
		if rawConsumer == nil {
			log.Fatalf("mode=%s requires Kafka brokers (set KAFKA_BROKERS)", mode)
		}
		defer func() { _ = rawConsumer.Close() }()

		headerTexts := sync.Map{} // pmuName -> header text from HDR frames
		go func() {
			err := rawConsumer.Run(ctx, func(frameCtx context.Context, frame output.RawFrame) error {
				monitoring.IncRawFramesConsumed()
				switch frame.FrameType {
				case output.FrameTypeHDR:
					text, perr := parser.ParseHeaderFrame(frame.Payload)
					if perr != nil {
						log.Printf("[%s] raw HDR parse error: %v", frame.PMUName, perr)
						return nil
					}
					headerTexts.Store(frame.PMUName, text)
					monitoring.RecordConversation(frame.PMUName, "KAFKA", "PDC", "raw-hdr", "ok",
						fmt.Sprintf("consumed HEADER (%d bytes)", len(frame.Payload)))
					return nil
				case output.FrameTypeCFG2:
					profile, perr := parser.ParseCFG2Frame(frame.Payload)
					if perr != nil {
						monitoring.IncParseErrors()
						log.Printf("[%s] raw CFG2 parse error: %v", frame.PMUName, perr)
						return perr
					}
					if v, ok := headerTexts.Load(frame.PMUName); ok {
						if s, ok := v.(string); ok {
							profile.HeaderText = s
						}
					}
					parser.SetProfile(frame.PMUName, profile)
					monitoring.RecordConversation(frame.PMUName, "KAFKA", "PDC", "raw-cfg2", "ok",
						fmt.Sprintf("registered CFG2 station=%q rate=%d", profile.Station, profile.DataRate))
					return nil
				case output.FrameTypeData, "":
					pl.HandleRawData(frameCtx, frame)
					return nil
				default:
					log.Printf("[%s] ignoring unknown raw frame type %q", frame.PMUName, frame.FrameType)
					return nil
				}
			})
			if err != nil && ctx.Err() == nil {
				log.Printf("raw kafka consumer stopped: %v", err)
			}
		}()
		log.Printf("raw kafka consumer started (topic=%s group=%s)",
			envOrFallback("KAFKA_RAW_TOPIC", "pmu.raw.frames"),
			envOrFallback("KAFKA_RAW_GROUP", "pdc-processor"))
	}

	var pmuManager *manager.PMUManager
	if runIngress {
		var directHandler receiver.FrameHandler
		var ingressPub receiver.RawFramePublisher
		if mode == "direct" || mode == "all" {
			directHandler = func(pmuName string, raw []byte) {
				pl.HandleFrame(ctx, pmuName, raw)
			}
		}
		if mode != "direct" {
			ingressPub = rawPub
		}
		pmuManager = manager.NewPMUManager(directHandler, ingressPub)
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
	} else {
		// Processor-only: still expose API for PMU CRUD, but no live TCP receivers.
		pmuManager = manager.NewPMUManager(nil, nil)
		api.StartServer(*apiAddr, dbStore, pmuManager)
		log.Printf("REST API listening on %s (processor-only; PMU TCP owned by ingress)", *apiAddr)
	}

	<-ctx.Done()
	if pmuManager != nil {
		pmuManager.StopAll()
	}
	log.Println("PDC shut down cleanly")
}
