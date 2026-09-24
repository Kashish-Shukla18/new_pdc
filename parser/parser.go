// Package parser turns raw IEEE C37.118 bytes into numbers we can use.
//
// Two frame types matter here:
//
//	CFG-2  → decoded in cfg2.go  → Profile (channel map) stored in RAM
//	DATA   → decoded in this file → Reading (freq, phasors, STAT, …)
//
// You cannot parse DATA without a Profile from the live handshake.
package parser

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	frameTypeData = 0x00 // SYNC bits 6–4 mean "DATA"
	frameTypeMask = 0x70 // mask to read those type bits
)

// ─── Types the rest of the PDC uses ─────────────────────────────────────────

// STATDecoded unpacks the 16-bit STAT word (IEEE C37.118.2-2011 Table 7).
// Example: is GPS locked? did config change? is data bad?
type STATDecoded struct {
	DataErrorCode    uint8 `json:"data_error_code"`    // bits 15-14
	DataError        bool  `json:"data_error"`         // true if code ≠ 0
	CFGChange        bool  `json:"cfg_change"`         // bit 13
	TriggerDetected  bool  `json:"trigger_detected"`   // bit 12
	SortMethod       bool  `json:"sort_method"`        // bit 11
	PMUSyncStatus    bool  `json:"pmu_sync_status"`    // bit 10: 1 = unlocked
	PMUTimeQuality   uint8 `json:"pmu_time_quality"`   // bits 9-6
	UnlockedDuration uint8 `json:"unlocked_duration"`  // bits 5-4
	PMUTriggerReason uint8 `json:"pmu_trigger_reason"` // bits 3-0
}

// MsgTimeQuality unpacks FRACSEC bits 31–24 (Table 5): leap-second + TQ code.
type MsgTimeQuality struct {
	Raw                 uint8 `json:"raw"`
	Reserved            bool  `json:"reserved"`
	LeapSecondDirection bool  `json:"leap_second_direction"`
	LeapSecondOccurred  bool  `json:"leap_second_occurred"`
	LeapSecondPending   bool  `json:"leap_second_pending"`
	TimeQualityCode     uint8 `json:"time_quality_code"`
}

// Phasor is one AC measurement: real/imag (or mag/angle) plus derived fields.
type Phasor struct {
	Real         float32
	Imag         float32
	Magnitude    float32
	PhaseRadians float32
	PhaseDegrees float32
	Power        float32
	PowerReal    float32
	PowerImag    float32
}

// NamedPhasor / NamedAnalog keep the CFG channel name next to the value.
type NamedPhasor struct {
	Name   string `json:"name"`
	Phasor Phasor `json:"phasor"`
}
type NamedAnalog struct {
	Name  string  `json:"name"`
	Value float32 `json:"value"`
}

// Reading is one fully decoded DATA frame — what the dashboard and aligner use.
type Reading struct {
	PMUName       string         `json:"pmu_name"`
	SyncWord      uint16         `json:"sync_word"`
	FrameType     string         `json:"frame_type"`
	FrameSize     int            `json:"frame_size"`
	IDCode        uint16         `json:"idcode"`
	SOC           uint32         `json:"soc"`
	FracSecRaw    uint32         `json:"fracsec_raw"`
	TimeQuality   uint8          `json:"time_quality"`
	MsgTQ         MsgTimeQuality `json:"msg_tq"`
	FracSecCount  uint32         `json:"fracsec_count"`
	Checksum      uint16         `json:"checksum"`
	ChecksumValid bool           `json:"checksum_valid"`
	Timestamp     time.Time      `json:"timestamp"`
	FrameBytes    int            `json:"frame_bytes"`

	Stat       uint16      `json:"stat_raw"`
	StatDetail STATDecoded `json:"stat_decoded"`

	Digitals     []uint16 `json:"digitals,omitempty"`
	DigitalNames []string `json:"digital_names,omitempty"`
	Phasors      []NamedPhasor `json:"phasors,omitempty"`
	Analogs      []NamedAnalog `json:"analogs,omitempty"`

	// Convenience copies of common channels (matched by CFG name when possible).
	VA Phasor `json:"va"`
	VB Phasor `json:"vb"`
	VC Phasor `json:"vc"`
	IA Phasor `json:"ia"`

	VoltageImbalancePercent  float32 `json:"voltage_imbalance_percent"`
	VAB_PhaseAngleDifference float32 `json:"vab_phase_diff_degrees"`
	VBC_PhaseAngleDifference float32 `json:"vbc_phase_diff_degrees"`
	VCA_PhaseAngleDifference float32 `json:"vca_phase_diff_degrees"`
	SequencePos              float32 `json:"sequence_positive_rms"`
	SequenceNeg              float32 `json:"sequence_negative_rms"`
	SequenceZero             float32 `json:"sequence_zero_rms"`

	Frequency          float32 `json:"frequency_hz"`
	FrequencyDeviation float32 `json:"frequency_deviation_hz"`
	ROCOF              float32 `json:"rocof_hz_per_sec"`

	MW                 float32 `json:"mw_active"`
	MVAR               float32 `json:"mvar_reactive"`
	MVA                float32 `json:"mva_apparent"`
	PowerFactor        float32 `json:"power_factor"`
	PowerFactorLeadLag string  `json:"power_factor_direction"`
	TotalPowerReal     float32 `json:"total_power_real_w"`
	TotalPowerImag     float32 `json:"total_power_imag_var"`

	Digital uint16       `json:"digital"`
	Trace   LatencyTrace `json:"trace,omitempty"`
}

// LatencyTrace is filled later by main.go for the latency UI (not from the wire).
type LatencyTrace struct {
	ReceivedAtUnixNano int64   `json:"received_at_ns,omitempty"`
	TcpWaitMs          float64 `json:"tcp_wait_ms,omitempty"`
	TcpCopyMs          float64 `json:"tcp_copy_ms,omitempty"`
	TcpReadMs          float64 `json:"tcp_read_ms,omitempty"`
	FrameToParseMs     float64 `json:"frame_to_parse_ms,omitempty"`
	ParseMs            float64 `json:"parse_ms,omitempty"`
	QualityMs          float64 `json:"quality_ms,omitempty"`
}

// ─── Small helpers ──────────────────────────────────────────────────────────

func decodeStat(stat uint16) STATDecoded {
	code := uint8((stat >> 14) & 0x3)
	return STATDecoded{
		DataErrorCode:    code,
		DataError:        code != 0,
		CFGChange:        (stat>>13)&1 == 1,
		TriggerDetected:  (stat>>12)&1 == 1,
		SortMethod:       (stat>>11)&1 == 1,
		PMUSyncStatus:    (stat>>10)&1 == 1,
		PMUTimeQuality:   uint8((stat >> 6) & 0xF),
		UnlockedDuration: uint8((stat >> 4) & 0x3),
		PMUTriggerReason: uint8(stat & 0xF),
	}
}

func decodeMsgTQ(fracsec uint32) MsgTimeQuality {
	b := uint8(fracsec >> 24)
	return MsgTimeQuality{
		Raw:                 b,
		Reserved:            (b>>7)&1 == 1,
		LeapSecondDirection: (b>>6)&1 == 1,
		LeapSecondOccurred:  (b>>5)&1 == 1,
		LeapSecondPending:   (b>>4)&1 == 1,
		TimeQualityCode:     b & 0x0F,
	}
}

// calcPhasor: rectangular (real, imag) → magnitude + angle.
func calcPhasor(real, imag float32) Phasor {
	mag := math.Sqrt(float64(real*real + imag*imag))
	phase := math.Atan2(float64(imag), float64(real))
	return Phasor{
		Real: real, Imag: imag,
		Magnitude:    float32(mag),
		PhaseRadians: float32(phase),
		PhaseDegrees: float32(phase * 180.0 / math.Pi),
	}
}

// angleDiffDeg = to − from, wrapped into (−180, +180].
func angleDiffDeg(from, to float32) float32 {
	d := to - from
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	return d
}

// calcSequenceMetrics: symmetrical components from VA/VB/VC (for imbalance %).
func calcSequenceMetrics(va, vb, vc Phasor) (pos, neg, zero, imbalancePct float32) {
	aR, aI := float32(-0.5), float32(math.Sqrt(3)/2)   // e^(j2π/3)
	a2R, a2I := float32(-0.5), float32(-math.Sqrt(3)/2) // e^(j4π/3)

	pR := (va.Real + aR*vb.Real - aI*vb.Imag + a2R*vc.Real - a2I*vc.Imag) / 3
	pI := (va.Imag + aR*vb.Imag + aI*vb.Real + a2R*vc.Imag + a2I*vc.Real) / 3
	nR := (va.Real + a2R*vb.Real - a2I*vb.Imag + aR*vc.Real - aI*vc.Imag) / 3
	nI := (va.Imag + a2R*vb.Imag + a2I*vb.Real + aR*vc.Imag + aI*vc.Real) / 3
	zR := (va.Real + vb.Real + vc.Real) / 3
	zI := (va.Imag + vb.Imag + vc.Imag) / 3

	pos = float32(math.Sqrt(float64(pR*pR + pI*pI)))
	neg = float32(math.Sqrt(float64(nR*nR + nI*nI)))
	zero = float32(math.Sqrt(float64(zR*zR + zI*zI)))
	if pos > 0.1 {
		imbalancePct = neg / pos * 100
	}
	return pos, neg, zero, imbalancePct
}

// ─── Public entry: parse one DATA frame ─────────────────────────────────────

// ParseDataFrame is what main.go calls for every DATA frame.
//
//	1. Look up this PMU's Profile (from CFG-2 handshake via GetProfile)
//	2. Hand off to parseDataWithProfile
func ParseDataFrame(pmuName string, raw []byte) (Reading, error) {
	profile, ok := GetProfile(pmuName)
	if !ok {
		return Reading{}, fmt.Errorf("no CFG2 profile for %q; complete handshake first", pmuName)
	}
	return parseDataWithProfile(pmuName, raw, profile)
}

// parseDataWithProfile walks one DATA frame left → right using cfg as the map.
//
// Wire layout (big-endian):
//
//	[SYNC 2][FRAMESIZE 2][IDCODE 2][SOC 4][FRACSEC 4][ payload … ][CRC 2]
//
// Payload (first PMU block) order:
//
//	STAT → phasors → FREQ + ROCOF → analogs → digitals
func parseDataWithProfile(pmuName string, raw []byte, cfg Profile) (Reading, error) {

	// ── Step 1: is this even a DATA frame? ──────────────────────────────────
	if len(raw) < 16 || raw[0] != 0xAA || (raw[1]&frameTypeMask) != frameTypeData {
		return Reading{}, fmt.Errorf("not a data frame")
	}
	frameSize := int(binary.BigEndian.Uint16(raw[2:4]))
	if frameSize != len(raw) {
		return Reading{}, fmt.Errorf("frame size mismatch: header=%d actual=%d", frameSize, len(raw))
	}

	// ── Step 2: common header + CRC check ───────────────────────────────────
	syncWord := binary.BigEndian.Uint16(raw[0:2])
	idCode := binary.BigEndian.Uint16(raw[4:6])
	socRaw := binary.BigEndian.Uint32(raw[6:10])
	fracsec := binary.BigEndian.Uint32(raw[10:14])
	checksumRx := binary.BigEndian.Uint16(raw[len(raw)-2:])
	checksumCalc := crc16(raw[:len(raw)-2])
	if checksumRx != checksumCalc {
		return Reading{}, fmt.Errorf("CRC mismatch: rx=0x%04X calc=0x%04X", checksumRx, checksumCalc)
	}
	msgTQ := decodeMsgTQ(fracsec)

	// ── Step 3: build UTC timestamp from SOC + FRACSEC ──────────────────────
	// FRACSEC low 24 bits = fraction of a second in units of 1/TIME_BASE.
	tb := cfg.TimeBase
	if tb == 0 {
		tb = 1_000_000
	}
	fracCount := fracsec & 0x00FFFFFF
	nanos := int64(fracCount) * int64(time.Second) / int64(tb)
	ts := time.Unix(int64(socRaw), nanos).UTC()

	// ── Step 4: payload must match CFG-2 expected size ──────────────────────
	p := raw[14 : len(raw)-2]
	want := cfg.DataPayloadBytes
	if want == 0 {
		want = ExpectedDataPayloadSize(cfg)
	}
	totalWant := cfg.TotalDataPayloadBytes
	if totalWant == 0 {
		totalWant = want
	}
	if len(p) != totalWant {
		return Reading{}, fmt.Errorf("payload length %d != expected %d (first_block=%d NUM_PMU=%d)",
			len(p), totalWant, want, cfg.NumPMU)
	}

	// Cursor into the payload. Each read* advances o.
	o := 0
	readF32 := func() (float32, error) {
		if o+4 > len(p) {
			return 0, fmt.Errorf("short float at %d", o)
		}
		v := math.Float32frombits(binary.BigEndian.Uint32(p[o : o+4]))
		o += 4
		return v, nil
	}
	readI16 := func() (int16, error) {
		if o+2 > len(p) {
			return 0, fmt.Errorf("short int16 at %d", o)
		}
		v := int16(binary.BigEndian.Uint16(p[o : o+2]))
		o += 2
		return v, nil
	}
	readU16 := func() (uint16, error) {
		if o+2 > len(p) {
			return 0, fmt.Errorf("short uint16 at %d", o)
		}
		v := binary.BigEndian.Uint16(p[o : o+2])
		o += 2
		return v, nil
	}
	phScale := func(i int) float64 {
		if i < len(cfg.PhUnits) && cfg.PhUnits[i].Factor > 0 {
			return cfg.PhUnits[i].Factor
		}
		return 1
	}
	anScale := func(i int) float64 {
		if i < len(cfg.AnUnits) && cfg.AnUnits[i].Factor > 0 {
			return cfg.AnUnits[i].Factor
		}
		return 1
	}

	// ── Step 5: STAT word ───────────────────────────────────────────────────
	stat := binary.BigEndian.Uint16(p[o:])
	o += 2

	// ── Step 6: phasors (count + format come from CFG-2) ────────────────────
	// CFG FORMAT bits choose: float vs integer, polar vs rectangular.
	phasors := make([]Phasor, cfg.Phnmr)
	for i := 0; i < cfg.Phnmr; i++ {
		scale := float32(phScale(i))
		switch {
		case cfg.PhFloat && cfg.Polar:
			// float polar: magnitude, angle(radians)
			mag, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			ang, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			rad := float64(ang)
			phasors[i] = Phasor{
				Magnitude: mag, PhaseRadians: ang,
				PhaseDegrees: float32(rad * 180 / math.Pi),
				Real:         mag * float32(math.Cos(rad)),
				Imag:         mag * float32(math.Sin(rad)),
			}
		case cfg.PhFloat:
			// float rectangular: real, imag
			re, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			im, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			phasors[i] = calcPhasor(re, im)
		case cfg.Polar:
			// int polar: mag uint16 × PHUNIT, angle int16 = radians × 10⁴
			magRaw, err := readU16()
			if err != nil {
				return Reading{}, err
			}
			angRaw, err := readI16()
			if err != nil {
				return Reading{}, err
			}
			mag := float32(magRaw) * scale
			rad := float64(angRaw) / 10000
			phasors[i] = Phasor{
				Magnitude: mag, PhaseRadians: float32(rad),
				PhaseDegrees: float32(rad * 180 / math.Pi),
				Real:         mag * float32(math.Cos(rad)),
				Imag:         mag * float32(math.Sin(rad)),
			}
		default:
			// int rectangular: both int16 × PHUNIT
			reRaw, err := readI16()
			if err != nil {
				return Reading{}, err
			}
			imRaw, err := readI16()
			if err != nil {
				return Reading{}, err
			}
			phasors[i] = calcPhasor(float32(reRaw)*scale, float32(imRaw)*scale)
		}
	}

	// ── Step 7: frequency + ROCOF ───────────────────────────────────────────
	var frequency, rocof float32
	if cfg.FreqFloat {
		var err error
		frequency, err = readF32()
		if err != nil {
			return Reading{}, err
		}
		rocof, err = readF32()
		if err != nil {
			return Reading{}, err
		}
	} else {
		// Integer: FREQ = FNOM + raw/1000, ROCOF = raw/100
		f, err := readI16()
		if err != nil {
			return Reading{}, err
		}
		r, err := readI16()
		if err != nil {
			return Reading{}, err
		}
		frequency = float32(cfg.FnomHz) + float32(f)/1000
		rocof = float32(r) / 100
	}

	// Map CFG names → VA/VB/VC/IA for charts that expect those labels.
	names := phasorNames(cfg)
	va, vb, vc, ia := mapPhasorsToStandard(names, phasors)
	namedPhasors := make([]NamedPhasor, len(phasors))
	for i, ph := range phasors {
		name := fmt.Sprintf("PH%d", i+1)
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		namedPhasors[i] = NamedPhasor{Name: name, Phasor: ph}
	}

	// ── Step 8: analog channels (names + values from CFG-2 / DATA) ─────────
	analogNames := make([]string, 0, cfg.Annmr)
	if len(cfg.Channels) >= cfg.Phnmr+cfg.Annmr {
		analogNames = cfg.Channels[cfg.Phnmr : cfg.Phnmr+cfg.Annmr]
	}
	namedAnalogs := make([]NamedAnalog, 0, cfg.Annmr)
	for i := 0; i < cfg.Annmr; i++ {
		var v float32
		if cfg.AnFloat {
			fv, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			v = fv
		} else {
			iv, err := readI16()
			if err != nil {
				return Reading{}, err
			}
			v = float32(iv) * float32(anScale(i))
		}
		name := fmt.Sprintf("ANA%d", i+1)
		if i < len(analogNames) && analogNames[i] != "" {
			name = analogNames[i]
		}
		namedAnalogs = append(namedAnalogs, NamedAnalog{Name: name, Value: v})
	}
	// Optional convenience copies of the first three analog slots (by index only —
	// not CFG semantic labels). Prefer Reading.Analogs for named access.
	var mw, mvar, mva float32
	if len(namedAnalogs) > 0 {
		mw = namedAnalogs[0].Value
	}
	if len(namedAnalogs) > 1 {
		mvar = namedAnalogs[1].Value
	}
	if len(namedAnalogs) > 2 {
		mva = namedAnalogs[2].Value
	}

	// ── Step 9: digital status words ────────────────────────────────────────
	digitals := make([]uint16, cfg.Dgnmr)
	for i := 0; i < cfg.Dgnmr; i++ {
		w, err := readU16()
		if err != nil {
			return Reading{}, err
		}
		digitals[i] = w
	}
	var digital uint16
	if len(digitals) > 0 {
		digital = digitals[0]
	}
	digStart := cfg.Phnmr + cfg.Annmr
	var digitalNames []string
	if len(cfg.Channels) > digStart {
		digitalNames = append([]string(nil), cfg.Channels[digStart:]...)
	}

	// We only unpack the first PMU block in detail; size must still match CFG.
	if o != want {
		return Reading{}, fmt.Errorf("first PMU block consumed %d bytes, expected %d", o, want)
	}

	// ── Step 10: derived metrics (not on the wire — computed here) ──────────
	nominal := float32(cfg.FnomHz)
	if nominal <= 0 {
		nominal = 50
	}
	seqPos, seqNeg, seqZero, voltageImbalance := calcSequenceMetrics(va, vb, vc)
	s := math.Sqrt(float64(mw*mw + mvar*mvar))
	if mva == 0 && s > 0 {
		// No third analog slot — apparent power from first two (index convenience only).
		mva = float32(s)
	}
	if s <= 0 {
		s = float64(mva)
	}
	pf, pfDir := float32(1), "unity"
	if s > 0.1 {
		pf = mw / float32(s)
		if pf > 1 {
			pf = 1
		}
		if mvar > 0 {
			pfDir = "lagging"
		} else if mvar < 0 {
			pfDir = "leading"
		}
	}

	return Reading{
		PMUName: pmuName, SyncWord: syncWord, FrameType: "DATA", FrameSize: frameSize,
		IDCode: idCode, SOC: socRaw, FracSecRaw: fracsec, TimeQuality: msgTQ.Raw,
		MsgTQ: msgTQ, FracSecCount: fracCount, Checksum: checksumRx, ChecksumValid: true,
		Timestamp: ts, FrameBytes: len(raw),
		Stat: stat, StatDetail: decodeStat(stat),
		Phasors: namedPhasors, Analogs: namedAnalogs,
		VA: va, VB: vb, VC: vc, IA: ia,
		VoltageImbalancePercent:  voltageImbalance,
		VAB_PhaseAngleDifference: angleDiffDeg(va.PhaseDegrees, vb.PhaseDegrees),
		VBC_PhaseAngleDifference: angleDiffDeg(vb.PhaseDegrees, vc.PhaseDegrees),
		VCA_PhaseAngleDifference: angleDiffDeg(vc.PhaseDegrees, va.PhaseDegrees),
		SequencePos: seqPos, SequenceNeg: seqNeg, SequenceZero: seqZero,
		Frequency: frequency, FrequencyDeviation: frequency - nominal, ROCOF: rocof,
		MW: mw, MVAR: mvar, MVA: mva, PowerFactor: pf, PowerFactorLeadLag: pfDir,
		TotalPowerReal: va.Real*ia.Real + va.Imag*ia.Imag,
		TotalPowerImag: va.Imag*ia.Real - va.Real*ia.Imag,
		Digital: digital, Digitals: digitals, DigitalNames: digitalNames,
	}, nil
}

// crc16 is the CRC-CCITT check used by C37.118 (seed 0xFFFF, poly 0x1021).
func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
