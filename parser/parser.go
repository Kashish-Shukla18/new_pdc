package parser

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	frameTypeData = 0x00 // SYNC bits 6-4
	frameTypeMask = 0x70 // mask for frame type (bits 6-4)
	minFrameSize  = 68   // legacy simulator layout only
)

// STATDecoded is IEEE C37.118.2-2011 Table 7.
type STATDecoded struct {
	DataErrorCode    uint8 `json:"data_error_code"`    // bits 15-14: 00=good, 01=PMU error, 10=test mode, 11=invalid
	DataError        bool  `json:"data_error"`         // true when DataErrorCode != 0
	CFGChange        bool  `json:"cfg_change"`         // bit 13
	TriggerDetected  bool  `json:"trigger_detected"`   // bit 12
	SortMethod       bool  `json:"sort_method"`        // bit 11: 0=timestamp, 1=arrival
	PMUSyncStatus    bool  `json:"pmu_sync_status"`    // bit 10: 0=locked to UTC, 1=unlocked
	PMUTimeQuality   uint8 `json:"pmu_time_quality"`   // bits 9-6 (4-bit TQ code)
	UnlockedDuration uint8 `json:"unlocked_duration"`  // bits 5-4: 00=<10s, 01=10-100s, 10=100-1000s, 11=>1000s
	PMUTriggerReason uint8 `json:"pmu_trigger_reason"` // bits 3-0
}

// MsgTimeQuality is FRACSEC bits 31-24 (IEEE C37.118.2-2011 Table 5).
type MsgTimeQuality struct {
	Raw                 uint8 `json:"raw"`
	Reserved            bool  `json:"reserved"`              // bit 7
	LeapSecondDirection bool  `json:"leap_second_direction"` // bit 6: 0=add, 1=delete
	LeapSecondOccurred  bool  `json:"leap_second_occurred"`  // bit 5
	LeapSecondPending   bool  `json:"leap_second_pending"`   // bit 4
	TimeQualityCode     uint8 `json:"time_quality_code"`     // bits 3-0
}

// Phasor represents a single phasor measurement with derived metrics
type Phasor struct {
	Real         float32 // rectangular real component
	Imag         float32 // rectangular imaginary component
	Magnitude    float32 // √(R² + I²) [RMS]
	PhaseRadians float32 // atan2(I, R)
	PhaseDegrees float32 // phase in degrees
	Power        float32 // power contribution (VA)
	PowerReal    float32 // real power component (W)
	PowerImag    float32 // imaginary power component (VAR)
}

// NamedPhasor is one CFG phasor channel with its wire name.
type NamedPhasor struct {
	Name   string `json:"name"`
	Phasor Phasor `json:"phasor"`
}

// NamedAnalog is one CFG analog channel with its wire name.
type NamedAnalog struct {
	Name  string  `json:"name"`
	Value float32 `json:"value"`
}

// Reading is the parsed payload from one C37.118 data frame.
type Reading struct {
	// ─ Identity & Timing
	PMUName       string         `json:"pmu_name"`
	SyncWord      uint16         `json:"sync_word"`
	FrameType     string         `json:"frame_type"`
	FrameSize     int            `json:"frame_size"`
	IDCode        uint16         `json:"idcode"`
	SOC           uint32         `json:"soc"`
	FracSecRaw    uint32         `json:"fracsec_raw"`
	TimeQuality   uint8          `json:"time_quality"` // FRACSEC high byte (raw MSG_TQ)
	MsgTQ         MsgTimeQuality `json:"msg_tq"`
	FracSecCount  uint32         `json:"fracsec_count"`
	Checksum      uint16         `json:"checksum"`
	ChecksumValid bool           `json:"checksum_valid"`
	Timestamp     time.Time      `json:"timestamp"`
	FrameBytes    int            `json:"frame_bytes"`

	// ─ STAT Word (decoded)
	Stat       uint16      `json:"stat_raw"`
	StatDetail STATDecoded `json:"stat_decoded"`

	// ─ All digital status words (Digital keeps word 0 for compatibility)
	Digitals     []uint16 `json:"digitals,omitempty"`
	DigitalNames []string `json:"digital_names,omitempty"` // CFG bit names for dig words

	// ─ All CFG phasors / analogs (by channel name)
	Phasors []NamedPhasor `json:"phasors,omitempty"`
	Analogs []NamedAnalog `json:"analogs,omitempty"`

	// ─ Phasors (mapped VA/VB/VC/IA for compatibility)
	VA Phasor `json:"va"`
	VB Phasor `json:"vb"`
	VC Phasor `json:"vc"`
	IA Phasor `json:"ia"`

	// ─ Derived Phasor Analysis
	VoltageImbalancePercent  float32 `json:"voltage_imbalance_percent"`
	VAB_PhaseAngleDifference float32 `json:"vab_phase_diff_degrees"`
	VBC_PhaseAngleDifference float32 `json:"vbc_phase_diff_degrees"`
	VCA_PhaseAngleDifference float32 `json:"vca_phase_diff_degrees"`
	SequencePos              float32 `json:"sequence_positive_rms"`
	SequenceNeg              float32 `json:"sequence_negative_rms"`
	SequenceZero             float32 `json:"sequence_zero_rms"`

	// ─ Frequency
	Frequency          float32 `json:"frequency_hz"`
	FrequencyDeviation float32 `json:"frequency_deviation_hz"` // from nominal 50Hz
	ROCOF              float32 `json:"rocof_hz_per_sec"`

	// ─ Power
	MW                 float32 `json:"mw_active"`
	MVAR               float32 `json:"mvar_reactive"`
	MVA                float32 `json:"mva_apparent"`
	PowerFactor        float32 `json:"power_factor"`
	PowerFactorLeadLag string  `json:"power_factor_direction"` // "leading" or "lagging"
	TotalPowerReal     float32 `json:"total_power_real_w"`
	TotalPowerImag     float32 `json:"total_power_imag_var"`

	// ─ Analog & Digital
	Digital uint16 `json:"digital"`

	// ─ Pipeline hop times (omitted from most storage views; used for latency UI)
	Trace LatencyTrace `json:"trace,omitempty"`
}

// LatencyTrace carries hop timings so the dashboard can show which function is slow
// even when ingress and processor run in different processes.
type LatencyTrace struct {
	ReceivedAtUnixNano int64   `json:"received_at_ns,omitempty"`
	TcpWaitMs          float64 `json:"tcp_wait_ms,omitempty"`
	TcpCopyMs          float64 `json:"tcp_copy_ms,omitempty"`
	TcpReadMs          float64 `json:"tcp_read_ms,omitempty"`
	KafkaLagMs         float64 `json:"kafka_lag_ms,omitempty"`
	FrameToParseMs     float64 `json:"frame_to_parse_ms,omitempty"`
	ParseMs            float64 `json:"parse_ms,omitempty"`
	QualityMs          float64 `json:"quality_ms,omitempty"`
	ReadingsPublishMs  float64 `json:"readings_pub_ms,omitempty"`
}

// decodeStat unpacks STAT per IEEE C37.118.2-2011 Table 7.
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

// decodeMsgTQ unpacks FRACSEC bits 31-24 per Table 5.
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

// calcPhasor computes magnitude, phase angle, and power for a phasor
func calcPhasor(real, imag float32) Phasor {
	mag := math.Sqrt(float64(real*real + imag*imag))
	phase := math.Atan2(float64(imag), float64(real))
	return Phasor{
		Real:         real,
		Imag:         imag,
		Magnitude:    float32(mag),
		PhaseRadians: float32(phase),
		PhaseDegrees: float32(phase * 180.0 / math.Pi),
	}
}

// calcSequenceMetrics computes positive/negative/zero sequence RMS and imbalance (%).
func calcSequenceMetrics(va, vb, vc Phasor) (float32, float32, float32, float32) {
	// Positive sequence: V+ = (Va + a*Vb + a²*Vc)/3 where a = e^(j2π/3)
	// Negative sequence: V- = (Va + a²*Vb + a*Vc)/3
	// Imbalance = |V-| / |V+| * 100%

	a_real := float32(-0.5)               // cos(2π/3)
	a_imag := float32(math.Sqrt(3) / 2)   // sin(2π/3)
	a2_real := float32(-0.5)              // cos(4π/3)
	a2_imag := float32(-math.Sqrt(3) / 2) // sin(4π/3)

	// V+ = (Va + a*Vb + a²*Vc)/3
	vpPos_r := (va.Real + a_real*vb.Real - a_imag*vb.Imag + a2_real*vc.Real - a2_imag*vc.Imag) / 3.0
	vpPos_i := (va.Imag + a_real*vb.Imag + a_imag*vb.Real + a2_real*vc.Imag + a2_imag*vc.Real) / 3.0
	vpPos := math.Sqrt(float64(vpPos_r*vpPos_r + vpPos_i*vpPos_i))

	// V- = (Va + a²*Vb + a*Vc)/3
	vpNeg_r := (va.Real + a2_real*vb.Real - a2_imag*vb.Imag + a_real*vc.Real - a_imag*vc.Imag) / 3.0
	vpNeg_i := (va.Imag + a2_real*vb.Imag + a2_imag*vb.Real + a_real*vc.Imag + a_imag*vc.Real) / 3.0
	vpNeg := math.Sqrt(float64(vpNeg_r*vpNeg_r + vpNeg_i*vpNeg_i))

	// V0 = (Va + Vb + Vc)/3
	vpZero_r := (va.Real + vb.Real + vc.Real) / 3.0
	vpZero_i := (va.Imag + vb.Imag + vc.Imag) / 3.0
	vpZero := math.Sqrt(float64(vpZero_r*vpZero_r + vpZero_i*vpZero_i))

	imbalance := float32(0)
	if vpPos > 0.1 {
		imbalance = float32(vpNeg / vpPos * 100.0)
	}
	return float32(vpPos), float32(vpNeg), float32(vpZero), imbalance
}

// ParseDataFrame decodes one C37.118 DATA frame using the registered CFG2 profile.
// A CFG2 profile is required (IEEE layout is not fixed without configuration).
func ParseDataFrame(pmuName string, raw []byte) (Reading, error) {
	profile, ok := GetProfile(pmuName)
	if !ok {
		return Reading{}, fmt.Errorf("no CFG2 profile for %q; complete handshake first", pmuName)
	}
	return parseDataWithProfile(pmuName, raw, profile)
}

func parseDataWithProfile(pmuName string, raw []byte, cfg Profile) (Reading, error) {
	if len(raw) < 16 || raw[0] != 0xAA || (raw[1]&frameTypeMask) != frameTypeData {
		return Reading{}, fmt.Errorf("not a data frame")
	}
	frameSize := int(binary.BigEndian.Uint16(raw[2:4]))
	if frameSize != len(raw) {
		return Reading{}, fmt.Errorf("frame size mismatch: header=%d actual=%d", frameSize, len(raw))
	}

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
	timeQuality := msgTQ.Raw

	tb := cfg.TimeBase
	if tb == 0 {
		tb = 1_000_000
	}
	fracCount := fracsec & 0x00FFFFFF
	nanos := int64(fracCount) * int64(time.Second) / int64(tb)
	ts := time.Unix(int64(socRaw), nanos).UTC()

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
		return Reading{}, fmt.Errorf("payload length %d != expected %d (first_block=%d NUM_PMU=%d)", len(p), totalWant, want, cfg.NumPMU)
	}

	o := 0
	stat := binary.BigEndian.Uint16(p[o:])
	o += 2

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

	phasors := make([]Phasor, cfg.Phnmr)
	for i := 0; i < cfg.Phnmr; i++ {
		scale := float32(phScale(i))
		if cfg.PhFloat {
			a, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			b, err := readF32()
			if err != nil {
				return Reading{}, err
			}
			if cfg.Polar {
				rad := float64(b)
				phasors[i] = Phasor{
					Magnitude:    a,
					PhaseRadians: b,
					PhaseDegrees: float32(rad * 180.0 / math.Pi),
					Real:         a * float32(math.Cos(rad)),
					Imag:         a * float32(math.Sin(rad)),
				}
			} else {
				phasors[i] = calcPhasor(a, b)
			}
		} else if cfg.Polar {
			// Integer polar: magnitude uint16, angle int16 in radians × 10^4
			magRaw, err := readU16()
			if err != nil {
				return Reading{}, err
			}
			angRaw, err := readI16()
			if err != nil {
				return Reading{}, err
			}
			mag := float32(magRaw) * scale
			rad := float64(angRaw) / 10000.0
			phasors[i] = Phasor{
				Magnitude:    mag,
				PhaseRadians: float32(rad),
				PhaseDegrees: float32(rad * 180.0 / math.Pi),
				Real:         mag * float32(math.Cos(rad)),
				Imag:         mag * float32(math.Sin(rad)),
			}
		} else {
			// Integer rectangular: both int16 × PHUNIT
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
		f, err := readI16()
		if err != nil {
			return Reading{}, err
		}
		r, err := readI16()
		if err != nil {
			return Reading{}, err
		}
		frequency = float32(cfg.FnomHz) + float32(f)/1000.0
		rocof = float32(r) / 100.0
	}

	names := phasorNames(cfg)
	va, vb, vc, ia := mapPhasorsToStandard(names, phasors)

	namedPhasors := make([]NamedPhasor, len(phasors))
	for i, p := range phasors {
		name := fmt.Sprintf("PH%d", i+1)
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		namedPhasors[i] = NamedPhasor{Name: name, Phasor: p}
	}

	analogNames := make([]string, 0, cfg.Annmr)
	if len(cfg.Channels) >= cfg.Phnmr+cfg.Annmr {
		analogNames = cfg.Channels[cfg.Phnmr : cfg.Phnmr+cfg.Annmr]
	}
	namedAnalogs := make([]NamedAnalog, 0, cfg.Annmr)
	var mw, mvar float32
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
		if i == 0 {
			mw = v
		} else if i == 1 {
			mvar = v
		}
	}

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

	if o != want {
		return Reading{}, fmt.Errorf("first PMU block consumed %d bytes, expected %d", o, want)
	}

	nominal := float32(cfg.FnomHz)
	if nominal <= 0 {
		nominal = 50
	}
	frequencyDeviation := frequency - nominal

	seqPos, seqNeg, seqZero, voltageImbalance := calcSequenceMetrics(va, vb, vc)
	vab_diff := vb.PhaseDegrees - va.PhaseDegrees
	if vab_diff > 180 {
		vab_diff -= 360
	} else if vab_diff < -180 {
		vab_diff += 360
	}
	vbc_diff := vc.PhaseDegrees - vb.PhaseDegrees
	if vbc_diff > 180 {
		vbc_diff -= 360
	} else if vbc_diff < -180 {
		vbc_diff += 360
	}
	vca_diff := va.PhaseDegrees - vc.PhaseDegrees
	if vca_diff > 180 {
		vca_diff -= 360
	} else if vca_diff < -180 {
		vca_diff += 360
	}

	s := math.Sqrt(float64(mw*mw + mvar*mvar))
	pf := float32(1.0)
	pfDir := "unity"
	if s > 0.1 {
		pf = mw / float32(s)
		if pf > 1.0 {
			pf = 1.0
		}
		if mvar > 0 {
			pfDir = "lagging"
		} else if mvar < 0 {
			pfDir = "leading"
		}
	}

	return Reading{
		PMUName:                  pmuName,
		SyncWord:                 syncWord,
		FrameType:                "DATA",
		FrameSize:                frameSize,
		IDCode:                   idCode,
		SOC:                      socRaw,
		FracSecRaw:               fracsec,
		TimeQuality:              timeQuality,
		MsgTQ:                    msgTQ,
		FracSecCount:             fracCount,
		Checksum:                 checksumRx,
		ChecksumValid:            true,
		Timestamp:                ts,
		FrameBytes:               len(raw),
		Stat:                     stat,
		StatDetail:               decodeStat(stat),
		Phasors:                  namedPhasors,
		Analogs:                  namedAnalogs,
		VA:                       va,
		VB:                       vb,
		VC:                       vc,
		IA:                       ia,
		VoltageImbalancePercent:  voltageImbalance,
		VAB_PhaseAngleDifference: vab_diff,
		VBC_PhaseAngleDifference: vbc_diff,
		VCA_PhaseAngleDifference: vca_diff,
		SequencePos:              seqPos,
		SequenceNeg:              seqNeg,
		SequenceZero:             seqZero,
		Frequency:                frequency,
		FrequencyDeviation:       frequencyDeviation,
		ROCOF:                    rocof,
		MW:                       mw,
		MVAR:                     mvar,
		MVA:                      float32(s),
		PowerFactor:              pf,
		PowerFactorLeadLag:       pfDir,
		TotalPowerReal:           va.Real*ia.Real + va.Imag*ia.Imag,
		TotalPowerImag:           va.Imag*ia.Real - va.Real*ia.Imag,
		Digital:                  digital,
		Digitals:                 digitals,
		DigitalNames:             digitalNames,
	}, nil
}

// crc16 computes the CRC-CCITT (0xFFFF) used by C37.118
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
