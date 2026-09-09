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

	"pdc/output/postgres"
)

// capturedFrame pairs one raw C37.118 DATA frame with the postgres row enqueued for history.
type capturedFrame struct {
	PMUName    string    `json:"pmu_name"`
	CapturedAt time.Time `json:"captured_at"`
	SOC        uint32    `json:"soc"`
	FracRaw    uint32    `json:"fracsec_raw"`
	FracCount  uint32    `json:"fracsec_count"`
	LagMs      float64   `json:"lag_ms"`
	RawHex     string    `json:"raw_hex"`
	RawBytes   int       `json:"raw_bytes"`
	Row        postgres.Row
}

type frameCaptureStore struct {
	mu        sync.Mutex
	buf       []capturedFrame // last N frames in arrival order (all PMUs combined)
	limit     int
	lastByPMU map[string]capturedFrame
}

var frameCapture = frameCaptureStore{
	lastByPMU: make(map[string]capturedFrame),
}

// ConfigureFrameCapture sets the ring-buffer capacity (default 1500, combined arrival order).
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

// RecordSinkFrame stores synchronously (tests / direct calls).
func RecordSinkFrame(pmuName string, raw []byte, row postgres.Row) {
	if len(raw) == 0 {
		return
	}
	now := time.Now().UTC()
	soc, fracRaw, fracCount, _ := decodeWireTime(raw)
	lagMs := 0.0
	if !row.Time.IsZero() {
		lagMs = now.Sub(row.Time.UTC()).Seconds() * 1000
	}
	recordSinkFrameLocked(capturedFrame{
		PMUName: pmuName, CapturedAt: now, SOC: soc, FracRaw: fracRaw, FracCount: fracCount,
		LagMs: lagMs, RawHex: hex.EncodeToString(raw), RawBytes: len(raw), Row: row,
	})
}

var (
	frameCaptureOnce sync.Once
	frameCaptureCh   chan capturedFrame
)

func startFrameCaptureWorker() {
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
				recordSinkFrameLocked(entry)
			}
		}()
	})
}

// RecordSinkFrameAsync enqueues frame capture off the parse hot path.
func RecordSinkFrameAsync(pmuName string, raw []byte, row postgres.Row) {
	if len(raw) == 0 {
		return
	}
	startFrameCaptureWorker()
	now := time.Now().UTC()
	soc, fracRaw, fracCount, _ := decodeWireTime(raw)
	lagMs := 0.0
	if !row.Time.IsZero() {
		lagMs = now.Sub(row.Time.UTC()).Seconds() * 1000
	}
	entry := capturedFrame{
		PMUName:    pmuName,
		CapturedAt: now,
		SOC:        soc,
		FracRaw:    fracRaw,
		FracCount:  fracCount,
		LagMs:      lagMs,
		RawHex:     hex.EncodeToString(raw),
		RawBytes:   len(raw),
		Row:        row,
	}
	select {
	case frameCaptureCh <- entry:
	default:
		// Drop capture sample under overload rather than stalling parse.
	}
}

func recordSinkFrameLocked(entry capturedFrame) {
	pmuName := entry.PMUName
	row := entry.Row
	now := entry.CapturedAt
	soc := entry.SOC
	fracCount := entry.FracCount
	lagMs := entry.LagMs

	frameCapture.mu.Lock()
	defer frameCapture.mu.Unlock()

	if prev, ok := frameCapture.lastByPMU[pmuName]; ok && !prev.Row.Time.IsZero() && !row.Time.IsZero() {
		dStamp := row.Time.Sub(prev.Row.Time).Seconds() * 1000
		dArrive := now.Sub(prev.CapturedAt).Seconds() * 1000
		if dStamp > 100 || dStamp < -10 {
			log.Printf("[frame-capture] TIMESTAMP JUMP pmu=%s stamp %s -> %s (dStamp=%.0fms) arrive_gap=%.1fms SOC %d->%d (dSOC=%d) frac %d->%d lag_ms=%.0f->%.0f",
				pmuName,
				prev.Row.Time.UTC().Format(time.RFC3339Nano),
				row.Time.UTC().Format(time.RFC3339Nano),
				dStamp, dArrive,
				prev.SOC, soc, int64(soc)-int64(prev.SOC),
				prev.FracCount, fracCount,
				prev.LagMs, lagMs,
			)
		}
	}
	frameCapture.lastByPMU[pmuName] = entry

	limit := frameCapture.limit
	if limit < 1 {
		limit = 1500
	}
	frameCapture.buf = append(frameCapture.buf, entry)
	if len(frameCapture.buf) > limit {
		frameCapture.buf = frameCapture.buf[len(frameCapture.buf)-limit:]
	}
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
	EntityID    string    `json:"entity_id"`
	IDCode      int32     `json:"idcode"`
	SOC         uint32    `json:"soc"`
	FracCount   uint32    `json:"fracsec_count"`
	CapturedAt  time.Time `json:"captured_at"`
	LagMs       float64   `json:"lag_ms"`
	Freq        float64   `json:"freq"`
	FreqDev     float64   `json:"freq_dev"`
	ROCOF       float64   `json:"rocof"`
	MW          float64   `json:"mw"`
	MVAR        float64   `json:"mvar"`
	MVA         float64   `json:"mva"`
	PowerFactor float64   `json:"power_factor"`
	VAMag       float64   `json:"va_mag"`
	VAAng       float64   `json:"va_ang"`
	VBMag       float64   `json:"vb_mag"`
	VBAng       float64   `json:"vb_ang"`
	VCMag       float64   `json:"vc_mag"`
	VCAng       float64   `json:"vc_ang"`
	IAMag       float64   `json:"ia_mag"`
	IAAng       float64   `json:"ia_ang"`
	Stat        int32     `json:"stat"`
	Digital     int32     `json:"digital"`
	CRCValid    bool      `json:"crc_valid"`
	TimeQuality int32     `json:"time_quality"`
}

func frameToParsedOut(idx int, f capturedFrame) parsedCaptureOut {
	r := f.Row
	return parsedCaptureOut{
		Index: idx, Time: r.Time, EntityID: r.EntityID, IDCode: r.IDCode,
		SOC: f.SOC, FracCount: f.FracCount, CapturedAt: f.CapturedAt, LagMs: f.LagMs,
		Freq: r.Freq, FreqDev: r.FreqDev, ROCOF: r.ROCOF,
		MW: r.MW, MVAR: r.MVAR, MVA: r.MVA, PowerFactor: r.PowerFactor,
		VAMag: r.VAMag, VAAng: r.VAAng, VBMag: r.VBMag, VBAng: r.VBAng,
		VCMag: r.VCMag, VCAng: r.VCAng, IAMag: r.IAMag, IAAng: r.IAAng,
		Stat: r.Stat, Digital: r.Digital, CRCValid: r.CRCValid, TimeQuality: r.TimeQuality,
	}
}

// Snapshot returns up to count most recent captured frames (newest last, arrival order).
func SnapshotFrames(count int, pmuFilter string) (raw []rawCaptureOut, parsed []parsedCaptureOut, src []capturedFrame) {
	frameCapture.mu.Lock()
	defer frameCapture.mu.Unlock()

	src = frameCapture.buf
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
		return nil, nil, nil
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
	return raw, parsed, src
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
			strconv.Itoa(r.Index),
			r.PMUName,
			r.CapturedAt.UTC().Format(time.RFC3339Nano),
			strconv.FormatUint(uint64(r.SOC), 10),
			strconv.FormatUint(uint64(r.FracRaw), 10),
			strconv.FormatUint(uint64(r.FracCount), 10),
			strconv.FormatFloat(r.LagMs, 'f', 3, 64),
			r.RawHex,
			strconv.Itoa(r.RawBytes),
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
		"index", "time", "entity_id", "idcode", "soc", "fracsec_count", "captured_at", "lag_ms",
		"freq", "freq_dev", "rocof", "mw", "mvar", "mva", "power_factor",
		"va_mag", "va_ang", "vb_mag", "vb_ang", "vc_mag", "vc_ang", "ia_mag", "ia_ang",
		"stat", "digital", "crc_valid", "time_quality",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			strconv.Itoa(r.Index),
			r.Time.UTC().Format(time.RFC3339Nano),
			r.EntityID,
			strconv.FormatInt(int64(r.IDCode), 10),
			strconv.FormatUint(uint64(r.SOC), 10),
			strconv.FormatUint(uint64(r.FracCount), 10),
			r.CapturedAt.UTC().Format(time.RFC3339Nano),
			strconv.FormatFloat(r.LagMs, 'f', 3, 64),
			strconv.FormatFloat(r.Freq, 'f', 6, 64),
			strconv.FormatFloat(r.FreqDev, 'f', 6, 64),
			strconv.FormatFloat(r.ROCOF, 'f', 6, 64),
			strconv.FormatFloat(r.MW, 'f', 6, 64),
			strconv.FormatFloat(r.MVAR, 'f', 6, 64),
			strconv.FormatFloat(r.MVA, 'f', 6, 64),
			strconv.FormatFloat(r.PowerFactor, 'f', 6, 64),
			strconv.FormatFloat(r.VAMag, 'f', 6, 64),
			strconv.FormatFloat(r.VAAng, 'f', 6, 64),
			strconv.FormatFloat(r.VBMag, 'f', 6, 64),
			strconv.FormatFloat(r.VBAng, 'f', 6, 64),
			strconv.FormatFloat(r.VCMag, 'f', 6, 64),
			strconv.FormatFloat(r.VCAng, 'f', 6, 64),
			strconv.FormatFloat(r.IAMag, 'f', 6, 64),
			strconv.FormatFloat(r.IAAng, 'f', 6, 64),
			strconv.Itoa(int(r.Stat)),
			strconv.Itoa(int(r.Digital)),
			strconv.FormatBool(r.CRCValid),
			strconv.Itoa(int(r.TimeQuality)),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

// DumpFrames writes raw + parsed CSV files.
func DumpFrames(count int, pmuFilter, rawPath, parsedPath string) (int, error) {
	raw, parsed, _ := SnapshotFrames(count, pmuFilter)
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

// FrameCaptureStatus reports ring-buffer configuration and fill level.
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

// RegisterFrameCaptureHandler exposes frame-capture HTTP endpoints.
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
		"ok":              true,
		"format":          "csv",
		"requested":       count,
		"count":           n,
		"buffer_limit":    status["limit"],
		"buffered":        status["buffered"],
		"buffered_by_pmu": status["buffered_by_pmu"],
		"raw_file":        rawOut,
		"parsed_file":     parsedOut,
		"warning":         warn,
	})
}
