package monitoring

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"pdc/parser"
)

// Pipeline stage IDs — keep stable; they are Prometheus labels and JSON keys.
const (
	StageDial            = "tcp_dial"
	StageHandshakeHDR    = "handshake_hdr"
	StageHandshakeCFG2   = "handshake_cfg2"
	StageHandshakeTotal  = "handshake_total"
	StageTCPWait         = "tcp_wait"
	StageTCPCopy         = "tcp_copy"
	StageTCPRead         = "tcp_read"
	StageTCPInterarrival = "tcp_interarrival"
	StageRawKafkaEnqueue = "raw_kafka_enqueue"
	StageRawKafkaPublish = "raw_kafka_publish"
	StageRawKafkaLag     = "raw_kafka_lag"
	StageFrameToParse    = "frame_to_parse"
	StageParse           = "parse"
	StageQuality         = "quality"
	StageTimeAlignPush   = "time_align_push"
	StageTimeAlignWait   = "time_align_wait"
	StageReadingsPublish = "readings_publish"
	StageDashboardRecord = "dashboard_record"
	StageStateSnapshot   = "dashboard_state_json"
	StageSinkStore       = "sink_store"
	StageE2ERecvToDash   = "e2e_recv_to_dashboard"
	StageClockSkewPMU    = "clock_skew_pmu"
	StageE2EPMUToDash    = "e2e_pmu_to_dashboard_raw"
	StageE2EPMUCorrected = "e2e_pmu_to_dashboard"
)

const latencyWindow = 256

type stageSpec struct {
	ID    string
	Label string
	Group string // connection | ingest | process | dashboard | sink | e2e
}

var stageOrder = []stageSpec{
	{StageDial, "TCP dial", "connection"},
	{StageHandshakeHDR, "Handshake HDR wait", "connection"},
	{StageHandshakeCFG2, "Handshake CFG-2 wait", "connection"},
	{StageHandshakeTotal, "Handshake total (one-time)", "connection"},
	{StageTCPWait, "TCP wait for first byte", "idle"},
	{StageTCPCopy, "TCP copy frame bytes", "ingest"},
	{StageTCPRead, "TCP wait+copy (total)", "idle"},
	{StageTCPInterarrival, "TCP complete-to-complete", "idle"},
	{StageRawKafkaEnqueue, "Raw Kafka enqueue", "ingest"},
	{StageRawKafkaPublish, "Raw Kafka broker write", "ingest"},
	{StageRawKafkaLag, "Raw Kafka lag", "ingest"},
	{StageFrameToParse, "Frame complete → parse start", "process"},
	{StageParse, "Parse DATA", "process"},
	{StageQuality, "Quality gate", "process"},
	{StageTimeAlignPush, "Time-align buffer push", "process"},
	{StageTimeAlignWait, "Time-align wait window", "process"},
	{StageReadingsPublish, "Readings Kafka publish", "process"},
	{StageDashboardRecord, "Dashboard RecordReading", "dashboard"},
	{StageStateSnapshot, "Dashboard /state JSON", "dashboard"},
	{StageSinkStore, "Redis + Postgres store", "sink"},
	{StageE2ERecvToDash, "E2E TCP-complete → dashboard", "e2e"},
	{StageClockSkewPMU, "PMU clock skew (receive − SOC)", "clock"},
	{StageE2EPMUToDash, "E2E PMU SOC → dashboard (raw, includes skew)", "clock"},
	{StageE2EPMUCorrected, "E2E PMU → dashboard (skew corrected)", "e2e"},
}

var stageByID = func() map[string]stageSpec {
	m := make(map[string]stageSpec, len(stageOrder))
	for _, s := range stageOrder {
		m[s.ID] = s
	}
	return m
}()

type stageWindow struct {
	samples []float64
	next    int
	filled  int
	count   int64
	maxMs   float64
}

// StageLatency is one pipeline hop as shown on the dashboard.
type StageLatency struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Group  string  `json:"group"`
	LastMs float64 `json:"lastMs"`
	AvgMs  float64 `json:"avgMs"`
	P95Ms  float64 `json:"p95Ms"`
	MaxMs  float64 `json:"maxMs"`
	Count  int64   `json:"count"`
}

// PipelineLatency is the fleet-wide hop breakdown.
type PipelineLatency struct {
	SlowestStage string         `json:"slowestStage"`
	SlowestLabel string         `json:"slowestLabel"`
	SlowestAvgMs float64        `json:"slowestAvgMs"`
	Stages       []StageLatency `json:"stages"`
}

var (
	stageLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pdc_stage_latency_seconds",
			Help:    "Wall time spent in each PDC pipeline stage.",
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
		[]string{"stage"},
	)

	latencyMu  sync.Mutex
	windows    = make(map[string]*stageWindow, len(stageOrder))
	lastGlobal = make(map[string]float64, len(stageOrder))
	lastByPMU  = make(map[string]map[string]float64)
)

func init() {
	prometheus.MustRegister(stageLatency)
	for _, s := range stageOrder {
		windows[s.ID] = &stageWindow{samples: make([]float64, latencyWindow)}
	}
}

// Ms converts a duration to milliseconds (0 if d <= 0).
func Ms(d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(d) / float64(time.Millisecond)
}

// FormatMs is a compact duration for logs and conversation messages.
func FormatMs(d time.Duration) string {
	if d <= 0 {
		return "0ms"
	}
	ms := Ms(d)
	if ms < 1 {
		return fmt.Sprintf("%.2fms", ms)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.1fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// ObserveStage records one timing sample for a pipeline function.
func ObserveStage(pmu, stage string, d time.Duration) {
	if d < 0 || d > 5*time.Minute {
		return
	}
	ms := Ms(d)
	if ms == 0 && d == 0 {
		return
	}

	stageLatency.WithLabelValues(stage).Observe(d.Seconds())

	pmu = strings.TrimSpace(pmu)
	if pmu == "" {
		pmu = "SYSTEM"
	}

	latencyMu.Lock()
	defer latencyMu.Unlock()

	w := windows[stage]
	if w == nil {
		w = &stageWindow{samples: make([]float64, latencyWindow)}
		windows[stage] = w
	}
	w.samples[w.next] = ms
	w.next = (w.next + 1) % latencyWindow
	if w.filled < latencyWindow {
		w.filled++
	}
	w.count++
	if ms > w.maxMs {
		w.maxMs = ms
	}
	lastGlobal[stage] = ms

	hops := lastByPMU[pmu]
	if hops == nil {
		hops = make(map[string]float64, 8)
		lastByPMU[pmu] = hops
	}
	hops[stage] = ms
}

// ApplyTraceHops copies hop times stamped on a reading (from another process)
// into the per-PMU last-hop map without double-counting Prometheus.
func ApplyTraceHops(pmu string, tr parser.LatencyTrace) {
	pmu = strings.TrimSpace(pmu)
	if pmu == "" {
		return
	}

	type pair struct {
		stage string
		ms    float64
	}
	pairs := []pair{
		{StageTCPWait, tr.TcpWaitMs},
		{StageTCPCopy, tr.TcpCopyMs},
		{StageTCPRead, tr.TcpReadMs},
		{StageRawKafkaLag, tr.KafkaLagMs},
		{StageFrameToParse, tr.FrameToParseMs},
		{StageParse, tr.ParseMs},
		{StageQuality, tr.QualityMs},
		{StageReadingsPublish, tr.ReadingsPublishMs},
	}

	latencyMu.Lock()
	defer latencyMu.Unlock()
	hops := lastByPMU[pmu]
	if hops == nil {
		hops = make(map[string]float64, 8)
		lastByPMU[pmu] = hops
	}
	for _, p := range pairs {
		if p.ms > 0 {
			hops[p.stage] = p.ms
		}
	}
}

// LastHopsForPMU returns a copy of the most recent hop times for one PMU.
func LastHopsForPMU(pmu string) map[string]float64 {
	latencyMu.Lock()
	defer latencyMu.Unlock()
	src := lastByPMU[pmu]
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]float64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func copyAllLastHops() map[string]map[string]float64 {
	latencyMu.Lock()
	defer latencyMu.Unlock()
	out := make(map[string]map[string]float64, len(lastByPMU))
	for pmu, hops := range lastByPMU {
		cp := make(map[string]float64, len(hops))
		for k, v := range hops {
			cp[k] = v
		}
		out[pmu] = cp
	}
	return out
}

// SnapshotPipelineLatency returns last/avg/p95/max for every known stage.
func SnapshotPipelineLatency() PipelineLatency {
	latencyMu.Lock()
	defer latencyMu.Unlock()

	stages := make([]StageLatency, 0, len(stageOrder))
	for _, spec := range stageOrder {
		w := windows[spec.ID]
		st := StageLatency{
			ID:     spec.ID,
			Label:  spec.Label,
			Group:  spec.Group,
			LastMs: lastGlobal[spec.ID],
		}
		if w != nil {
			st.Count = w.count
			st.MaxMs = w.maxMs
			st.AvgMs, st.P95Ms = windowStats(w)
		}
		stages = append(stages, st)
	}

	slowID, slowLabel, slowAvg := pickSlowest(stages)
	return PipelineLatency{
		SlowestStage: slowID,
		SlowestLabel: slowLabel,
		SlowestAvgMs: slowAvg,
		Stages:       stages,
	}
}

func windowStats(w *stageWindow) (avg, p95 float64) {
	if w == nil || w.filled == 0 {
		return 0, 0
	}
	n := w.filled
	sum := 0.0
	tmp := make([]float64, n)
	if w.filled < latencyWindow {
		copy(tmp, w.samples[:w.filled])
	} else {
		copy(tmp, w.samples)
	}
	for _, v := range tmp {
		sum += v
	}
	avg = sum / float64(n)
	sort.Float64s(tmp)
	p95 = percentileSorted(tmp, 0.95)
	return avg, p95
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func pickSlowest(stages []StageLatency) (id, label string, avg float64) {
	for _, st := range stages {
		if st.Count == 0 {
			continue
		}
		// Connection = one-time setup. idle = waiting for the next PMU sample.
		// clock = PMU vs PDC wall clock. e2e = end-to-end delivery metrics.
		if st.Group == "connection" || st.Group == "e2e" || st.Group == "idle" || st.Group == "clock" {
			continue
		}
		if st.AvgMs >= avg {
			avg = st.AvgMs
			id = st.ID
			label = st.Label
		}
	}
	return id, label, avg
}

// FormatLatencySummary is a hop summary. Connection (one-time) is a separate
// line from per-frame ingest so handshake_total is never mistaken for fps delay.
func FormatLatencySummary() string {
	snap := SnapshotPipelineLatency()
	var conn, idle, clock, hops []string
	for _, st := range snap.Stages {
		if st.Count == 0 {
			continue
		}
		part := fmt.Sprintf("%s=%.2fms", st.ID, st.AvgMs)
		switch st.Group {
		case "connection":
			conn = append(conn, part)
		case "idle":
			idle = append(idle, part)
		case "clock":
			clock = append(clock, part)
		default:
			hops = append(hops, part)
		}
	}
	if len(conn) == 0 && len(idle) == 0 && len(clock) == 0 && len(hops) == 0 {
		return "[latency] no samples yet"
	}
	slow := "n/a"
	if snap.SlowestStage != "" {
		slow = fmt.Sprintf("%s (avg %.2fms)", snap.SlowestStage, snap.SlowestAvgMs)
	}
	var b strings.Builder
	if len(conn) > 0 {
		b.WriteString("[latency] connection (one-time, not per-frame): ")
		b.WriteString(strings.Join(conn, " "))
		b.WriteByte('\n')
	}
	if len(clock) > 0 {
		b.WriteString("[latency] PMU clock skew (not pipeline delay): ")
		b.WriteString(strings.Join(clock, " "))
		b.WriteByte('\n')
	}
	if len(idle) > 0 {
		b.WriteString("[latency] wait-for-PMU (inter-sample, not processing): ")
		b.WriteString(strings.Join(idle, " "))
		b.WriteByte('\n')
	}
	b.WriteString("[latency] processing slowest=")
	b.WriteString(slow)
	if len(hops) > 0 {
		b.WriteString(" | ")
		b.WriteString(strings.Join(hops, " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// StartLatencyReporter logs a hop summary every interval so the console
// shows which function is dominating without opening the dashboard.
func StartLatencyReporter(ctxDone <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctxDone:
				return
			case <-ticker.C:
				for _, line := range strings.Split(FormatLatencySummary(), "\n") {
					if line != "" {
						log.Print(line)
					}
				}
			}
		}
	}()
}
