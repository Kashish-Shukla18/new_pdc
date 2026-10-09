package parser

// cfg2.go — decode the CFG-2 "channel map" frame from a PMU.
//
// After handshake we remember the layout in RAM (SetProfile).
// DATA frames are useless without this map — names, scales, format bits.

import (
	"encoding/binary"
	"fmt"
	"log"
	"strings"
	"sync"
)

// PhUnit is one PHUNIT word (IEEE C37.118.2-2011 §6.4).
// Bits 31–24: 0=voltage, 1=current; bits 23–0: scale × 10⁻⁵.
type PhUnit struct {
	IsCurrent bool
	Factor    float64 // engineering units per integer count
}

// AnUnit is one ANUNIT word.
// Bits 31–24: analog type; bits 23–0: scale × 10⁻⁵.
type AnUnit struct {
	Type   uint8
	Factor float64
}

// DigUnit is one DIGUNIT word: normal-status mask + valid-inputs mask.
type DigUnit struct {
	NormalMask uint16
	ValidMask  uint16
}

// Profile describes PMU channel layout from a CFG2 frame.
type Profile struct {
	Station   string
	IDCode    uint16
	SyncWord  uint16
	TimeBase  uint32
	NumPMU    uint16
	Format    uint16
	Phnmr     int
	Annmr     int
	Dgnmr     int
	FnomHz    int
	FnomWord  uint16
	CfgCnt    uint16
	DataRate  int16
	Channels  []string
	PhUnits   []PhUnit
	AnUnits   []AnUnit
	DigUnits  []DigUnit
	Polar     bool
	PhFloat   bool
	AnFloat   bool
	FreqFloat bool
	// DataPayloadBytes is the expected DATA body size for the first PMU block.
	DataPayloadBytes int
	// TotalDataPayloadBytes sums all PMU blocks (for multi-PMU frames).
	TotalDataPayloadBytes int
	// HeaderText is optional ASCII from a Header frame (CMD 0x0003).
	HeaderText string
}

var profileRegistry sync.Map // map[string]Profile — filled from live CFG2 handshake

// GetProfile returns the CFG-2 channel layout for a PMU (from the last handshake).
func GetProfile(pmuName string) (Profile, bool) {
	v, ok := profileRegistry.Load(pmuName)
	if !ok {
		return Profile{}, false
	}
	p, ok := v.(Profile)
	return p, ok
}

// SetProfile remembers a CFG-2 layout in RAM after we talk to the PMU.
// We need this to unpack later DATA frames (they only make sense with CFG-2).
func SetProfile(pmuName string, p Profile) {
	profileRegistry.Store(pmuName, p)
}

// ExpectedDataPayloadSize returns the byte length of one PMU's DATA body
// (STAT through digital words), per FORMAT / channel counts.
func ExpectedDataPayloadSize(cfg Profile) int {
	phSize := 4
	if cfg.PhFloat {
		phSize = 8
	}
	freqSize := 4
	if cfg.FreqFloat {
		freqSize = 8
	}
	anSize := 2
	if cfg.AnFloat {
		anSize = 4
	}
	return 2 + cfg.Phnmr*phSize + freqSize + cfg.Annmr*anSize + cfg.Dgnmr*2
}

func parsePhUnit(w uint32) PhUnit {
	return PhUnit{
		IsCurrent: (w >> 24) != 0,
		Factor:    float64(w&0x00FFFFFF) * 1e-5,
	}
}

func parseAnUnit(w uint32) AnUnit {
	return AnUnit{
		Type:   uint8(w >> 24),
		Factor: float64(w&0x00FFFFFF) * 1e-5,
	}
}

func parseDigUnit(w uint32) DigUnit {
	return DigUnit{
		NormalMask: uint16(w >> 16),
		ValidMask:  uint16(w & 0xFFFF),
	}
}

// parseOnePMUBlock parses one PMU configuration block starting at p[o].
func parseOnePMUBlock(p []byte, o int) (station string, id uint16, format uint16, phnmr, annmr, dgnmr int, channels []string, phUnits []PhUnit, anUnits []AnUnit, digUnits []DigUnit, fnomWord, cfgCnt uint16, next int, err error) {
	if len(p) < o+26 {
		return "", 0, 0, 0, 0, 0, nil, nil, nil, nil, 0, 0, o, fmt.Errorf("PMU block truncated at %d", o)
	}
	station = trimASCII(p[o : o+16])
	o += 16
	id = binary.BigEndian.Uint16(p[o:])
	o += 2
	format = binary.BigEndian.Uint16(p[o:])
	o += 2
	phnmr = int(binary.BigEndian.Uint16(p[o:]))
	o += 2
	annmr = int(binary.BigEndian.Uint16(p[o:]))
	o += 2
	dgnmr = int(binary.BigEndian.Uint16(p[o:]))
	o += 2

	chnCount := phnmr + annmr + 16*dgnmr
	if len(p) < o+chnCount*16 {
		return "", 0, 0, 0, 0, 0, nil, nil, nil, nil, 0, 0, o, fmt.Errorf("channel names truncated")
	}
	channels = make([]string, chnCount)
	for i := 0; i < chnCount; i++ {
		channels[i] = trimASCII(p[o+i*16 : o+(i+1)*16])
	}
	o += chnCount * 16

	if len(p) < o+phnmr*4+annmr*4+dgnmr*4+4 {
		return "", 0, 0, 0, 0, 0, nil, nil, nil, nil, 0, 0, o, fmt.Errorf("unit/fnom block truncated")
	}

	phUnits = make([]PhUnit, phnmr)
	for i := 0; i < phnmr; i++ {
		phUnits[i] = parsePhUnit(binary.BigEndian.Uint32(p[o:]))
		o += 4
	}
	anUnits = make([]AnUnit, annmr)
	for i := 0; i < annmr; i++ {
		anUnits[i] = parseAnUnit(binary.BigEndian.Uint32(p[o:]))
		o += 4
	}
	digUnits = make([]DigUnit, dgnmr)
	for i := 0; i < dgnmr; i++ {
		digUnits[i] = parseDigUnit(binary.BigEndian.Uint32(p[o:]))
		o += 4
	}

	fnomWord = binary.BigEndian.Uint16(p[o:])
	o += 2
	cfgCnt = binary.BigEndian.Uint16(p[o:])
	o += 2
	return station, id, format, phnmr, annmr, dgnmr, channels, phUnits, anUnits, digUnits, fnomWord, cfgCnt, o, nil
}

// ParseCFG2Frame unpacks a CFG2 frame into a Profile (first PMU block).
// All NUM_PMU blocks are walked so DATA_RATE is read at the correct offset.
func ParseCFG2Frame(raw []byte) (Profile, error) {
	if len(raw) < 40 || raw[0] != 0xAA || (raw[1]&0x70) != 0x30 {
		return Profile{}, fmt.Errorf("not a CFG2 frame")
	}
	// Version bits 3-0 should be 2 for C37.118.2-2011; accept 1 for interoperability.
	ver := raw[1] & 0x0F
	if ver != 1 && ver != 2 {
		return Profile{}, fmt.Errorf("unsupported SYNC version %d", ver)
	}

	p := raw[14 : len(raw)-2]
	if len(p) < 34 {
		return Profile{}, fmt.Errorf("CFG2 payload too short")
	}
	o := 0
	timeBaseRaw := binary.BigEndian.Uint32(p[o:])
	o += 4
	// IEEE C37.118.2: TIME_BASE is 24-bit; bits 31–24 are reserved.
	if timeBaseRaw&0xFF000000 != 0 {
		log.Printf("[parser] CFG2 TIME_BASE has reserved bits set (0x%08X); masking to 24 bits", timeBaseRaw)
	}
	timeBase := timeBaseRaw & 0x00FFFFFF
	numPMU := binary.BigEndian.Uint16(p[o:])
	o += 2
	if numPMU < 1 {
		return Profile{}, fmt.Errorf("NUM_PMU=%d", numPMU)
	}

	var (
		station                      string
		id, format, fnomWord, cfgCnt uint16
		phnmr, annmr, dgnmr          int
		channels                     []string
		phUnits                      []PhUnit
		anUnits                      []AnUnit
		digUnits                     []DigUnit
		err                          error
		firstPayload                 int
		totalPayload                 int
	)

	for i := 0; i < int(numPMU); i++ {
		var st string
		var idI, fmtI, fnI, cfgI uint16
		var ph, an, dg int
		var ch []string
		var pu []PhUnit
		var au []AnUnit
		var du []DigUnit
		st, idI, fmtI, ph, an, dg, ch, pu, au, du, fnI, cfgI, o, err = parseOnePMUBlock(p, o)
		if err != nil {
			return Profile{}, fmt.Errorf("PMU[%d]: %w", i, err)
		}
		block := Profile{
			Phnmr: ph, Annmr: an, Dgnmr: dg,
			PhFloat: fmtI&0x0002 != 0, AnFloat: fmtI&0x0004 != 0, FreqFloat: fmtI&0x0008 != 0,
		}
		sz := ExpectedDataPayloadSize(block)
		totalPayload += sz
		if i == 0 {
			station, id, format = st, idI, fmtI
			phnmr, annmr, dgnmr = ph, an, dg
			channels, phUnits, anUnits, digUnits = ch, pu, au, du
			fnomWord, cfgCnt = fnI, cfgI
			firstPayload = sz
		}
	}

	if len(p) < o+2 {
		return Profile{}, fmt.Errorf("DATA_RATE truncated")
	}
	dataRate := int16(binary.BigEndian.Uint16(p[o:]))
	o += 2
	if o != len(p) {
		return Profile{}, fmt.Errorf("CFG2 trailing bytes: consumed=%d payload=%d", o, len(p))
	}

	fnom := 60
	if fnomWord&1 == 1 {
		fnom = 50
	}

	return Profile{
		Station:               station,
		IDCode:                id,
		SyncWord:              binary.BigEndian.Uint16(raw[0:2]),
		TimeBase:              timeBase,
		NumPMU:                numPMU,
		Format:                format,
		Phnmr:                 phnmr,
		Annmr:                 annmr,
		Dgnmr:                 dgnmr,
		FnomHz:                fnom,
		FnomWord:              fnomWord,
		CfgCnt:                cfgCnt,
		DataRate:              dataRate,
		Channels:              channels,
		PhUnits:               phUnits,
		AnUnits:               anUnits,
		DigUnits:              digUnits,
		Polar:                 format&0x0001 != 0,
		PhFloat:               format&0x0002 != 0,
		AnFloat:               format&0x0004 != 0,
		FreqFloat:             format&0x0008 != 0,
		DataPayloadBytes:      firstPayload,
		TotalDataPayloadBytes: totalPayload,
	}, nil
}

func trimASCII(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func phasorNames(p Profile) []string {
	if p.Phnmr <= 0 || len(p.Channels) < p.Phnmr {
		return nil
	}
	return p.Channels[:p.Phnmr]
}

// ClassifyPhasorName maps a CFG channel name onto VA/VB/VC/IA/IB/IC ("" if unknown).
// Handles common aliases: V1/I1, VAN, "PHASOR CH 1:V1", …AV/…AI, etc.
func ClassifyPhasorName(name string) string {
	n := strings.ToUpper(strings.TrimSpace(name))
	// "PHASOR CH 1:V1" / "CH1:IA" → classify the label after the last colon.
	if i := strings.LastIndex(n, ":"); i >= 0 && i+1 < len(n) {
		n = n[i+1:]
	}
	n = strings.ReplaceAll(n, " ", "")
	n = strings.ReplaceAll(n, "_", "")
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, ".", "")
	if n == "" {
		return ""
	}
	switch n {
	case "VA", "VAN", "V1", "PHASEA", "PHASEAV", "VOLTAGEA":
		return "va"
	case "VB", "VBN", "V2", "PHASEB", "PHASEBV", "VOLTAGEB":
		return "vb"
	case "VC", "VCN", "V3", "PHASEC", "PHASECV", "VOLTAGEC":
		return "vc"
	case "IA", "IAN", "I1", "CURRENTA", "PHASEAI":
		return "ia"
	case "IB", "IBN", "I2", "CURRENTB", "PHASEBI":
		return "ib"
	case "IC", "ICN", "I3", "CURRENTC", "PHASECI":
		return "ic"
	}
	switch {
	case strings.HasSuffix(n, "AV") || strings.HasPrefix(n, "VA"):
		return "va"
	case strings.HasSuffix(n, "BV") || strings.HasPrefix(n, "VB"):
		return "vb"
	case strings.HasSuffix(n, "CV") || strings.HasPrefix(n, "VC"):
		return "vc"
	case strings.HasSuffix(n, "AI") || strings.HasPrefix(n, "IA"):
		return "ia"
	case strings.HasSuffix(n, "BI") || strings.HasPrefix(n, "IB"):
		return "ib"
	case strings.HasSuffix(n, "CI") || strings.HasPrefix(n, "IC"):
		return "ic"
	case strings.HasPrefix(n, "V1") || strings.HasSuffix(n, "V1"):
		return "va"
	case strings.HasPrefix(n, "V2") || strings.HasSuffix(n, "V2"):
		return "vb"
	case strings.HasPrefix(n, "V3") || strings.HasSuffix(n, "V3"):
		return "vc"
	case strings.HasPrefix(n, "I1") || strings.HasSuffix(n, "I1"):
		return "ia"
	case strings.HasPrefix(n, "I2") || strings.HasSuffix(n, "I2"):
		return "ib"
	case strings.HasPrefix(n, "I3") || strings.HasSuffix(n, "I3"):
		return "ic"
	}
	return ""
}

// ResolveNominalHz picks the FNOM used for Δf.
// Absolute float FREQ near 50/60 while CFG FNOM is the other band is a common
// misconfig; snap nominal to the band the measurement sits in.
func ResolveNominalHz(freq float32, fnom int, freqFloat bool) float32 {
	nominal := float32(fnom)
	if nominal <= 0 {
		nominal = 50
	}
	if !freqFloat {
		return nominal
	}
	alt := float32(60)
	if nominal >= 55 {
		alt = 50
	}
	if abs32(freq-nominal) > 5 && abs32(freq-alt) < 3 {
		return alt
	}
	return nominal
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func mapPhasorsToStandard(names []string, phasors []Phasor) (va, vb, vc, ia Phasor) {
	var voltages, currents []Phasor
	for i, name := range names {
		if i >= len(phasors) {
			break
		}
		ph := phasors[i]
		switch ClassifyPhasorName(name) {
		case "va":
			va = ph
			voltages = append(voltages, ph)
		case "vb":
			vb = ph
			voltages = append(voltages, ph)
		case "vc":
			vc = ph
			voltages = append(voltages, ph)
		case "ia":
			ia = ph
			currents = append(currents, ph)
		case "ib", "ic":
			currents = append(currents, ph)
		default:
			n := strings.ToUpper(strings.TrimSpace(name))
			if strings.HasPrefix(n, "V") {
				voltages = append(voltages, ph)
			} else if strings.HasPrefix(n, "I") {
				currents = append(currents, ph)
			}
		}
	}
	if va.Magnitude == 0 && len(voltages) > 0 {
		va = voltages[0]
	}
	if vb.Magnitude == 0 && len(voltages) > 1 {
		vb = voltages[1]
	}
	if vc.Magnitude == 0 && len(voltages) > 2 {
		vc = voltages[2]
	}
	if ia.Magnitude == 0 && len(currents) > 0 {
		ia = currents[0]
	}
	// Legacy simulator order: VA, VB, VC, IA at indices 0..3
	if va.Magnitude == 0 && vb.Magnitude == 0 && len(phasors) >= 4 {
		va, vb, vc, ia = phasors[0], phasors[1], phasors[2], phasors[3]
	}
	// Single-voltage / voltage+current devices (e.g. V1, I1).
	if va.Magnitude == 0 && len(phasors) >= 1 {
		va = phasors[0]
	}
	if ia.Magnitude == 0 && len(phasors) >= 2 {
		// Prefer a current-classified channel when names exist.
		if len(currents) > 0 {
			ia = currents[0]
		} else if len(names) >= 2 && ClassifyPhasorName(names[1]) == "ia" {
			ia = phasors[1]
		} else if len(names) == 0 || strings.HasPrefix(strings.ToUpper(strings.TrimSpace(names[1])), "I") {
			ia = phasors[1]
		}
	}
	return va, vb, vc, ia
}
