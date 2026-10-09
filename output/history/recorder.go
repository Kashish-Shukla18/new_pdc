// Package history records CFG versions, lifecycle events, and DATA frames for Timescale.
package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"pdc/output/postgres"
	"pdc/parser"
)

// Sink is the subset of postgres.Writer used here (tests can fake it).
type Sink interface {
	EnsureCfgVersion(ctx context.Context, v postgres.CfgVersion) (int64, error)
	EnqueueEvent(row postgres.EventRow)
	EnqueueFrame(row postgres.FrameRow)
}

// Recorder caches cfg_id per PMU and emits sparse lifecycle/quality events.
type Recorder struct {
	w Sink

	mu       sync.Mutex
	cfgID    map[string]int64
	hash     map[string]string
	qualityOK map[string]bool // last known quality state for edge events
}

// NewRecorder wraps a history sink. nil sink → no-op recorder.
func NewRecorder(w Sink) *Recorder {
	if w == nil {
		return &Recorder{
			cfgID:     make(map[string]int64),
			hash:      make(map[string]string),
			qualityOK: make(map[string]bool),
		}
	}
	return &Recorder{
		w:         w,
		cfgID:     make(map[string]int64),
		hash:      make(map[string]string),
		qualityOK: make(map[string]bool),
	}
}

// Enabled reports whether events/cfg will be persisted.
func (r *Recorder) Enabled() bool {
	return r != nil && r.w != nil
}

// CfgID returns the cached layout id for a PMU (0 if unknown).
func (r *Recorder) CfgID(pmu string) int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfgID[pmu]
}

// LayoutHash is a stable digest of the CFG-2 fields that affect DATA decoding.
func LayoutHash(p parser.Profile) string {
	type unit struct {
		Kind   string  `json:"kind,omitempty"`
		Factor float64 `json:"factor"`
		Type   uint8   `json:"type,omitempty"`
		Normal uint16  `json:"normal,omitempty"`
		Valid  uint16  `json:"valid,omitempty"`
	}
	payload := struct {
		Station  string   `json:"station"`
		IDCode   uint16   `json:"idcode"`
		TimeBase uint32   `json:"time_base"`
		Format   uint16   `json:"format"`
		Phnmr    int      `json:"phnmr"`
		Annmr    int      `json:"annmr"`
		Dgnmr    int      `json:"dgnmr"`
		FnomHz   int      `json:"fnom"`
		CfgCnt   uint16   `json:"cfg_cnt"`
		DataRate int16    `json:"data_rate"`
		Channels []string `json:"channels"`
		Polar    bool     `json:"polar"`
		PhFloat  bool     `json:"ph_float"`
		AnFloat  bool     `json:"an_float"`
		FreqFloat bool    `json:"freq_float"`
		PhUnits  []unit   `json:"ph_units"`
		AnUnits  []unit   `json:"an_units"`
		DigUnits []unit   `json:"dig_units"`
	}{
		Station: p.Station, IDCode: p.IDCode, TimeBase: p.TimeBase & 0x00FFFFFF,
		Format: p.Format, Phnmr: p.Phnmr, Annmr: p.Annmr, Dgnmr: p.Dgnmr,
		FnomHz: p.FnomHz, CfgCnt: p.CfgCnt, DataRate: p.DataRate,
		Channels: append([]string(nil), p.Channels...),
		Polar: p.Polar, PhFloat: p.PhFloat, AnFloat: p.AnFloat, FreqFloat: p.FreqFloat,
	}
	for _, u := range p.PhUnits {
		kind := "v"
		if u.IsCurrent {
			kind = "i"
		}
		payload.PhUnits = append(payload.PhUnits, unit{Kind: kind, Factor: u.Factor})
	}
	for _, u := range p.AnUnits {
		payload.AnUnits = append(payload.AnUnits, unit{Type: u.Type, Factor: u.Factor})
	}
	for _, u := range p.DigUnits {
		payload.DigUnits = append(payload.DigUnits, unit{Normal: u.NormalMask, Valid: u.ValidMask})
	}
	b, err := json.Marshal(payload)
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", p)))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func channelsJSON(p parser.Profile) []byte {
	b, err := json.Marshal(map[string]any{
		"channels":   p.Channels,
		"phnmr":      p.Phnmr,
		"annmr":      p.Annmr,
		"dgnmr":      p.Dgnmr,
		"polar":      p.Polar,
		"ph_float":   p.PhFloat,
		"an_float":   p.AnFloat,
		"freq_float": p.FreqFloat,
		"fnom_hz":    p.FnomHz,
		"cfg_cnt":    p.CfgCnt,
	})
	if err != nil {
		return []byte("[]")
	}
	return b
}

func (r *Recorder) emit(pmu, typ string, detail map[string]any) {
	if r == nil || r.w == nil {
		return
	}
	var raw []byte
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	r.w.EnqueueEvent(postgres.EventRow{
		Time:   time.Now().UTC(),
		PMUID:  pmu,
		Type:   typ,
		Detail: raw,
	})
}

// NoteProfile persists cfg_version when the layout hash is new/changed.
func (r *Recorder) NoteProfile(pmu string) {
	if r == nil || r.w == nil || pmu == "" {
		return
	}
	prof, ok := parser.GetProfile(pmu)
	if !ok {
		return
	}
	hash := LayoutHash(prof)
	r.mu.Lock()
	prev := r.hash[pmu]
	r.mu.Unlock()
	if prev == hash {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := r.w.EnsureCfgVersion(ctx, postgres.CfgVersion{
		PMUID:      pmu,
		LayoutHash: hash,
		Station:    prof.Station,
		IDCode:     int32(prof.IDCode),
		DataRate:   int32(prof.DataRate),
		TimeBase:   int32(prof.TimeBase & 0x00FFFFFF),
		Channels:   channelsJSON(prof),
	})
	if err != nil {
		log.Printf("[history] cfg_version %s: %v", pmu, err)
		r.emit(pmu, "cfg_persist_failed", map[string]any{"error": err.Error(), "layout_hash": hash})
		return
	}
	r.mu.Lock()
	r.hash[pmu] = hash
	r.cfgID[pmu] = id
	r.mu.Unlock()
	if prev != "" {
		r.emit(pmu, "cfg_changed", map[string]any{
			"cfg_id": id, "layout_hash": hash, "prev_hash": prev,
			"station": prof.Station, "data_rate": prof.DataRate,
		})
	}
}

// NoteBecameLive / NoteLeftLive — aligner live-set transitions.
func (r *Recorder) NoteBecameLive(pmu string) {
	r.emit(pmu, "became_live", nil)
	r.NoteProfile(pmu) // ensure cfg row exists once streaming
}

func (r *Recorder) NoteLeftLive(pmu string) {
	r.emit(pmu, "left_live", nil)
}

// NoteOperatorStart / NoteOperatorStop — configured set changes.
func (r *Recorder) NoteOperatorStart(pmu string) {
	r.emit(pmu, "operator_start", nil)
}

func (r *Recorder) NoteOperatorStop(pmu string) {
	r.emit(pmu, "operator_stop", nil)
}

// NoteQuality emits rate-limited edge events (ok↔bad), not per-frame spam.
func (r *Recorder) NoteQuality(pmu string, ok bool, reason string) {
	if r == nil || r.w == nil || pmu == "" {
		return
	}
	r.mu.Lock()
	prev, seen := r.qualityOK[pmu]
	r.qualityOK[pmu] = ok
	r.mu.Unlock()
	if seen && prev == ok {
		return
	}
	if !ok {
		r.emit(pmu, "quality_degraded", map[string]any{"reason": reason})
		return
	}
	if seen {
		r.emit(pmu, "quality_recovered", nil)
	}
}

// NoteFrame enqueues one parsed DATA frame. qualityOK comes from Validate(qerr==nil)
// and must not depend on DROP_QUALITY_REJECTED (live-path only).
func (r *Recorder) NoteFrame(reading parser.Reading, qualityOK bool, rejectReason string) {
	if r == nil || r.w == nil {
		return
	}
	cfgID := r.CfgID(reading.PMUName)
	r.w.EnqueueFrame(postgres.FrameFromReading(reading, qualityOK, rejectReason, cfgID))
}
