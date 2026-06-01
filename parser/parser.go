package parser

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	frameTypeData = 0x00
	minFrameSize  = 68
)

// STATDecoded represents all 9 decoded flags from the STAT word
type STATDecoded struct {
	SyncPMU          bool  // bit 15: 1 if data out of sync
	DataErr          bool  // bit 14: 1 if data error
	CFGChange        bool  // bit 13: 1 if config change pending
	TriggerDetected  bool  // bit 12: 1 if trigger event detected
	SortMethod       bool  // bit 11: 1 if data sorted by arrival time, 0 if by sample time
	PMUSyncStatus    bool  // bit 10: 1 if GPS not locked
	PMUTimeQuality   uint8 // bits 9-7: time quality code (0=locked to UTC, 1-7 various unlocked states)
	UnlockedDuration uint8 // bits 6-5: unlocked time range (0=<5s, 1=5-10s, 2=10-60s, 3=>60s)
	DSO              bool  // bit 4: 1 if data was post-processed/digital signature output
	PMUTriggerReason uint8 // bits 3-0: trigger reason code (0=manual, 1-15 various fault types)
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

// Reading is the parsed payload from one C37.118 data frame.
type Reading struct {
	// ─ Identity & Timing
	PMUName       string    `json:"pmu_name"`
	SyncWord      uint16    `json:"sync_word"`
	FrameType     string    `json:"frame_type"`
	FrameSize     int       `json:"frame_size"`
	IDCode        uint16    `json:"idcode"`
	SOC           uint32    `json:"soc"`
	FracSecRaw    uint32    `json:"fracsec_raw"`
	TimeQuality   uint8     `json:"time_quality"`
	FracSecCount  uint32    `json:"fracsec_count"`
	Checksum      uint16    `json:"checksum"`
	ChecksumValid bool      `json:"checksum_valid"`
	Timestamp     time.Time `json:"timestamp"`
	FrameBytes    int       `json:"frame_bytes"`

	// ─ STAT Word (decoded)
	Stat       uint16      `json:"stat_raw"`
	StatDetail STATDecoded `json:"stat_decoded"`

	// ─ Phasors (raw components + derived metrics)
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
}

// decodeStat unpacks all 9 flag fields from the STAT word
func decodeStat(stat uint16) STATDecoded {
	return STATDecoded{
		SyncPMU:          (stat>>15)&1 == 1,
		DataErr:          (stat>>14)&1 == 1,
		CFGChange:        (stat>>13)&1 == 1,
		TriggerDetected:  (stat>>12)&1 == 1,
		SortMethod:       (stat>>11)&1 == 1,
		PMUSyncStatus:    (stat>>10)&1 == 1,
		PMUTimeQuality:   uint8((stat >> 7) & 0x7),
		UnlockedDuration: uint8((stat >> 5) & 0x3),
		DSO:              (stat>>4)&1 == 1,
		PMUTriggerReason: uint8(stat & 0xF),
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

// ParseDataFrame decodes one CRC-verified C37.118 data frame.
func ParseDataFrame(pmuName string, raw []byte) (Reading, error) {
	if len(raw) < minFrameSize {
		return Reading{}, fmt.Errorf("short frame: got %d bytes", len(raw))
	}
	if raw[0] != 0xAA {
		return Reading{}, fmt.Errorf("invalid sync byte: 0x%02X", raw[0])
	}
	if raw[1]&0xF0 != frameTypeData {
		return Reading{}, fmt.Errorf("not a data frame: type=0x%02X", raw[1]&0xF0)
	}

	frameSize := int(binary.BigEndian.Uint16(raw[2:4]))
	if frameSize != len(raw) {
		return Reading{}, fmt.Errorf("frame size mismatch: header=%d actual=%d", frameSize, len(raw))
	}

	syncWord := binary.BigEndian.Uint16(raw[0:2])
	idCode := binary.BigEndian.Uint16(raw[4:6])
	socRaw := binary.BigEndian.Uint32(raw[6:10])
	soc := int64(socRaw)
	fracsec := binary.BigEndian.Uint32(raw[10:14])
	checksumRx := binary.BigEndian.Uint16(raw[len(raw)-2:])
	checksumCalc := crc16(raw[:len(raw)-2])
	checksumValid := checksumRx == checksumCalc

	timeQuality := uint8(fracsec >> 24)

	// Simulator uses TIME_BASE=1_000_000 and stores fraction count in lower 24 bits.
	fracCountRaw := fracsec & 0x00FFFFFF
	fracCount := int64(fracCountRaw)
	nanos := (fracCount * int64(time.Second)) / 1_000_000
	ts := time.Unix(soc, nanos).UTC()

	p := raw[14 : len(raw)-2]
	if len(p) < 52 {
		return Reading{}, fmt.Errorf("short payload: got %d bytes", len(p))
	}

	o := 0
	stat := binary.BigEndian.Uint16(p[o : o+2])
	o += 2

	readF32 := func() float32 {
		v := math.Float32frombits(binary.BigEndian.Uint32(p[o : o+4]))
		o += 4
		return v
	}

	// Parse raw phasor components
	va_r, va_i := readF32(), readF32()
	vb_r, vb_i := readF32(), readF32()
	vc_r, vc_i := readF32(), readF32()
	ia_r, ia_i := readF32(), readF32()

	// Calculate phasor metrics (magnitude, phase, power)
	va := calcPhasor(va_r, va_i)
	vb := calcPhasor(vb_r, vb_i)
	vc := calcPhasor(vc_r, vc_i)
	ia := calcPhasor(ia_r, ia_i)

	// Frequency and ROCOF
	frequency := readF32()
	rocof := readF32()
	frequencyDeviation := frequency - 50.0 // nominal 50Hz

	// MW and MVAR
	mw := readF32()
	mvar := readF32()

	// Digital word
	digital := binary.BigEndian.Uint16(p[o : o+2])

	// Calculate derived metrics
	seqPos, seqNeg, seqZero, voltageImbalance := calcSequenceMetrics(va, vb, vc)

	// Phase angle differences (separation between phases)
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

	// Apparent power S = √(P² + Q²), Power Factor = P/S
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

	// Calculate power from VA/IA
	va_ia_real := va.Real*ia.Real + va.Imag*ia.Imag
	va_ia_imag := va.Imag*ia.Real - va.Real*ia.Imag
	totalPowerReal := va_ia_real
	totalPowerImag := va_ia_imag

	reading := Reading{
		PMUName:                  pmuName,
		SyncWord:                 syncWord,
		FrameType:                "DATA",
		FrameSize:                frameSize,
		IDCode:                   idCode,
		SOC:                      socRaw,
		FracSecRaw:               fracsec,
		TimeQuality:              timeQuality,
		FracSecCount:             fracCountRaw,
		Checksum:                 checksumRx,
		ChecksumValid:            checksumValid,
		Timestamp:                ts,
		FrameBytes:               len(raw),
		Stat:                     stat,
		StatDetail:               decodeStat(stat),
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
		TotalPowerReal:           totalPowerReal,
		TotalPowerImag:           totalPowerImag,
		Digital:                  digital,
	}

	return reading, nil
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
