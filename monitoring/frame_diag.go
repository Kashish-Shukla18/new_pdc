package monitoring

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"pdc/parser"
)

const maxFrameLossSamples = 32

// FrameLossSample identifies one dropped/rejected frame for investigation.
type FrameLossSample struct {
	Stage        string    `json:"stage"`
	SOC          uint32    `json:"soc"`
	FracSecCount uint32    `json:"fracSecCount"`
	FracSecRaw   uint32    `json:"fracSecRaw,omitempty"`
	Reason       string    `json:"reason"`
	At           time.Time `json:"at"`
}

// FrameDiagSnapshot is a per-PMU funnel of existing pipeline stages.
type FrameDiagSnapshot struct {
	PMU      string    `json:"pmu"`
	Since    time.Time `json:"since"`
	DataRate int16     `json:"dataRate"` // CFG-2 rate when known (fps)

	TCPComplete  int64 `json:"tcpComplete"`  // CRC-verified DATA frames from socket
	CRCFail      int64 `json:"crcFail"`
	HandlerDrop  int64 `json:"handlerDrop"`  // pool full before parse
	ParseOK      int64 `json:"parseOK"`
	ParseFail    int64 `json:"parseFail"`
	QualityOK    int64 `json:"qualityOK"`
	QualityFlag  int64 `json:"qualityFlag"`  // Validate() failed (may still continue)
	QualityDrop  int64 `json:"qualityDrop"`  // actually dropped (DROP_QUALITY_REJECTED)
	Dashboard    int64 `json:"dashboard"`
	KafkaOK      int64 `json:"kafkaOK"`
	KafkaFail    int64 `json:"kafkaFail"`

	Losses []FrameLossSample `json:"losses"`
}

type frameDiagRuntime struct {
	mu       sync.Mutex
	since    time.Time
	tcpOK    int64
	crcFail  int64
	hDrop    int64
	parseOK  int64
	parseFail int64
	qualOK   int64
	qualFlag int64
	qualDrop int64
	dash     int64
	kafkaOK  int64
	kafkaFail int64
	losses   []FrameLossSample
}

var frameDiag = struct {
	mu   sync.Mutex
	pmus map[string]*frameDiagRuntime
}{pmus: make(map[string]*frameDiagRuntime)}

func frameDiagOf(pmu string) *frameDiagRuntime {
	frameDiag.mu.Lock()
	defer frameDiag.mu.Unlock()
	st := frameDiag.pmus[pmu]
	if st == nil {
		st = &frameDiagRuntime{since: time.Now().UTC()}
		frameDiag.pmus[pmu] = st
	}
	return st
}

func (st *frameDiagRuntime) noteLoss(stage, reason string, soc, fracRaw, fracCount uint32) {
	sample := FrameLossSample{
		Stage:        stage,
		SOC:          soc,
		FracSecRaw:   fracRaw,
		FracSecCount: fracCount,
		Reason:       reason,
		At:           time.Now().UTC(),
	}
	if len(st.losses) >= maxFrameLossSamples {
		copy(st.losses, st.losses[1:])
		st.losses = st.losses[:maxFrameLossSamples-1]
	}
	st.losses = append(st.losses, sample)
}

// FrameIDFromRaw extracts SOC / FRACSEC from a CRC-complete C37.118 frame.
func FrameIDFromRaw(raw []byte) (soc, fracRaw, fracCount uint32) {
	if len(raw) < 14 {
		return 0, 0, 0
	}
	soc = binary.BigEndian.Uint32(raw[6:10])
	fracRaw = binary.BigEndian.Uint32(raw[10:14])
	fracCount = fracRaw & 0x00FFFFFF
	return soc, fracRaw, fracCount
}

func NoteFrameTCPComplete(pmu string) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.tcpOK++
	st.mu.Unlock()
}

func NoteFrameCRCFail(pmu, reason string, raw []byte) {
	st := frameDiagOf(pmu)
	soc, fracRaw, fracCount := FrameIDFromRaw(raw)
	st.mu.Lock()
	st.crcFail++
	st.noteLoss("crc", reason, soc, fracRaw, fracCount)
	st.mu.Unlock()
}

func NoteFrameHandlerDrop(pmu string, raw []byte) {
	st := frameDiagOf(pmu)
	soc, fracRaw, fracCount := FrameIDFromRaw(raw)
	st.mu.Lock()
	st.hDrop++
	st.noteLoss("handler", "handler pool full", soc, fracRaw, fracCount)
	st.mu.Unlock()
}

func NoteFrameParseOK(pmu string) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.parseOK++
	st.mu.Unlock()
}

func NoteFrameParseFail(pmu, reason string, raw []byte) {
	st := frameDiagOf(pmu)
	soc, fracRaw, fracCount := FrameIDFromRaw(raw)
	st.mu.Lock()
	st.parseFail++
	st.noteLoss("parse", reason, soc, fracRaw, fracCount)
	st.mu.Unlock()
}

func NoteFrameQualityOK(pmu string) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.qualOK++
	st.mu.Unlock()
}

func NoteFrameQualityFlag(pmu, reason string, r parser.Reading) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.qualFlag++
	st.noteLoss("quality_flag", reason, r.SOC, r.FracSecRaw, r.FracSecCount)
	st.mu.Unlock()
}

func NoteFrameQualityDrop(pmu, reason string, r parser.Reading) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.qualDrop++
	st.noteLoss("quality", reason, r.SOC, r.FracSecRaw, r.FracSecCount)
	st.mu.Unlock()
}

func NoteFrameDashboard(pmu string) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.dash++
	st.mu.Unlock()
}

func NoteFrameKafkaOK(pmu string) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.kafkaOK++
	st.mu.Unlock()
}

func NoteFrameKafkaFail(pmu, reason string, r parser.Reading) {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	st.kafkaFail++
	st.noteLoss("kafka", reason, r.SOC, r.FracSecRaw, r.FracSecCount)
	st.mu.Unlock()
}

func ResetFrameDiag(pmu string) {
	frameDiag.mu.Lock()
	defer frameDiag.mu.Unlock()
	frameDiag.pmus[pmu] = &frameDiagRuntime{since: time.Now().UTC()}
}

func SnapshotFrameDiag(pmu string) FrameDiagSnapshot {
	st := frameDiagOf(pmu)
	st.mu.Lock()
	defer st.mu.Unlock()
	losses := make([]FrameLossSample, len(st.losses))
	copy(losses, st.losses)
	rate := int16(0)
	if prof, ok := parser.GetProfile(pmu); ok {
		rate = prof.DataRate
	}
	return FrameDiagSnapshot{
		PMU:          pmu,
		Since:        st.since,
		DataRate:     rate,
		TCPComplete:  st.tcpOK,
		CRCFail:      st.crcFail,
		HandlerDrop:  st.hDrop,
		ParseOK:      st.parseOK,
		ParseFail:    st.parseFail,
		QualityOK:    st.qualOK,
		QualityFlag:  st.qualFlag,
		QualityDrop:  st.qualDrop,
		Dashboard:    st.dash,
		KafkaOK:      st.kafkaOK,
		KafkaFail:    st.kafkaFail,
		Losses:       losses,
	}
}

func handleConversationFrameDiag(w http.ResponseWriter, r *http.Request) {
	pmu := strings.TrimSpace(r.URL.Query().Get("pmu"))
	if pmu == "" {
		http.Error(w, "missing ?pmu=", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("reset") == "1" || r.Method == http.MethodPost {
		ResetFrameDiag(pmu)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SnapshotFrameDiag(pmu))
}
