package parser

import (
	"encoding/binary"
	"fmt"
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
	Station  string
	IDCode   uint16
	TimeBase uint32
	NumPMU   uint16
	Format   uint16
	Phnmr    int
	Annmr    int
	Dgnmr    int
	FnomHz   int
	FnomWord uint16
	CfgCnt   uint16
	DataRate int16
	Channels []string
	PhUnits  []PhUnit
	AnUnits  []AnUnit
	DigUnits []DigUnit
	Polar     bool
	PhFloat   bool
	AnFloat   bool
	FreqFloat bool
	// DataPayloadBytes is the expected DATA body size for the first PMU block.
	DataPayloadBytes int
	// TotalDataPayloadBytes sums all PMU blocks (for multi-PMU frames).
	TotalDataPayloadBytes int
}

var profileRegistry sync.Map // map[string]Profile

// SetProfile stores the CFG2-derived layout for a PMU.
func SetProfile(pmuName string, p Profile) {
	profileRegistry.Store(pmuName, p)
}

// GetProfile returns the CFG2 layout if the receiver registered one.
func GetProfile(pmuName string) (Profile, bool) {
	v, ok := profileRegistry.Load(pmuName)
	if !ok {
		return Profile{}, false
	}
	p, ok := v.(Profile)
	return p, ok
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
	timeBase := binary.BigEndian.Uint32(p[o:])
	o += 4
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

func mapPhasorsToStandard(names []string, phasors []Phasor) (va, vb, vc, ia Phasor) {
	for i, name := range names {
		if i >= len(phasors) {
			break
		}
		n := strings.ToUpper(strings.TrimSpace(name))
		switch {
		case strings.HasSuffix(n, "AV") || n == "VA":
			va = phasors[i]
		case strings.HasSuffix(n, "BV") || n == "VB":
			vb = phasors[i]
		case strings.HasSuffix(n, "CV") || n == "VC":
			vc = phasors[i]
		case strings.HasSuffix(n, "AI") || n == "IA":
			ia = phasors[i]
		}
	}
	// Legacy simulator order: VA, VB, VC, IA at indices 0..3
	if va.Magnitude == 0 && vb.Magnitude == 0 && len(phasors) >= 4 {
		va, vb, vc, ia = phasors[0], phasors[1], phasors[2], phasors[3]
	}
	return va, vb, vc, ia
}
