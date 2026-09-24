package monitoring

// conversation.go — live "what each PMU is doing" state for the dashboard (JSON / SSE).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// AlignedPoint is one PMU's contribution inside a locked aligned dashboard batch.
type AlignedPoint struct {
	Frequency    float64 `json:"frequency"`
	FrequencyDev float64 `json:"frequencyDev"`
	ROCOF        float64 `json:"rocof"`
	VA           float64 `json:"va"`
	VB           float64 `json:"vb"`
	VC           float64 `json:"vc"`
	IA           float64 `json:"ia"`
	IB           float64 `json:"ib"`
	IC           float64 `json:"ic"`
	VAAngle      float64 `json:"vaAngle"`
	VBAngle      float64 `json:"vbAngle"`
	VCAngle      float64 `json:"vcAngle"`
	IAAngle      float64 `json:"iaAngle"`
	// Analogs keyed by CFG-2 channel names (e.g. Analog1) — no invented labels.
	Analogs map[string]float64 `json:"analogs,omitempty"`
}

// AlignedBatch is a locked SOC/FRACSEC slot for analytics charts.
type AlignedBatch struct {
	TS       int64                   `json:"ts"`
	Points   map[string]AlignedPoint `json:"points"`
	Missing  []string                `json:"missing"`
	Complete bool                    `json:"complete"`
	Reason   string                  `json:"reason"`
}

// AlignerStatus is the live auto-tune config + emit counters for testing.
type AlignerStatus struct {
	N           int     `json:"n"`
	FPS         float64 `json:"fps"`
	PeriodMs    float64 `json:"periodMs"`
	WaitMs      float64 `json:"waitMs"`
	MaxOpen     int     `json:"maxOpen"`
	FreshMs     float64 `json:"freshMs"`
	Expected    []string `json:"expected,omitempty"`
	Emitted     int64   `json:"emitted"`
	Complete    int64   `json:"complete"`
	Timeout     int64   `json:"timeout"`
	Cap         int64   `json:"cap"`
	CompletePct float64 `json:"completePct"`
}

type DashboardState struct {
	NowUTC         time.Time       `json:"nowUtc"`
	PMUs           []PMUState      `json:"pmus"`
	EventCount     int             `json:"eventCount"`
	Latency        PipelineLatency `json:"latency"`
	Aligner        AlignerStatus   `json:"aligner"`
	AlignedBatches []AlignedBatch  `json:"alignedBatches,omitempty"`
}

type pmuRuntime struct {
	name           string
	lastEventTime  time.Time
	lastFrameTime  time.Time
	lastHandshake  time.Time
	lastError      string
	totalFrames    int64
	qualityReject  int64
	sinkErrors     int64
	spoolQueued    int64
	lastPhasor     PhasorSnapshot
	lastChannels   ChannelSnapshot
	lastFrame      FrameStamp
	trends         []TrendPoint
	fnomHz         int
	statDataError  bool
	approxFPS      float64
	fpsWindowStart time.Time
	fpsWindowCount int
	lastFPSKey     string // last SOC|FRACSEC counted toward approxFPS (dedupe wire repeats)
	lastTrendAt    time.Time
}

const maxConversationEvents = 1000
const maxTrendPoints = 180 // ~18s at 10 Hz dashboard sample rate
const dashboardTrendExport = 120
const trendMinInterval = 100 * time.Millisecond // keep charts smooth without 50–60 Hz SVG load
const maxAlignedBatches = 180                    // ~9s at 20 FPS aligned slots
const alignedBatchExport = 120
const alignerSummaryEvery = 5 * time.Second // rate-limit [aligner] summary logs

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
	mu             sync.Mutex
	events         []ConversationEvent
	subscribers    map[chan ConversationEvent]struct{}
	pmus           map[string]*pmuRuntime
	alignedBatches []AlignedBatch

	// Auto-tune snapshot + emit counters (for /conversation/state + periodic logs).
	alignN        int
	alignFPS      float64
	alignPeriod   time.Duration
	alignWait     time.Duration
	alignMaxOpen  int
	alignFresh    time.Duration
	alignExpected []string
	alignEmitted  int64
	alignComplete int64
	alignTimeout  int64
	alignCap      int64
	alignLastLog  time.Time
}{
	events:         make([]ConversationEvent, 0, maxConversationEvents),
	subscribers:    make(map[chan ConversationEvent]struct{}),
	pmus:           make(map[string]*pmuRuntime),
	alignedBatches: make([]AlignedBatch, 0, maxAlignedBatches),
}

var (
	dashboardCh   chan parser.Reading
	dashboardOnce sync.Once
)

// StartDashboardWorkers drains dashboard updates off the parse/handler hot path.
func StartDashboardWorkers(ctx context.Context, queueCap, workers int) {
	if queueCap < 256 {
		queueCap = 256
	}
	if workers < 1 {
		workers = 1
	}
	dashboardOnce.Do(func() {
		dashboardCh = make(chan parser.Reading, queueCap)
		for i := 0; i < workers; i++ {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case r := <-dashboardCh:
						recordReadingSync(r)
					}
				}
			}()
		}
	})
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
	if dashboardCh == nil {
		recordReadingSync(r)
		return
	}
	select {
	case dashboardCh <- r:
	default:
		IncDashboardQueueDropped()
	}
}

// SetAlignerTune records the auto-tuned aligner parameters after SetExpected.
func SetAlignerTune(n int, fps float64, period, wait, fresh time.Duration, maxOpen int, expected []string) {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	conversationBus.alignN = n
	conversationBus.alignFPS = fps
	conversationBus.alignPeriod = period
	conversationBus.alignWait = wait
	conversationBus.alignFresh = fresh
	conversationBus.alignMaxOpen = maxOpen
	conversationBus.alignExpected = append([]string(nil), expected...)
	conversationBus.alignEmitted = 0
	conversationBus.alignComplete = 0
	conversationBus.alignTimeout = 0
	conversationBus.alignCap = 0
	conversationBus.alignLastLog = time.Time{}
}

// RecordAlignedFrame stores one locked dashboard alignment batch for analytics charts.
// Inventory / FPS still come from RecordReading on the raw path.
func RecordAlignedFrame(tsMs int64, present map[string]parser.Reading, missing []string, complete bool, reason string) {
	points := make(map[string]AlignedPoint, len(present))
	for name, r := range present {
		fnom := 0.0
		if prof, ok := parser.GetProfile(name); ok && prof.FnomHz > 0 {
			fnom = float64(prof.FnomHz)
		}
		va, vb, vc, ia, ib, ic := trendPhasorMags(r)
		freq := float64(r.Frequency)
		freqDev := float64(r.FrequencyDeviation)
		if fnom > 0 {
			freqDev = freq - fnom
		}
		analogs := make(map[string]float64, len(r.Analogs))
		for _, a := range r.Analogs {
			if a.Name == "" {
				continue
			}
			analogs[a.Name] = float64(a.Value)
		}
		points[name] = AlignedPoint{
			Frequency:    freq,
			FrequencyDev: freqDev,
			ROCOF:        float64(r.ROCOF),
			VA:           va,
			VB:           vb,
			VC:           vc,
			IA:           ia,
			IB:           ib,
			IC:           ic,
			VAAngle:      float64(r.VA.PhaseDegrees),
			VBAngle:      float64(r.VB.PhaseDegrees),
			VCAngle:      float64(r.VC.PhaseDegrees),
			IAAngle:      float64(r.IA.PhaseDegrees),
			Analogs:      analogs,
		}
	}
	missCopy := append([]string(nil), missing...)

	conversationBus.mu.Lock()
	if len(conversationBus.alignedBatches) >= maxAlignedBatches {
		copy(conversationBus.alignedBatches, conversationBus.alignedBatches[1:])
		conversationBus.alignedBatches = conversationBus.alignedBatches[:maxAlignedBatches-1]
	}
	conversationBus.alignedBatches = append(conversationBus.alignedBatches, AlignedBatch{
		TS:       tsMs,
		Points:   points,
		Missing:  missCopy,
		Complete: complete,
		Reason:   reason,
	})
	conversationBus.alignEmitted++
	switch reason {
	case "complete":
		conversationBus.alignComplete++
	case "timeout":
		conversationBus.alignTimeout++
	case "cap":
		conversationBus.alignCap++
	}
	emitted := conversationBus.alignEmitted
	completeN := conversationBus.alignComplete
	timeoutN := conversationBus.alignTimeout
	capN := conversationBus.alignCap
	n := conversationBus.alignN
	wait := conversationBus.alignWait
	fps := conversationBus.alignFPS
	maxOpen := conversationBus.alignMaxOpen
	shouldLog := conversationBus.alignLastLog.IsZero() || time.Since(conversationBus.alignLastLog) >= alignerSummaryEvery
	if shouldLog {
		conversationBus.alignLastLog = time.Now()
	}
	conversationBus.mu.Unlock()

	if !shouldLog {
		return
	}
	pct := 0.0
	if emitted > 0 {
		pct = 100 * float64(completeN) / float64(emitted)
	}
	msg := fmt.Sprintf("summary N=%d fps=%.1f wait=%s max_open=%d emitted=%d complete=%d (%.1f%%) timeout=%d cap=%d",
		n, fps, wait, maxOpen, emitted, completeN, pct, timeoutN, capN)
	log.Printf("[aligner] %s", msg)
	RecordConversation("SYSTEM", "PDC", "ALIGN", "summary", "ok", msg)
}

// ClearAlignedBatches drops dashboard alignment history (PMU set change / restart).
func ClearAlignedBatches() {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	conversationBus.alignedBatches = conversationBus.alignedBatches[:0]
	conversationBus.alignEmitted = 0
	conversationBus.alignComplete = 0
	conversationBus.alignTimeout = 0
	conversationBus.alignCap = 0
	conversationBus.alignLastLog = time.Time{}
}

func recordReadingSync(r parser.Reading) {
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

	measurementTS := now.UnixMilli()
	if !r.Timestamp.IsZero() {
		measurementTS = r.Timestamp.UnixMilli()
	}

	freqDev := float64(r.FrequencyDeviation)
	if fnom > 0 {
		freqDev = float64(r.Frequency) - float64(fnom)
	}

	vaMag, vbMag, vcMag, iaMag, ibMag, icMag := trendPhasorMags(r)

	t := TrendPoint{
		TS:            measurementTS,
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
		TS: measurementTS,
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
		TS:          measurementTS,
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

	// FPS = unique measurement stamps / sec (SOC+FRACSEC). Some devices/sims
	// emit several identical DATA frames per reporting instant; counting every
	// TCP frame then shows e.g. 100 / 25 while CFG DATA_RATE is 25.
	gap := time.Duration(0)
	if !prevFrame.IsZero() {
		gap = now.Sub(prevFrame)
	}
	stampKey := fmt.Sprintf("%d:%d", r.SOC, r.FracSecCount)
	newStamp := stampKey != st.lastFPSKey
	if newStamp {
		st.lastFPSKey = stampKey
	}
	if st.fpsWindowStart.IsZero() || gap > 750*time.Millisecond {
		st.fpsWindowStart = now
		if newStamp {
			st.fpsWindowCount = 1
		} else {
			st.fpsWindowCount = 0
		}
	} else if newStamp {
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
	// aligner-buffers is registered from main (needs the live Bank)
}

// handleConversationPage used to serve a standalone HTML monitor. The React app
// in dashboard/ now renders the same data from the JSON endpoints below, so this
// only points callers at them rather than maintaining a second UI.
func handleConversationPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, strings.Join([]string{
		"PDC telemetry endpoints (UI lives in the dashboard/ React app):",
		"  /conversation/state       dashboard snapshot (PMUs, live inventory)",
		"  /conversation/events      SSE live event stream",
		"  /conversation/recent      recent events, one-shot",
		"  /conversation/latency     pipeline stage latency",
		"  /conversation/frame-diag  per-stage frame accounting",
		"  /conversation/aligner-buffers  time-align buffers (built separately)",
		"  /metrics                  Prometheus metrics",
	}, "\n")+"\n")
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

	pct := 0.0
	if conversationBus.alignEmitted > 0 {
		pct = 100 * float64(conversationBus.alignComplete) / float64(conversationBus.alignEmitted)
	}
	alignStatus := AlignerStatus{
		N:           conversationBus.alignN,
		FPS:         conversationBus.alignFPS,
		PeriodMs:    float64(conversationBus.alignPeriod) / float64(time.Millisecond),
		WaitMs:      float64(conversationBus.alignWait) / float64(time.Millisecond),
		MaxOpen:     conversationBus.alignMaxOpen,
		FreshMs:     float64(conversationBus.alignFresh) / float64(time.Millisecond),
		Expected:    append([]string(nil), conversationBus.alignExpected...),
		Emitted:     conversationBus.alignEmitted,
		Complete:    conversationBus.alignComplete,
		Timeout:     conversationBus.alignTimeout,
		Cap:         conversationBus.alignCap,
		CompletePct: pct,
	}
	alignedCopy := make([]AlignedBatch, len(conversationBus.alignedBatches))
	copy(alignedCopy, conversationBus.alignedBatches)
	if len(alignedCopy) > alignedBatchExport {
		alignedCopy = alignedCopy[len(alignedCopy)-alignedBatchExport:]
	}

	return DashboardState{
		NowUTC:         now,
		PMUs:           pmus,
		EventCount:     len(conversationBus.events),
		Latency:        SnapshotPipelineLatency(),
		Aligner:        alignStatus,
		AlignedBatches: alignedCopy,
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
