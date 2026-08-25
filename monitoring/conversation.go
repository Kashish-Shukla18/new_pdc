package monitoring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"pdc/parser"
)

type ConversationEvent struct {
	Time      time.Time `json:"time"`
	PMU       string    `json:"pmu"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Stage     string    `json:"stage"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Direction string    `json:"direction"`
}

type TrendPoint struct {
	TS            int64   `json:"ts"`
	Frequency     float64 `json:"frequency"`
	FrequencyDev  float64 `json:"frequencyDev"` // Hz relative to CFG FNOM
	MW            float64 `json:"mw"`
	MVAR          float64 `json:"mvar"`
	ROCOF         float64 `json:"rocof"`
	StatDataError bool    `json:"statDataError"`
	VA            float64 `json:"va"`
	VB            float64 `json:"vb"`
	VC            float64 `json:"vc"`
	IA            float64 `json:"ia"`
	IB            float64 `json:"ib"`
	IC            float64 `json:"ic"`
}

type PhasorVector struct {
	Magnitude float64 `json:"magnitude"`
	AngleDeg  float64 `json:"angleDeg"`
}

type PhasorSnapshot struct {
	VA PhasorVector `json:"va"`
	VB PhasorVector `json:"vb"`
	VC PhasorVector `json:"vc"`
	IA PhasorVector `json:"ia"`
	TS int64        `json:"ts"`
}

// FrameStamp is timing/STAT from the latest parsed DATA frame (wire values).
type FrameStamp struct {
	SOC          uint32                `json:"soc"`
	FracSecRaw   uint32                `json:"fracSecRaw"`
	FracSecCount uint32                `json:"fracSecCount"`
	TimeQuality  uint8                 `json:"timeQuality"`
	MsgTQ        parser.MsgTimeQuality `json:"msgTq"`
	Stat         uint16                `json:"stat"`
	StatDetail   parser.STATDecoded    `json:"statDetail"`
	IDCode       uint16                `json:"idCode"`
	SyncWord     uint16                `json:"syncWord"`
	Digital      uint16                `json:"digital"`
	Digitals     []uint16              `json:"digitals,omitempty"`
}

type NamedPhasorView struct {
	Name      string  `json:"name"`
	Magnitude float64 `json:"magnitude"`
	AngleDeg  float64 `json:"angleDeg"`
}

type NamedAnalogView struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type DigitalBitView struct {
	Name string `json:"name"`
	Bit  int    `json:"bit"`
	Set  bool   `json:"set"`
}

// ChannelSnapshot is the full CFG-driven data frame decode for the inspector page.
type ChannelSnapshot struct {
	Phasors     []NamedPhasorView `json:"phasors"`
	Analogs     []NamedAnalogView `json:"analogs"`
	DigitalBits []DigitalBitView  `json:"digitalBits"`
	TS          int64             `json:"ts"`
}

// CFGSummary is a compact view of the CFG-2 profile used by the parser.
type CFGSummary struct {
	Available    bool     `json:"available"`
	SyncWord     uint16   `json:"syncWord"`
	IDCode       uint16   `json:"idCode"`
	Station      string   `json:"station"`
	FnomHz       int      `json:"fnomHz"`
	DataRate     int16    `json:"dataRate"`
	Format       uint16   `json:"format"`
	Polar        bool     `json:"polar"`
	PhFloat      bool     `json:"phFloat"`
	AnFloat      bool     `json:"anFloat"`
	FreqFloat    bool     `json:"freqFloat"`
	Phasors      []string `json:"phasors"`
	Analogs      []string `json:"analogs"`
	DigitalWords int      `json:"digitalWords"`
	CfgCnt       uint16   `json:"cfgCnt"`
	HeaderText   string   `json:"headerText,omitempty"`
}

type PMUState struct {
	Name           string             `json:"name"`
	Connected      bool               `json:"connected"`
	ConnectionText string             `json:"connectionText"`
	LastEventTime  time.Time          `json:"lastEventTime"`
	LastFrameTime  time.Time          `json:"lastFrameTime"`
	LastHandshake  time.Time          `json:"lastHandshake"`
	LastError      string             `json:"lastError"`
	TotalFrames    int64              `json:"totalFrames"`
	ApproxFPS      float64            `json:"approxFps"`
	QualityRejects int64              `json:"qualityRejects"`
	KafkaErrors    int64              `json:"kafkaErrors"`
	SinkErrors     int64              `json:"sinkErrors"`
	SpoolQueued    int64              `json:"spoolQueued"`
	LastReading    TrendPoint         `json:"lastReading"`
	LastPhasor     PhasorSnapshot     `json:"lastPhasor"`
	LastChannels   ChannelSnapshot    `json:"lastChannels"`
	LastFrame      FrameStamp         `json:"lastFrame"`
	CFG            CFGSummary         `json:"cfg"`
	Trends         []TrendPoint       `json:"trends"`
	FnomHz         int                `json:"fnomHz"`
	StatDataError  bool               `json:"statDataError"`
	LastHops       map[string]float64 `json:"lastHops,omitempty"`
}

type DashboardState struct {
	NowUTC     time.Time       `json:"nowUtc"`
	PMUs       []PMUState      `json:"pmus"`
	EventCount int             `json:"eventCount"`
	Latency    PipelineLatency `json:"latency"`
}

type pmuRuntime struct {
	name          string
	lastEventTime time.Time
	lastFrameTime time.Time
	lastHandshake time.Time
	lastError     string
	totalFrames   int64
	qualityReject int64
	kafkaErrors   int64
	sinkErrors    int64
	spoolQueued   int64
	lastPhasor    PhasorSnapshot
	lastChannels  ChannelSnapshot
	lastFrame     FrameStamp
	trends        []TrendPoint
	fnomHz        int
	statDataError bool
	approxFPS     float64
	fpsWindowStart time.Time
	fpsWindowCount int
	lastTrendAt   time.Time
}

const maxConversationEvents = 1000
const maxTrendPoints = 180 // ~18s at 10 Hz dashboard sample rate
const dashboardTrendExport = 120
const trendMinInterval = 100 * time.Millisecond // keep charts smooth without 50–60 Hz SVG load

// trendPhasorMags maps CFG channel names onto VA–IC magnitudes for trend series.
func trendPhasorMags(r parser.Reading) (va, vb, vc, ia, ib, ic float64) {
	va = float64(r.VA.Magnitude)
	vb = float64(r.VB.Magnitude)
	vc = float64(r.VC.Magnitude)
	ia = float64(r.IA.Magnitude)
	for _, p := range r.Phasors {
		n := strings.ToUpper(strings.TrimSpace(p.Name))
		mag := float64(p.Phasor.Magnitude)
		switch {
		case n == "VA" || strings.HasSuffix(n, "AV"):
			va = mag
		case n == "VB" || strings.HasSuffix(n, "BV"):
			vb = mag
		case n == "VC" || strings.HasSuffix(n, "CV"):
			vc = mag
		case n == "IA" || strings.HasSuffix(n, "AI"):
			ia = mag
		case n == "IB" || strings.HasSuffix(n, "BI"):
			ib = mag
		case n == "IC" || strings.HasSuffix(n, "CI"):
			ic = mag
		}
	}
	return
}

// cfgSummaryForPMU builds a dashboard CFG-2 summary from the parser profile registry.
func cfgSummaryForPMU(name string) CFGSummary {
	prof, ok := parser.GetProfile(name)
	if !ok {
		return CFGSummary{Available: false}
	}
	phasors := make([]string, 0, prof.Phnmr)
	analogs := make([]string, 0, prof.Annmr)
	if len(prof.Channels) >= prof.Phnmr {
		phasors = append(phasors, prof.Channels[:prof.Phnmr]...)
	}
	if len(prof.Channels) >= prof.Phnmr+prof.Annmr {
		analogs = append(analogs, prof.Channels[prof.Phnmr:prof.Phnmr+prof.Annmr]...)
	}
	// Prefer actual CFG-2 SYNC from handshake when present.
	sync := prof.SyncWord
	if sync == 0 {
		sync = uint16(0xAA31)
	}
	return CFGSummary{
		Available:    true,
		SyncWord:     sync,
		IDCode:       prof.IDCode,
		Station:      prof.Station,
		FnomHz:       prof.FnomHz,
		DataRate:     prof.DataRate,
		Format:       prof.Format,
		Polar:        prof.Polar,
		PhFloat:      prof.PhFloat,
		AnFloat:      prof.AnFloat,
		FreqFloat:    prof.FreqFloat,
		Phasors:      phasors,
		Analogs:      analogs,
		DigitalWords: prof.Dgnmr,
		CfgCnt:       prof.CfgCnt,
		HeaderText:   strings.TrimSpace(prof.HeaderText),
	}
}

var conversationBus = struct {
	mu          sync.Mutex
	events      []ConversationEvent
	subscribers map[chan ConversationEvent]struct{}
	pmus        map[string]*pmuRuntime
}{
	events:      make([]ConversationEvent, 0, maxConversationEvents),
	subscribers: make(map[chan ConversationEvent]struct{}),
	pmus:        make(map[string]*pmuRuntime),
}

func getOrCreatePMU(name string) *pmuRuntime {
	pmu := strings.TrimSpace(name)
	if pmu == "" {
		pmu = "SYSTEM"
	}
	st, ok := conversationBus.pmus[pmu]
	if !ok {
		st = &pmuRuntime{name: pmu, trends: make([]TrendPoint, 0, maxTrendPoints)}
		conversationBus.pmus[pmu] = st
	}
	return st
}

func RecordReading(r parser.Reading) {
	conversationBus.mu.Lock()

	st := getOrCreatePMU(r.PMUName)
	now := time.Now().UTC()
	prevFrame := st.lastFrameTime
	st.lastEventTime = now
	st.lastFrameTime = now
	st.totalFrames++
	st.statDataError = r.StatDetail.DataError

	fnom := 0
	if prof, ok := parser.GetProfile(r.PMUName); ok && prof.FnomHz > 0 {
		fnom = prof.FnomHz
	}
	st.fnomHz = fnom

	freqDev := float64(r.FrequencyDeviation)
	if fnom > 0 {
		freqDev = float64(r.Frequency) - float64(fnom)
	}

	vaMag, vbMag, vcMag, iaMag, ibMag, icMag := trendPhasorMags(r)

	t := TrendPoint{
		TS:            now.UnixMilli(),
		Frequency:     float64(r.Frequency),
		FrequencyDev:  freqDev,
		MW:            float64(r.MW),
		MVAR:          float64(r.MVAR),
		ROCOF:         float64(r.ROCOF),
		StatDataError: r.StatDetail.DataError,
		VA:            vaMag,
		VB:            vbMag,
		VC:            vcMag,
		IA:            iaMag,
		IB:            ibMag,
		IC:            icMag,
	}

	st.lastPhasor = PhasorSnapshot{
		VA: PhasorVector{Magnitude: float64(r.VA.Magnitude), AngleDeg: float64(r.VA.PhaseDegrees)},
		VB: PhasorVector{Magnitude: float64(r.VB.Magnitude), AngleDeg: float64(r.VB.PhaseDegrees)},
		VC: PhasorVector{Magnitude: float64(r.VC.Magnitude), AngleDeg: float64(r.VC.PhaseDegrees)},
		IA: PhasorVector{Magnitude: float64(r.IA.Magnitude), AngleDeg: float64(r.IA.PhaseDegrees)},
		TS: now.UnixMilli(),
	}

	phasorViews := make([]NamedPhasorView, 0, len(r.Phasors))
	for _, p := range r.Phasors {
		phasorViews = append(phasorViews, NamedPhasorView{
			Name:      p.Name,
			Magnitude: float64(p.Phasor.Magnitude),
			AngleDeg:  float64(p.Phasor.PhaseDegrees),
		})
	}
	if len(phasorViews) == 0 {
		// Fallback for legacy parse path without named phasors.
		phasorViews = []NamedPhasorView{
			{Name: "VA", Magnitude: float64(r.VA.Magnitude), AngleDeg: float64(r.VA.PhaseDegrees)},
			{Name: "VB", Magnitude: float64(r.VB.Magnitude), AngleDeg: float64(r.VB.PhaseDegrees)},
			{Name: "VC", Magnitude: float64(r.VC.Magnitude), AngleDeg: float64(r.VC.PhaseDegrees)},
			{Name: "IA", Magnitude: float64(r.IA.Magnitude), AngleDeg: float64(r.IA.PhaseDegrees)},
		}
	}
	analogViews := make([]NamedAnalogView, 0, len(r.Analogs))
	for _, a := range r.Analogs {
		analogViews = append(analogViews, NamedAnalogView{Name: a.Name, Value: float64(a.Value)})
	}
	bits := make([]DigitalBitView, 0, 16)
	digWord := r.Digital
	for bit := 0; bit < 16; bit++ {
		name := fmt.Sprintf("bit%d", bit)
		if bit < len(r.DigitalNames) && r.DigitalNames[bit] != "" {
			name = r.DigitalNames[bit]
		}
		bits = append(bits, DigitalBitView{
			Name: name,
			Bit:  bit,
			Set:  (digWord>>uint(bit))&1 == 1,
		})
	}
	st.lastChannels = ChannelSnapshot{
		Phasors:     phasorViews,
		Analogs:     analogViews,
		DigitalBits: bits,
		TS:          now.UnixMilli(),
	}
	st.lastFrame = FrameStamp{
		SOC:          r.SOC,
		FracSecRaw:   r.FracSecRaw,
		FracSecCount: r.FracSecCount,
		TimeQuality:  r.TimeQuality,
		MsgTQ:        r.MsgTQ,
		Stat:         r.Stat,
		StatDetail:   r.StatDetail,
		IDCode:       r.IDCode,
		SyncWord:     r.SyncWord,
		Digital:      r.Digital,
		Digitals:     append([]uint16(nil), r.Digitals...),
	}

	// FPS from active-stream windows only. Reconnect gaps must not dilute the rate
	// (otherwise inventory shows ~2 FPS while CFG/tcp_wait say 30/60).
	gap := time.Duration(0)
	if !prevFrame.IsZero() {
		gap = now.Sub(prevFrame)
	}
	if st.fpsWindowStart.IsZero() || gap > 750*time.Millisecond {
		st.fpsWindowStart = now
		st.fpsWindowCount = 1
	} else {
		st.fpsWindowCount++
		if elapsed := now.Sub(st.fpsWindowStart); elapsed >= time.Second {
			instant := float64(st.fpsWindowCount) / elapsed.Seconds()
			if st.approxFPS <= 0 {
				st.approxFPS = instant
			} else {
				st.approxFPS = st.approxFPS*0.35 + instant*0.65
			}
			st.fpsWindowStart = now
			st.fpsWindowCount = 0
		}
	}

	// Downsample dashboard trends (~10 Hz) so fleet charts stay smooth at 100s of PMUs.
	if st.lastTrendAt.IsZero() || now.Sub(st.lastTrendAt) >= trendMinInterval {
		st.lastTrendAt = now
		if len(st.trends) == maxTrendPoints {
			copy(st.trends, st.trends[1:])
			st.trends = st.trends[:maxTrendPoints-1]
		}
		st.trends = append(st.trends, t)
	}
	conversationBus.mu.Unlock()

	NoteFrameDashboard(r.PMUName)
	ApplyTraceHops(r.PMUName, r.Trace)
}

func IncQualityRejectForPMU(pmu string) {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.qualityReject++
	st.lastEventTime = time.Now().UTC()
}

func IncKafkaErrorForPMU(pmu string) {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.kafkaErrors++
	st.lastEventTime = time.Now().UTC()
}

func IncSinkErrorForPMU(pmu string) {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.sinkErrors++
	st.lastEventTime = time.Now().UTC()
}

func IncSpoolQueuedForPMU(pmu string) {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.spoolQueued++
	st.lastEventTime = time.Now().UTC()
}

func AddSpoolQueuedForPMU(pmu string, n int64) {
	if n <= 0 {
		return
	}
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.spoolQueued += n
	st.lastEventTime = time.Now().UTC()
}

func DecSpoolQueuedForPMU(pmu string, n int64) {
	if n <= 0 {
		return
	}
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	st := getOrCreatePMU(pmu)
	st.spoolQueued -= n
	if st.spoolQueued < 0 {
		st.spoolQueued = 0
	}
	st.lastEventTime = time.Now().UTC()
}

func RecordConversation(pmu, from, to, stage, status, message string) {
	if strings.TrimSpace(pmu) == "" {
		pmu = "SYSTEM"
	}
	now := time.Now().UTC()
	e := ConversationEvent{
		Time:      now,
		PMU:       pmu,
		From:      from,
		To:        to,
		Stage:     stage,
		Status:    status,
		Message:   message,
		Direction: fmt.Sprintf("%s -> %s", from, to),
	}

	conversationBus.mu.Lock()
	st := getOrCreatePMU(pmu)
	st.lastEventTime = now
	if stage == "handshake" && strings.EqualFold(status, "ok") {
		st.lastHandshake = now
	}
	if strings.Contains(strings.ToLower(stage), "connect") && strings.EqualFold(status, "error") {
		st.lastError = message
	}
	if strings.Contains(strings.ToLower(status), "error") {
		st.lastError = message
	}

	if len(conversationBus.events) == maxConversationEvents {
		copy(conversationBus.events, conversationBus.events[1:])
		conversationBus.events = conversationBus.events[:maxConversationEvents-1]
	}
	conversationBus.events = append(conversationBus.events, e)

	for ch := range conversationBus.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
	conversationBus.mu.Unlock()
}

func registerConversationHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/conversation", handleConversationPage)
	mux.HandleFunc("/conversation/events", handleConversationEvents)
	mux.HandleFunc("/conversation/recent", handleConversationRecent)
	mux.HandleFunc("/conversation/state", handleConversationState)
	mux.HandleFunc("/conversation/latency", handleConversationLatency)
	mux.HandleFunc("/conversation/frame-diag", handleConversationFrameDiag)
}

func handleConversationPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(conversationPageHTML))
}

func handleConversationRecent(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(snapshotEvents())
}

func handleConversationState(w http.ResponseWriter, _ *http.Request) {
	t0 := time.Now()
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(snapshotDashboard())
	ObserveStage("SYSTEM", StageStateSnapshot, time.Since(t0))
}

func handleConversationLatency(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SnapshotPipelineLatency())
}

func handleConversationEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan ConversationEvent, 128)
	addSubscriber(ch)
	defer removeSubscriber(ch)

	for _, e := range snapshotEvents() {
		writeSSE(w, e)
	}
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case e := <-ch:
			writeSSE(w, e)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, e ConversationEvent) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
}

func snapshotEvents() []ConversationEvent {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	out := make([]ConversationEvent, len(conversationBus.events))
	copy(out, conversationBus.events)
	return out
}

func snapshotDashboard() DashboardState {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()

	now := time.Now().UTC()
	pmus := make([]PMUState, 0, len(conversationBus.pmus))

	for _, st := range conversationBus.pmus {
		if st.name == "SYSTEM" {
			continue
		}
		connected := !st.lastFrameTime.IsZero() && now.Sub(st.lastFrameTime) <= 3*time.Second
		connText := "disconnected"
		if connected {
			connText = "receiving frames"
		} else if !st.lastHandshake.IsZero() {
			connText = "handshake done, waiting for data"
		}

		n := len(st.trends)
		cfg := cfgSummaryForPMU(st.name)
		fps := st.approxFPS
		// Never use downsampled trend spacing for FPS (that caps near ~10 Hz).
		// Prefer measured counter; else CFG-2 DATA_RATE; else leave 0 until hops applied below.
		if fps <= 0 && cfg.DataRate > 0 {
			fps = float64(cfg.DataRate)
		} else if fps <= 0 && cfg.DataRate < 0 {
			fps = 1.0 / float64(-cfg.DataRate)
		}

		last := TrendPoint{}
		if n > 0 {
			last = st.trends[n-1]
		}

		exportN := n
		if exportN > dashboardTrendExport {
			exportN = dashboardTrendExport
		}
		trendCopy := make([]TrendPoint, exportN)
		if exportN > 0 {
			copy(trendCopy, st.trends[n-exportN:])
		}

		pmus = append(pmus, PMUState{
			Name:           st.name,
			Connected:      connected,
			ConnectionText: connText,
			LastEventTime:  st.lastEventTime,
			LastFrameTime:  st.lastFrameTime,
			LastHandshake:  st.lastHandshake,
			LastError:      st.lastError,
			TotalFrames:    st.totalFrames,
			ApproxFPS:      fps,
			QualityRejects: st.qualityReject,
			KafkaErrors:    st.kafkaErrors,
			SinkErrors:     st.sinkErrors,
			SpoolQueued:    st.spoolQueued,
			LastReading:    last,
			LastPhasor:     st.lastPhasor,
			LastChannels:   st.lastChannels,
			LastFrame:      st.lastFrame,
			CFG:            cfg,
			Trends:         trendCopy,
			FnomHz:         st.fnomHz,
			StatDataError:  st.statDataError,
		})
	}

	hops := copyAllLastHops()
	for i := range pmus {
		if h := hops[pmus[i].Name]; len(h) > 0 {
			pmus[i].LastHops = h
		}
		if !pmus[i].Connected {
			continue
		}
		// Fill cold counter from inter-frame wait only. Do not overwrite a real
		// 1s window rate with instantaneous tcp_wait (that inflated FPS vs CFG).
		wait := 0.0
		if h := hops[pmus[i].Name]; len(h) > 0 {
			wait = h["tcp_wait"]
			if wait <= 0 {
				wait = h["tcp_interarrival"]
			}
		}
		cfgRate := float64(pmus[i].CFG.DataRate)
		if cfgRate < 0 {
			cfgRate = 1.0 / float64(-pmus[i].CFG.DataRate)
		}
		if pmus[i].ApproxFPS <= 0 {
			if wait > 1 && wait < 500 {
				pmus[i].ApproxFPS = 1000.0 / wait
			} else if cfgRate > 0 {
				pmus[i].ApproxFPS = cfgRate
			}
		}
	}

	sort.Slice(pmus, func(i, j int) bool {
		return pmus[i].Name < pmus[j].Name
	})

	return DashboardState{
		NowUTC:     now,
		PMUs:       pmus,
		EventCount: len(conversationBus.events),
		Latency:    SnapshotPipelineLatency(),
	}
}

func addSubscriber(ch chan ConversationEvent) {
	conversationBus.mu.Lock()
	conversationBus.subscribers[ch] = struct{}{}
	conversationBus.mu.Unlock()
}

func removeSubscriber(ch chan ConversationEvent) {
	conversationBus.mu.Lock()
	delete(conversationBus.subscribers, ch)
	conversationBus.mu.Unlock()
	close(ch)
}

const conversationPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>PMU-PDC Real-Time Monitor</title>
  <style>
    :root {
      --bg: #f0f6f8;
      --ink: #102333;
      --card: #ffffff;
      --line: #c7d8e0;
      --ok: #0e8a4a;
      --warn: #c17a00;
      --err: #b3261e;
      --accent: #0277a8;
      --muted: #4a6476;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", "Tahoma", sans-serif;
      color: var(--ink);
      background: radial-gradient(circle at 100% 0%, #d9edf6 0%, transparent 35%),
                  radial-gradient(circle at 0% 100%, #e6f5ef 0%, transparent 25%),
                  var(--bg);
      min-height: 100vh;
    }
    .wrap {
      max-width: 1280px;
      margin: 20px auto;
      padding: 0 16px 24px;
    }
    .head {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 12px;
      align-items: center;
      margin-bottom: 12px;
    }
    h1 {
      margin: 0;
      letter-spacing: 0.3px;
      font-size: clamp(1.4rem, 2.3vw, 2.1rem);
    }
    .sub { margin: 6px 0 0; color: var(--muted); }
    .status-chip {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      border: 1px solid var(--line);
      background: var(--card);
      border-radius: 999px;
      font-size: 0.95rem;
    }
    .dot {
      width: 11px;
      height: 11px;
      border-radius: 999px;
      background: #777;
    }
    .topology {
      border: 1px solid var(--line);
      background: var(--card);
      border-radius: 14px;
      padding: 14px;
      margin-bottom: 14px;
      box-shadow: 0 6px 18px rgba(16,35,51,0.06);
    }
    .topology-title {
      margin: 0 0 10px;
      font-size: 1rem;
      color: var(--muted);
    }
    .flow {
      display: grid;
      grid-template-columns: repeat(5, minmax(90px, 1fr));
      gap: 10px;
      align-items: center;
    }
    .node {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #fbfdff;
      padding: 10px;
      min-height: 72px;
    }
    .node h3 {
      margin: 0;
      font-size: 0.92rem;
      font-weight: 700;
    }
    .node p {
      margin: 5px 0 0;
      font-size: 0.84rem;
      color: var(--muted);
    }
    .arrow {
      text-align: center;
      color: var(--accent);
      font-size: 1.4rem;
      font-weight: 700;
    }
    .dash {
      display: grid;
      grid-template-columns: 1.1fr 0.9fr;
      gap: 14px;
    }
    @media (max-width: 920px) {
      .flow { grid-template-columns: 1fr; }
      .arrow { display: none; }
      .dash { grid-template-columns: 1fr; }
      .head { grid-template-columns: 1fr; }
    }
    .panel {
      border: 1px solid var(--line);
      border-radius: 14px;
      background: var(--card);
      padding: 12px;
      box-shadow: 0 8px 20px rgba(16,35,51,0.06);
    }
    .panel h2 {
      margin: 0 0 10px;
      font-size: 1.03rem;
    }
    .pmu-list {
      max-height: 240px;
      overflow: auto;
    }
    .pmu-item {
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 8px;
      margin-bottom: 8px;
      background: #fcfeff;
      cursor: pointer;
    }
    .pmu-item.active {
      border-color: var(--accent);
      box-shadow: 0 0 0 2px rgba(2,119,168,0.12);
    }
    .row {
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: center;
      font-size: 0.85rem;
      color: var(--muted);
    }
    .name {
      font-weight: 700;
      color: var(--ink);
      font-size: 0.94rem;
    }
    .badge {
      border-radius: 999px;
      padding: 2px 8px;
      border: 1px solid var(--line);
      font-size: 0.78rem;
    }
    .badge.ok { border-color: #8fd8b0; color: var(--ok); background: #edf9f1; }
    .badge.err { border-color: #f1b6b2; color: var(--err); background: #fff1f0; }
    .stats {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 8px;
      margin: 10px 0;
    }
    .kpi {
      border: 1px solid var(--line);
      border-radius: 9px;
      padding: 8px;
      background: #fcfeff;
    }
    .kpi .k {
      font-size: 0.76rem;
      color: var(--muted);
    }
    .kpi .v {
      margin-top: 3px;
      font-size: 1rem;
      font-weight: 700;
    }
    .graph-wrap {
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 10px;
      background: #fbfdff;
    }
    .legend {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      font-size: 0.8rem;
      color: var(--muted);
      margin-bottom: 6px;
    }
    .legend span::before {
      content: "";
      display: inline-block;
      width: 10px;
      height: 10px;
      border-radius: 999px;
      margin-right: 6px;
      vertical-align: -1px;
    }
    .lg-freq::before { background: #0277a8; }
    .lg-mw::before { background: #b06a00; }
    .lg-mvar::before { background: #6c47ff; }
    canvas { width: 100%; height: 260px; }
    .stream {
      max-height: 340px;
      overflow: auto;
      padding-right: 4px;
      margin-top: 10px;
    }
    .evt {
      border: 1px solid #d8e3e9;
      border-left: 4px solid var(--accent);
      border-radius: 10px;
      padding: 7px 9px;
      margin-bottom: 7px;
      background: #fff;
    }
    .evt.ok { border-left-color: var(--ok); }
    .evt.warn { border-left-color: var(--warn); }
    .evt.error { border-left-color: var(--err); }
    .evt .msg { font-size: 0.85rem; }
    .evt .time { color: var(--muted); font-size: 0.77rem; }
    .small-note { color: var(--muted); font-size: 0.82rem; }
    @media (max-width: 700px) {
      .stats { grid-template-columns: repeat(2, 1fr); }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="head">
      <div>
        <h1>PMU-PDC Real-Time Connection Monitor</h1>
        <p class="sub">Live handshake status, frame health, and synchrophasor trends.</p>
      </div>
      <div class="status-chip"><span class="dot" id="dot"></span><span id="conn">Connecting...</span></div>
    </div>

    <section class="topology">
      <p class="topology-title">Architecture Path Visibility</p>
      <div class="flow">
        <div class="node"><h3>PMU</h3><p id="nodePmu">waiting</p></div>
        <div class="arrow">→</div>
        <div class="node"><h3>PDC</h3><p id="nodePdc">waiting</p></div>
        <div class="arrow">→</div>
        <div class="node"><h3>Kafka / Redis / Postgres</h3><p id="nodeOut">waiting</p></div>
      </div>
    </section>

    <div class="dash">
      <section class="panel">
        <h2>PMU Connection Health</h2>
        <div class="pmu-list" id="pmuList"></div>
        <p class="small-note">Connected means frame seen in last 3 seconds.</p>
      </section>

      <section class="panel">
        <h2>Selected PMU Live Graph</h2>
        <div class="stats" id="stats"></div>
        <div class="graph-wrap">
          <div class="legend">
            <span class="lg-freq">Frequency (Hz)</span>
            <span class="lg-mw">MW</span>
            <span class="lg-mvar">MVAR</span>
          </div>
          <canvas id="trendCanvas" width="820" height="260"></canvas>
        </div>
      </section>
    </div>

    <section class="panel" style="margin-top:14px;">
      <h2>Conversation Stream</h2>
      <div class="dash">
        <div>
          <div class="small-note">PMU -> PDC</div>
          <div class="stream" id="pmuToPdc"></div>
        </div>
        <div>
          <div class="small-note">PDC -> Internal/Outputs</div>
          <div class="stream" id="pdcToPmu"></div>
        </div>
      </section>
    </section>
  </div>
  <script>
    const pmuToPdc = document.getElementById('pmuToPdc');
    const pdcToPmu = document.getElementById('pdcToPmu');
    const dot = document.getElementById('dot');
    const conn = document.getElementById('conn');
    const pmuList = document.getElementById('pmuList');
    const stats = document.getElementById('stats');
    const trendCanvas = document.getElementById('trendCanvas');
    const nodePmu = document.getElementById('nodePmu');
    const nodePdc = document.getElementById('nodePdc');
    const nodeOut = document.getElementById('nodeOut');

    let selectedPMU = '';
    let dashboard = { pmus: [] };

    function cls(status) {
      const s = (status || '').toLowerCase();
      if (s.includes('error') || s.includes('fail')) return 'error';
      if (s.includes('warn') || s.includes('reject') || s.includes('queued')) return 'warn';
      return 'ok';
    }

    function appendEvent(target, e) {
      const item = document.createElement('article');
      item.className = 'evt ' + cls(e.status);
      const t = new Date(e.time).toLocaleTimeString();
      item.innerHTML = '<div class="time">' + t + ' · ' + (e.pmu || 'SYSTEM') + ' · ' + e.direction + '</div>' +
        '<div class="msg"><strong>' + e.stage + '</strong> [' + e.status + '] - ' + e.message + '</div>';
      target.prepend(item);
      while (target.children.length > 240) {
        target.removeChild(target.lastChild);
      }
    }

    function route(e) {
      if (e.from === 'PMU') {
        appendEvent(pmuToPdc, e);
      } else {
        appendEvent(pdcToPmu, e);
      }
    }

    function n(v, d = 2) {
      if (typeof v !== 'number' || Number.isNaN(v)) return 'n/a';
      return v.toFixed(d);
    }

    function ageText(ts) {
      if (!ts) return 'never';
      const sec = (Date.now() - new Date(ts).getTime()) / 1000;
      if (sec < 1) return 'just now';
      if (sec < 60) return sec.toFixed(1) + 's ago';
      return (sec / 60).toFixed(1) + 'm ago';
    }

    function renderPMUList() {
      pmuList.innerHTML = '';
      if (!dashboard.pmus || dashboard.pmus.length === 0) {
        pmuList.innerHTML = '<div class="small-note">No PMU state yet. Waiting for frames...</div>';
        return;
      }

      if (!selectedPMU || !dashboard.pmus.find(p => p.name === selectedPMU)) {
        selectedPMU = dashboard.pmus[0].name;
      }

      for (const p of dashboard.pmus) {
        const el = document.createElement('div');
        el.className = 'pmu-item' + (p.name === selectedPMU ? ' active' : '');
        const badge = p.connected ? 'ok' : 'err';
        const txt = p.connected ? 'connected' : 'disconnected';
        el.innerHTML =
          '<div class="row"><span class="name">' + p.name + '</span><span class="badge ' + badge + '">' + txt + '</span></div>' +
          '<div class="row"><span>fps ' + n(p.approxFps, 1) + '</span><span>frames ' + p.totalFrames + '</span></div>' +
          '<div class="row"><span>last frame</span><span>' + ageText(p.lastFrameTime) + '</span></div>';
        el.onclick = () => {
          selectedPMU = p.name;
          renderPMUList();
          renderSelected();
        };
        pmuList.appendChild(el);
      }
    }

    function statCard(label, value) {
      return '<div class="kpi"><div class="k">' + label + '</div><div class="v">' + value + '</div></div>';
    }

    function drawLine(ctx, points, color, minY, maxY, w, h, pad) {
      if (points.length < 2) return;
      const minX = points[0].ts;
      const maxX = points[points.length - 1].ts;
      const xSpan = Math.max(1, maxX - minX);
      const ySpan = Math.max(0.0001, maxY - minY);

      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.beginPath();
      points.forEach((pt, i) => {
        const x = pad + ((pt.ts - minX) / xSpan) * (w - pad * 2);
        const y = h - pad - ((pt.v - minY) / ySpan) * (h - pad * 2);
        if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
      });
      ctx.stroke();
    }

    function renderSelected() {
      const p = (dashboard.pmus || []).find(x => x.name === selectedPMU);
      if (!p) {
        stats.innerHTML = '';
        return;
      }

      stats.innerHTML =
        statCard('Connection', p.connected ? 'Healthy' : 'Down') +
        statCard('Frequency', n(p.lastReading.frequency, 3) + ' Hz') +
        statCard('MW', n(p.lastReading.mw, 3)) +
        statCard('MVAR', n(p.lastReading.mvar, 3)) +
        statCard('Quality Rejects', p.qualityRejects) +
        statCard('Kafka Errors', p.kafkaErrors) +
        statCard('Store Errors', p.sinkErrors) +
        statCard('Spool Queued', p.spoolQueued);

      const ctx = trendCanvas.getContext('2d');
      const w = trendCanvas.width;
      const h = trendCanvas.height;
      const pad = 28;

      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = '#f8fcff';
      ctx.fillRect(0, 0, w, h);
      ctx.strokeStyle = '#d0dee6';
      ctx.strokeRect(0.5, 0.5, w - 1, h - 1);

      const tr = p.trends || [];
      if (tr.length < 2) {
        ctx.fillStyle = '#4a6476';
        ctx.font = '13px Segoe UI';
        ctx.fillText('Waiting for enough samples to draw trend...', 20, 28);
        return;
      }

      const freq = tr.map(x => ({ ts: x.ts, v: x.frequency }));
      const mw = tr.map(x => ({ ts: x.ts, v: x.mw }));
      const mvar = tr.map(x => ({ ts: x.ts, v: x.mvar }));
      const all = freq.concat(mw).concat(mvar).map(x => x.v).filter(v => Number.isFinite(v));
      const minY = Math.min.apply(null, all);
      const maxY = Math.max.apply(null, all);
      const margin = (maxY - minY) * 0.08 + 0.001;

      for (let i = 0; i <= 4; i++) {
        const y = pad + ((h - pad * 2) * i / 4);
        ctx.strokeStyle = '#e5edf2';
        ctx.beginPath();
        ctx.moveTo(pad, y);
        ctx.lineTo(w - pad, y);
        ctx.stroke();
      }

      drawLine(ctx, freq, '#0277a8', minY - margin, maxY + margin, w, h, pad);
      drawLine(ctx, mw, '#b06a00', minY - margin, maxY + margin, w, h, pad);
      drawLine(ctx, mvar, '#6c47ff', minY - margin, maxY + margin, w, h, pad);
    }

    function renderTopology() {
      const pmus = dashboard.pmus || [];
      const connected = pmus.filter(x => x.connected).length;
      const total = pmus.length;
      const kafkaErrors = pmus.reduce((s, x) => s + (x.kafkaErrors || 0), 0);
      const sinkErrors = pmus.reduce((s, x) => s + (x.sinkErrors || 0), 0);

      nodePmu.textContent = total === 0 ? 'waiting for PMUs' : (connected + '/' + total + ' connected');
      nodePdc.textContent = total === 0 ? 'idle' : 'tracking ' + total + ' PMU streams';
      nodeOut.textContent = 'kafkaErr=' + kafkaErrors + ' | storeErr=' + sinkErrors;
    }

    async function refreshState() {
      try {
        const resp = await fetch('/conversation/state');
        dashboard = await resp.json();
        renderPMUList();
        renderSelected();
        renderTopology();
      } catch (_) {}
    }

    fetch('/conversation/recent')
      .then(r => r.json())
      .then(events => events.forEach(route))
      .catch(() => {});

    refreshState();
    setInterval(refreshState, 1000);

    const es = new EventSource('/conversation/events');
    es.onopen = () => {
      dot.style.background = '#146c43';
      conn.textContent = 'Live telemetry connected';
    };
    es.onerror = () => {
      dot.style.background = '#a61111';
      conn.textContent = 'Telemetry disconnected - retrying';
    };
    es.onmessage = (msg) => {
      try {
        route(JSON.parse(msg.data));
      } catch (_) {}
    };
  </script>
</body>
</html>`
