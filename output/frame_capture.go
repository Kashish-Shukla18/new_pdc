// Package output keeps a short in-memory tape of recent DATA frames.
//
// Think of it like a DVR: the last ~1500 frames stay in RAM so you can
// dump them to CSV. Nothing here is written to a database.
package output

import (
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pdc/parser"
)

// capturedFrame = one raw frame + the numbers we parsed from it.
type capturedFrame struct {
	PMUName    string
	CapturedAt time.Time
	SOC        uint32
	FracRaw    uint32
	FracCount  uint32
	LagMs      float64 // how late the frame arrived vs its own timestamp
	RawHex     string
	RawBytes   int
	Reading    parser.Reading
}

// jumpStat counts weird jumps in timestamps (gaps / backwards time).
type jumpStat struct {
	count      int64
	maxGapMs   float64
	lastGapMs  float64
	lastFrom   time.Time
	lastTo     time.Time
	lastLogged time.Time
}

type frameCaptureStore struct {
	mu        sync.Mutex
	buf       []capturedFrame
	limit     int
	lastByPMU map[string]capturedFrame
	jumps     map[string]*jumpStat
}

var frameCapture = frameCaptureStore{
	lastByPMU: make(map[string]capturedFrame),
	jumps:     make(map[string]*jumpStat),
}

const (
	jumpForwardMs  = 100 // more than two 50 ms periods → dropped frames
	jumpBackwardMs = -10 // time went backwards — should not happen
	jumpLogEvery   = 30 * time.Second
)

// ConfigureFrameCapture sets how many frames the tape holds (default 1500).
func ConfigureFrameCapture(limit int) {
	if limit < 1 {
		limit = 1500
	}
	frameCapture.mu.Lock()
	frameCapture.limit = limit
	frameCapture.mu.Unlock()
}

func decodeWireTime(raw []byte) (soc, fracRaw, fracCount uint32, ok bool) {
	if len(raw) < 14 {
		return 0, 0, 0, false
	}
	soc = binary.BigEndian.Uint32(raw[6:10])
	fracRaw = binary.BigEndian.Uint32(raw[10:14])
	fracCount = fracRaw & 0x00FFFFFF
	return soc, fracRaw, fracCount, true
}

func makeEntry(pmuName string, raw []byte, reading parser.Reading) capturedFrame {
	now := time.Now().UTC()
	soc, fracRaw, fracCount, _ := decodeWireTime(raw)
	lagMs := 0.0
	if !reading.Timestamp.IsZero() {
		lagMs = now.Sub(reading.Timestamp.UTC()).Seconds() * 1000
	}
	return capturedFrame{
		PMUName:    pmuName,
		CapturedAt: now,
		SOC:        soc,
		FracRaw:    fracRaw,
		FracCount:  fracCount,
		LagMs:      lagMs,
		RawHex:     hex.EncodeToString(raw),
		RawBytes:   len(raw),
		Reading:    reading,
	}
}

// RecordFrame stores one frame right away (tests / direct calls).
func RecordFrame(pmuName string, raw []byte, reading parser.Reading) {
	if len(raw) == 0 {
		return
	}
	recordLocked(makeEntry(pmuName, raw, reading))
}

var (
	frameCaptureOnce sync.Once
	frameCaptureCh   chan capturedFrame
)

func startWorker() {
	frameCaptureOnce.Do(func() {
		cap := 8192
		if v := strings.TrimSpace(os.Getenv("FRAME_CAPTURE_QUEUE")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cap = n
			}
		}
		frameCaptureCh = make(chan capturedFrame, cap)
		go func() {
			for entry := range frameCaptureCh {
				recordLocked(entry)
			}
		}()
	})
}

// RecordFrameAsync remembers a frame without slowing down the parse path.
// If the queue is full we drop the sample (live data still flows).
func RecordFrameAsync(pmuName string, raw []byte, reading parser.Reading) {
	if len(raw) == 0 {
		return
	}
	startWorker()
	select {
	case frameCaptureCh <- makeEntry(pmuName, raw, reading):
	default:
	}
}

func recordLocked(entry capturedFrame) {
	frameCapture.mu.Lock()
	defer frameCapture.mu.Unlock()

	ts := entry.Reading.Timestamp
	if prev, ok := frameCapture.lastByPMU[entry.PMUName]; ok && !prev.Reading.Timestamp.IsZero() && !ts.IsZero() {
		if gap := ts.Sub(prev.Reading.Timestamp).Seconds() * 1000; gap > jumpForwardMs || gap < jumpBackwardMs {
			noteStampJumpLocked(entry.PMUName, gap, prev.Reading.Timestamp.UTC(), ts.UTC())
		}
	}
	frameCapture.lastByPMU[entry.PMUName] = entry

	limit := frameCapture.limit
	if limit < 1 {
		limit = 1500
	}
	frameCapture.buf = append(frameCapture.buf, entry)
	if len(frameCapture.buf) > limit {
		frameCapture.buf = frameCapture.buf[len(frameCapture.buf)-limit:]
	}
}

func noteStampJumpLocked(pmuName string, gapMs float64, from, to time.Time) {
	st := frameCapture.jumps[pmuName]
	if st == nil {
		st = &jumpStat{}
		frameCapture.jumps[pmuName] = st
	}
	st.count++
	st.lastGapMs = gapMs
	st.lastFrom, st.lastTo = from, to
	if gapMs > st.maxGapMs {
		st.maxGapMs = gapMs
	}
	if !st.lastLogged.IsZero() && time.Since(st.lastLogged) < jumpLogEvery {
		return
	}
	st.lastLogged = time.Now()
	log.Printf("[frame-capture] %s: %d stamp gap(s) since start, max=%.0fms latest=%.0fms (%s -> %s)",
		pmuName, st.count, st.maxGapMs, st.lastGapMs,
		from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
}

type rawCaptureOut struct {
	Index      int       `json:"index"`
	PMUName    string    `json:"pmu_name"`
	CapturedAt time.Time `json:"captured_at"`
	SOC        uint32    `json:"soc"`
	FracRaw    uint32    `json:"fracsec_raw"`
	FracCount  uint32    `json:"fracsec_count"`
	LagMs      float64   `json:"lag_ms"`
	RawHex     string    `json:"raw_hex"`
	RawBytes   int       `json:"raw_bytes"`
}

type parsedCaptureOut struct {
	Index       int       `json:"index"`
	Time        time.Time `json:"time"`
	PMUName     string    `json:"pmu_name"`
	IDCode      uint16    `json:"idcode"`
	SOC         uint32    `json:"soc"`
	FracCount   uint32    `json:"fracsec_count"`
	CapturedAt  time.Time `json:"captured_at"`
	LagMs       float64   `json:"lag_ms"`
	Freq        float32   `json:"freq"`
	FreqDev     float32   `json:"freq_dev"`
	ROCOF       float32   `json:"rocof"`
	MW          float32   `json:"mw"`
	MVAR        float32   `json:"mvar"`
	MVA         float32   `json:"mva"`
	PowerFactor float32   `json:"power_factor"`
	VAMag       float32   `json:"va_mag"`
	VAAng       float32   `json:"va_ang"`
	VBMag       float32   `json:"vb_mag"`
	VBAng       float32   `json:"vb_ang"`
	VCMag       float32   `json:"vc_mag"`
	VCAng       float32   `json:"vc_ang"`
	IAMag       float32   `json:"ia_mag"`
	IAAng       float32   `json:"ia_ang"`
	Stat        uint16    `json:"stat"`
	Digital     uint16    `json:"digital"`
	CRCValid    bool      `json:"crc_valid"`
	TimeQuality uint8     `json:"time_quality"`
}

func frameToParsedOut(idx int, f capturedFrame) parsedCaptureOut {
	r := f.Reading
	return parsedCaptureOut{
		Index: idx, Time: r.Timestamp, PMUName: r.PMUName, IDCode: r.IDCode,
		SOC: f.SOC, FracCount: f.FracCount, CapturedAt: f.CapturedAt, LagMs: f.LagMs,
		Freq: r.Frequency, FreqDev: r.FrequencyDeviation, ROCOF: r.ROCOF,
		MW: r.MW, MVAR: r.MVAR, MVA: r.MVA, PowerFactor: r.PowerFactor,
		VAMag: r.VA.Magnitude, VAAng: r.VA.PhaseDegrees,
		VBMag: r.VB.Magnitude, VBAng: r.VB.PhaseDegrees,
		VCMag: r.VC.Magnitude, VCAng: r.VC.PhaseDegrees,
		IAMag: r.IA.Magnitude, IAAng: r.IA.PhaseDegrees,
		Stat: r.Stat, Digital: r.Digital, CRCValid: r.ChecksumValid, TimeQuality: r.TimeQuality,
	}
}

// SnapshotFrames returns up to count recent frames (newest last).
func SnapshotFrames(count int, pmuFilter string) (raw []rawCaptureOut, parsed []parsedCaptureOut) {
	frameCapture.mu.Lock()
	defer frameCapture.mu.Unlock()

	src := frameCapture.buf
	if pmuFilter = strings.TrimSpace(pmuFilter); pmuFilter != "" {
		filtered := make([]capturedFrame, 0, len(src))
		for _, f := range src {
			if f.PMUName == pmuFilter {
				filtered = append(filtered, f)
			}
		}
		src = filtered
	}
	if count <= 0 || count > len(src) {
		count = len(src)
	}
	if count == 0 {
		return nil, nil
	}
	src = src[len(src)-count:]

	raw = make([]rawCaptureOut, 0, len(src))
	parsed = make([]parsedCaptureOut, 0, len(src))
	for i, f := range src {
		idx := i + 1
		raw = append(raw, rawCaptureOut{
			Index: idx, PMUName: f.PMUName, CapturedAt: f.CapturedAt,
			SOC: f.SOC, FracRaw: f.FracRaw, FracCount: f.FracCount, LagMs: f.LagMs,
			RawHex: f.RawHex, RawBytes: f.RawBytes,
		})
		parsed = append(parsed, frameToParsedOut(idx, f))
	}
	return raw, parsed
}

func writeRawCSV(path string, frames []rawCaptureOut) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{
		"index", "pmu_name", "captured_at", "soc", "fracsec_raw", "fracsec_count", "lag_ms", "raw_hex", "raw_bytes",
	}); err != nil {
		return err
	}
	for _, r := range frames {
		if err := w.Write([]string{
			strconv.Itoa(r.Index), r.PMUName, r.CapturedAt.UTC().Format(time.RFC3339Nano),
			strconv.FormatUint(uint64(r.SOC), 10), strconv.FormatUint(uint64(r.FracRaw), 10),
			strconv.FormatUint(uint64(r.FracCount), 10), strconv.FormatFloat(r.LagMs, 'f', 3, 64),
			r.RawHex, strconv.Itoa(r.RawBytes),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeParsedCSV(path string, rows []parsedCaptureOut) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{
		"index", "time", "pmu_name", "idcode", "soc", "fracsec_count", "captured_at", "lag_ms",
		"freq", "freq_dev", "rocof", "mw", "mvar", "mva", "power_factor",
		"va_mag", "va_ang", "vb_mag", "vb_ang", "vc_mag", "vc_ang", "ia_mag", "ia_ang",
		"stat", "digital", "crc_valid", "time_quality",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			strconv.Itoa(r.Index), r.Time.UTC().Format(time.RFC3339Nano), r.PMUName,
			strconv.FormatUint(uint64(r.IDCode), 10), strconv.FormatUint(uint64(r.SOC), 10),
			strconv.FormatUint(uint64(r.FracCount), 10), r.CapturedAt.UTC().Format(time.RFC3339Nano),
			strconv.FormatFloat(r.LagMs, 'f', 3, 64),
			strconv.FormatFloat(float64(r.Freq), 'f', 6, 64),
			strconv.FormatFloat(float64(r.FreqDev), 'f', 6, 64),
			strconv.FormatFloat(float64(r.ROCOF), 'f', 6, 64),
			strconv.FormatFloat(float64(r.MW), 'f', 6, 64),
			strconv.FormatFloat(float64(r.MVAR), 'f', 6, 64),
			strconv.FormatFloat(float64(r.MVA), 'f', 6, 64),
			strconv.FormatFloat(float64(r.PowerFactor), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VAMag), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VAAng), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VBMag), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VBAng), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VCMag), 'f', 6, 64),
			strconv.FormatFloat(float64(r.VCAng), 'f', 6, 64),
			strconv.FormatFloat(float64(r.IAMag), 'f', 6, 64),
			strconv.FormatFloat(float64(r.IAAng), 'f', 6, 64),
			strconv.Itoa(int(r.Stat)), strconv.Itoa(int(r.Digital)),
			strconv.FormatBool(r.CRCValid), strconv.Itoa(int(r.TimeQuality)),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

// DumpFrames writes raw + parsed CSV files from the in-memory tape.
func DumpFrames(count int, pmuFilter, rawPath, parsedPath string) (int, error) {
	raw, parsed := SnapshotFrames(count, pmuFilter)
	if len(raw) == 0 {
		return 0, fmt.Errorf("no captured frames in buffer (wait for PDC to receive data)")
	}
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(parsedPath), 0o755); err != nil {
		return 0, err
	}
	if err := writeRawCSV(rawPath, raw); err != nil {
		return 0, fmt.Errorf("write raw csv: %w", err)
	}
	if err := writeParsedCSV(parsedPath, parsed); err != nil {
		return 0, fmt.Errorf("write parsed csv: %w", err)
	}
	log.Printf("[frame-capture] dumped count=%d raw=%s parsed=%s", len(raw), rawPath, parsedPath)
	return len(raw), nil
}

// FrameCaptureStatus reports how full the tape is.
func FrameCaptureStatus() map[string]any {
	frameCapture.mu.Lock()
	defer frameCapture.mu.Unlock()
	limit := frameCapture.limit
	if limit < 1 {
		limit = 1500
	}
	perPMU := make(map[string]int)
	for _, f := range frameCapture.buf {
		perPMU[f.PMUName]++
	}
	return map[string]any{
		"limit":           limit,
		"mode":            "combined_arrival",
		"buffered":        len(frameCapture.buf),
		"buffered_by_pmu": perPMU,
		"format":          "csv",
		"default_count":   1500,
		"default_raw":     filepath.Join("data", "last_1500_combined_raw.csv"),
		"default_parsed":  filepath.Join("data", "last_1500_combined_parsed.csv"),
	}
}

// RegisterFrameCaptureHandler exposes dump / status HTTP endpoints.
func RegisterFrameCaptureHandler(mux *http.ServeMux) {
	mux.HandleFunc("/conversation/frame-capture/status", handleFrameCaptureStatus)
	mux.HandleFunc("/conversation/frame-capture/dump", handleFrameCaptureDump)
}

func handleFrameCaptureStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(FrameCaptureStatus())
}

func handleFrameCaptureDump(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	count := 1500
	if v := strings.TrimSpace(q.Get("count")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}
	pmu := q.Get("pmu")
	rawOut := strings.TrimSpace(q.Get("raw_out"))
	parsedOut := strings.TrimSpace(q.Get("parsed_out"))
	if rawOut == "" {
		rawOut = filepath.Join("data", "last_1500_combined_raw.csv")
	}
	if parsedOut == "" {
		parsedOut = filepath.Join("data", "last_1500_combined_parsed.csv")
	}

	n, err := DumpFrames(count, pmu, rawOut, parsedOut)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	status := FrameCaptureStatus()
	warn := ""
	if n < count {
		warn = fmt.Sprintf("only %d frames in buffer (limit=%v); wait longer or lower -count", n, status["limit"])
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "format": "csv", "requested": count, "count": n,
		"buffer_limit": status["limit"], "buffered": status["buffered"],
		"buffered_by_pmu": status["buffered_by_pmu"],
		"raw_file": rawOut, "parsed_file": parsedOut, "warning": warn,
	})
}
