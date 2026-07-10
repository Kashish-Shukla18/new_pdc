package parser

import (
	"encoding/binary"
	"math"
	"testing"
)

func putCRC(buf []byte) {
	c := crc16(buf[:len(buf)-2])
	binary.BigEndian.PutUint16(buf[len(buf)-2:], c)
}

func TestDecodeStat_C371182011(t *testing.T) {
	// bits 15-14=00, 13=1 cfg, 12=1 trig, 11=0, 10=1 unlocked,
	// 9-6=TQ=5, 5-4=unlocked=2 (100-1000s), 3-0=reason=7
	stat := uint16(0x0000)
	stat |= 1 << 13
	stat |= 1 << 12
	stat |= 1 << 10
	stat |= 5 << 6
	stat |= 2 << 4
	stat |= 7

	d := decodeStat(stat)
	if d.DataErrorCode != 0 || d.DataError {
		t.Fatalf("data error: %+v", d)
	}
	if !d.CFGChange || !d.TriggerDetected || d.SortMethod || !d.PMUSyncStatus {
		t.Fatalf("flags: %+v", d)
	}
	if d.PMUTimeQuality != 5 {
		t.Fatalf("TQ=%d want 5", d.PMUTimeQuality)
	}
	if d.UnlockedDuration != 2 {
		t.Fatalf("unlocked=%d want 2", d.UnlockedDuration)
	}
	if d.PMUTriggerReason != 7 {
		t.Fatalf("reason=%d want 7", d.PMUTriggerReason)
	}

	// bits 15-14 = 11 (invalid)
	bad := uint16(0xC000)
	d2 := decodeStat(bad)
	if d2.DataErrorCode != 3 || !d2.DataError {
		t.Fatalf("error code: %+v", d2)
	}
}

func TestDecodeMsgTQ(t *testing.T) {
	// bit6 leap dir, bit5 occurred, bit4 pending, code=0xA → 0x7A
	frac := uint32(0x7A000000)
	m := decodeMsgTQ(frac)
	if !m.LeapSecondDirection || !m.LeapSecondOccurred || !m.LeapSecondPending {
		t.Fatalf("leap flags: %+v", m)
	}
	if m.TimeQualityCode != 0xA {
		t.Fatalf("code=%d", m.TimeQualityCode)
	}
}

func TestExpectedDataPayloadSize_FloatPolar(t *testing.T) {
	// PMU.001-like: 6 float polar phasors, float freq, 3 float analogs, 1 digital
	cfg := Profile{Phnmr: 6, Annmr: 3, Dgnmr: 1, PhFloat: true, AnFloat: true, FreqFloat: true}
	// 2 + 6*8 + 8 + 3*4 + 2 = 2+48+8+12+2 = 72
	if got := ExpectedDataPayloadSize(cfg); got != 72 {
		t.Fatalf("got %d want 72", got)
	}
}

func TestParseCFG2AndData_FloatPolar(t *testing.T) {
	// Build minimal CFG2: 1 PMU, 1 float polar phasor, 0 analog, 0 digital, float freq
	// Header 14 + payload + CHK 2
	// payload: TIME_BASE(4) NUM_PMU(2) STN(16) ID(2) FORMAT(2) PHNMR(2) ANNMR(2) DGNMR(2)
	//          CHNAM(16) PHUNIT(4) FNOM(2) CFGCNT(2) DATA_RATE(2)
	payloadLen := 4 + 2 + 16 + 2 + 2 + 2 + 2 + 2 + 16 + 4 + 2 + 2 + 2
	frame := make([]byte, 14+payloadLen+2)
	frame[0] = 0xAA
	frame[1] = 0x32 // CFG2 version 2
	binary.BigEndian.PutUint16(frame[2:], uint16(len(frame)))
	binary.BigEndian.PutUint16(frame[4:], 1) // idcode
	binary.BigEndian.PutUint32(frame[6:], 1000)
	binary.BigEndian.PutUint32(frame[10:], 0)

	p := frame[14:]
	o := 0
	binary.BigEndian.PutUint32(p[o:], 1_000_000)
	o += 4
	binary.BigEndian.PutUint16(p[o:], 1)
	o += 2
	copy(p[o:], []byte("TEST-STATION    "))
	o += 16
	binary.BigEndian.PutUint16(p[o:], 1)
	o += 2
	binary.BigEndian.PutUint16(p[o:], 0x000B) // polar|ph float|freq float
	o += 2
	binary.BigEndian.PutUint16(p[o:], 1) // phnmr
	o += 2
	binary.BigEndian.PutUint16(p[o:], 0)
	o += 2
	binary.BigEndian.PutUint16(p[o:], 0)
	o += 2
	copy(p[o:], []byte("VA              "))
	o += 16
	binary.BigEndian.PutUint32(p[o:], 0) // PHUNIT voltage, factor 0
	o += 4
	binary.BigEndian.PutUint16(p[o:], 0) // FNOM 60Hz
	o += 2
	binary.BigEndian.PutUint16(p[o:], 7) // CFGCNT
	o += 2
	binary.BigEndian.PutUint16(p[o:], 30) // DATA_RATE
	o += 2
	if o != payloadLen {
		t.Fatalf("payload build o=%d want %d", o, payloadLen)
	}
	putCRC(frame)

	prof, err := ParseCFG2Frame(frame)
	if err != nil {
		t.Fatalf("ParseCFG2Frame: %v", err)
	}
	if prof.Station != "TEST-STATION" || prof.Phnmr != 1 || !prof.Polar || !prof.PhFloat || !prof.FreqFloat {
		t.Fatalf("profile: %+v", prof)
	}
	if prof.CfgCnt != 7 || prof.DataRate != 30 || prof.FnomHz != 60 {
		t.Fatalf("meta: cfgcnt=%d rate=%d fnom=%d", prof.CfgCnt, prof.DataRate, prof.FnomHz)
	}
	if prof.DataPayloadBytes != ExpectedDataPayloadSize(prof) {
		t.Fatalf("payload bytes %d vs %d", prof.DataPayloadBytes, ExpectedDataPayloadSize(prof))
	}

	SetProfile("t1", prof)

	// DATA: STAT + mag/ang float + freq/rocof float
	body := 2 + 8 + 8
	data := make([]byte, 14+body+2)
	data[0] = 0xAA
	data[1] = 0x02
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)))
	binary.BigEndian.PutUint16(data[4:], 1)
	binary.BigEndian.PutUint32(data[6:], 2000)
	binary.BigEndian.PutUint32(data[10:], 500000) // frac count with TIME_BASE 1e6 → 0.5s

	b := data[14:]
	binary.BigEndian.PutUint16(b[0:], 0) // STAT good
	binary.BigEndian.PutUint32(b[2:], math.Float32bits(110.0))
	binary.BigEndian.PutUint32(b[6:], math.Float32bits(float32(math.Pi/2)))
	binary.BigEndian.PutUint32(b[10:], math.Float32bits(60.01))
	binary.BigEndian.PutUint32(b[14:], math.Float32bits(0.02))
	putCRC(data)

	r, err := ParseDataFrame("t1", data)
	if err != nil {
		t.Fatalf("ParseDataFrame: %v", err)
	}
	if math.Abs(float64(r.VA.Magnitude)-110) > 0.01 {
		t.Fatalf("mag=%v", r.VA.Magnitude)
	}
	if math.Abs(float64(r.VA.PhaseDegrees)-90) > 0.1 {
		t.Fatalf("deg=%v", r.VA.PhaseDegrees)
	}
	if math.Abs(float64(r.Frequency)-60.01) > 0.001 {
		t.Fatalf("f=%v", r.Frequency)
	}
	if !r.ChecksumValid {
		t.Fatal("crc")
	}
}

func TestIntegerPolarScaling(t *testing.T) {
	cfg := Profile{
		Station: "I", IDCode: 1, TimeBase: 1_000_000, NumPMU: 1,
		Phnmr: 1, Annmr: 0, Dgnmr: 0, FnomHz: 60,
		Channels: []string{"VA"},
		Polar: true, PhFloat: false, FreqFloat: false,
		PhUnits: []PhUnit{{Factor: 0.01}}, // 1e-2
	}
	// Also set format-derived sizes via ExpectedDataPayloadSize
	cfg.DataPayloadBytes = ExpectedDataPayloadSize(cfg)
	cfg.TotalDataPayloadBytes = cfg.DataPayloadBytes
	SetProfile("int1", cfg)

	body := cfg.DataPayloadBytes
	data := make([]byte, 14+body+2)
	data[0], data[1] = 0xAA, 0x02
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)))
	binary.BigEndian.PutUint16(data[4:], 1)
	b := data[14:]
	o := 0
	binary.BigEndian.PutUint16(b[o:], 0)
	o += 2
	binary.BigEndian.PutUint16(b[o:], 1000) // mag count → 1000*0.01=10
	o += 2
	binary.BigEndian.PutUint16(b[o:], uint16(int16(15708))) // ~π/2 * 10000
	o += 2
	binary.BigEndian.PutUint16(b[o:], 10) // +10 mHz
	o += 2
	binary.BigEndian.PutUint16(b[o:], 5) // ROCOF 0.05
	o += 2
	if o != body {
		t.Fatalf("o=%d body=%d", o, body)
	}
	putCRC(data)

	r, err := ParseDataFrame("int1", data)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if math.Abs(float64(r.VA.Magnitude)-10) > 0.01 {
		t.Fatalf("mag=%v", r.VA.Magnitude)
	}
	if math.Abs(float64(r.VA.PhaseDegrees)-90) > 1 {
		t.Fatalf("deg=%v (rad=%v)", r.VA.PhaseDegrees, r.VA.PhaseRadians)
	}
	if math.Abs(float64(r.Frequency)-60.01) > 0.0001 {
		t.Fatalf("f=%v", r.Frequency)
	}
	if math.Abs(float64(r.ROCOF)-0.05) > 0.001 {
		t.Fatalf("rocof=%v", r.ROCOF)
	}
}

func TestBuildCMDVersion(t *testing.T) {
	// Ensure CRC helper works on version-2 style header bytes
	buf := []byte{0xAA, 0x42, 0, 18, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0}
	putCRC(buf)
	if crc16(buf[:16]) != binary.BigEndian.Uint16(buf[16:]) {
		t.Fatal("crc")
	}
}
