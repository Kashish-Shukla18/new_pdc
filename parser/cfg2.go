package parser

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
)

// Profile describes PMU channel layout from a CFG2 frame.
type Profile struct {
	Station   string
	IDCode    uint16
	TimeBase  uint32
	Format    uint16
	Phnmr     int
	Annmr     int
	Dgnmr     int
	FnomHz    int
	DataRate  int16
	Channels  []string
	Polar     bool
	PhFloat   bool
	AnFloat   bool
	FreqFloat bool
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

// ParseCFG2Frame unpacks a CFG2 frame into a Profile.
func ParseCFG2Frame(raw []byte) (Profile, error) {
	if len(raw) < 40 || raw[0] != 0xAA || (raw[1]&0x70) != 0x30 {
		return Profile{}, fmt.Errorf("not a CFG2 frame")
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
	station := trimASCII(p[o : o+16])
	o += 16
	id := binary.BigEndian.Uint16(p[o:])
	o += 2
	format := binary.BigEndian.Uint16(p[o:])
	o += 2
	phnmr := int(binary.BigEndian.Uint16(p[o:]))
	o += 2
	annmr := int(binary.BigEndian.Uint16(p[o:]))
	o += 2
	dgnmr := int(binary.BigEndian.Uint16(p[o:]))
	o += 2

	chnCount := phnmr + annmr + 16*dgnmr
	if len(p) < o+chnCount*16 {
		return Profile{}, fmt.Errorf("channel names truncated")
	}
	channels := make([]string, chnCount)
	for i := 0; i < chnCount; i++ {
		channels[i] = trimASCII(p[o+i*16 : o+(i+1)*16])
	}
	o += chnCount * 16
	o += phnmr*4 + annmr*4
	if dgnmr > 0 {
		o += dgnmr * 4
	}
	if len(p) < o+6 {
		return Profile{}, fmt.Errorf("fnom/rate truncated")
	}
	fnomWord := binary.BigEndian.Uint16(p[o:])
	o += 2
	o += 2
	dataRate := int16(binary.BigEndian.Uint16(p[o:]))
	fnom := 60
	if fnomWord&1 == 1 {
		fnom = 50
	}

	return Profile{
		Station:   station,
		IDCode:    id,
		TimeBase:  timeBase,
		Format:    format,
		Phnmr:     phnmr,
		Annmr:     annmr,
		Dgnmr:     dgnmr,
		FnomHz:    fnom,
		DataRate:  dataRate,
		Channels:  channels,
		Polar:     format&0x0001 != 0,
		PhFloat:   format&0x0002 != 0,
		AnFloat:   format&0x0004 != 0,
		FreqFloat: format&0x0008 != 0,
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
