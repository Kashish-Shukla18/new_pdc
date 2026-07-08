// capture-pmu connects to a C37.118 PMU, unpacks CFG2+DATA frames, and writes CSV.
// There is no link-layer encryption on standard IEEE C37.118 — "decrypt" here means
// binary unpack (CRC verify + CFG2-driven field decode).
//
// Usage:
//   go run ./cmd/capture-pmu -addr 172.24.105.87:4713 -duration 1m -out data/pmu001_1min.csv
package main

import (
	"encoding/binary"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

func buildCMD(idcode uint16, cmd uint16) []byte {
	f := make([]byte, 18)
	f[0], f[1] = 0xAA, 0x41
	binary.BigEndian.PutUint16(f[2:], 18)
	binary.BigEndian.PutUint16(f[4:], idcode)
	binary.BigEndian.PutUint32(f[6:], uint32(time.Now().Unix()))
	binary.BigEndian.PutUint32(f[10:], 0)
	binary.BigEndian.PutUint16(f[14:], cmd)
	binary.BigEndian.PutUint16(f[16:], crc16(f[:16]))
	return f
}

func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[2:]))
	if n < 4 || n > 65535 {
		return nil, fmt.Errorf("bad frame size %d (hdr=%x)", n, hdr)
	}
	rest := make([]byte, n-4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	return append(hdr, rest...), nil
}

func trimASCII(b []byte) string {
	s := string(b)
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

type cfg2Info struct {
	station   string
	idcode    uint16
	timeBase  uint32
	format    uint16
	phnmr     int
	annmr     int
	dgnmr     int
	fnom      int
	dataRate  int16
	channels  []string
	phasorMag []bool // true => polar (mag,angle); false => rect (real,imag) — from FORMAT bit0
	phFloat   bool
	anFloat   bool
	freqFloat bool
}

func parseCFG2(raw []byte) (*cfg2Info, error) {
	if len(raw) < 40 || raw[0] != 0xAA || (raw[1]&0x70) != 0x30 {
		return nil, fmt.Errorf("not a CFG2 frame")
	}
	p := raw[14 : len(raw)-2]
	if len(p) < 34 {
		return nil, fmt.Errorf("CFG2 payload too short")
	}
	o := 0
	timeBase := binary.BigEndian.Uint32(p[o:])
	o += 4
	numPMU := binary.BigEndian.Uint16(p[o:])
	o += 2
	if numPMU < 1 {
		return nil, fmt.Errorf("NUM_PMU=%d", numPMU)
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
		return nil, fmt.Errorf("channel names truncated")
	}
	channels := make([]string, chnCount)
	for i := 0; i < chnCount; i++ {
		channels[i] = trimASCII(p[o+i*16 : o+(i+1)*16])
	}
	o += chnCount * 16
	o += phnmr*4 + annmr*4 // units
	if dgnmr > 0 {
		o += dgnmr * 4
	}
	if len(p) < o+6 {
		return nil, fmt.Errorf("fnom/rate truncated")
	}
	fnomWord := binary.BigEndian.Uint16(p[o:])
	o += 2
	o += 2 // cfgcnt
	dataRate := int16(binary.BigEndian.Uint16(p[o:]))
	fnom := 60
	if fnomWord&1 == 1 {
		fnom = 50
	}

	return &cfg2Info{
		station:   station,
		idcode:    id,
		timeBase:  timeBase,
		format:    format,
		phnmr:     phnmr,
		annmr:     annmr,
		dgnmr:     dgnmr,
		fnom:      fnom,
		dataRate:  dataRate,
		channels:  channels,
		phasorMag: nil,
		phFloat:   format&0x0002 != 0,
		anFloat:   format&0x0004 != 0,
		freqFloat: format&0x0008 != 0,
	}, nil
}

func (c *cfg2Info) polar() bool {
	// FORMAT bit 0: 0=rectangular, 1=polar
	return c.format&0x0001 != 0
}

func (c *cfg2Info) phasorNames() []string {
	out := make([]string, c.phnmr)
	copy(out, c.channels[:c.phnmr])
	return out
}

func (c *cfg2Info) analogNames() []string {
	out := make([]string, c.annmr)
	copy(out, c.channels[c.phnmr:c.phnmr+c.annmr])
	return out
}

type row struct {
	isoTime   string
	soc       uint32
	frac      uint32
	idcode    uint16
	stat      uint16
	crcOK     bool
	freq      float64
	rocof     float64
	phasors   [][2]float64 // mag/angle or real/imag
	analogs   []float64
	digital   uint16
	rawSize   int
}

func unpackData(cfg *cfg2Info, raw []byte) (*row, error) {
	if len(raw) < 16 || raw[0] != 0xAA || (raw[1]&0x70) != 0x00 {
		return nil, fmt.Errorf("not a data frame (type=0x%02X size=%d)", raw[1]&0x70, len(raw))
	}
	crcRx := binary.BigEndian.Uint16(raw[len(raw)-2:])
	crcOK := crcRx == crc16(raw[:len(raw)-2])

	id := binary.BigEndian.Uint16(raw[4:6])
	soc := binary.BigEndian.Uint32(raw[6:10])
	frac := binary.BigEndian.Uint32(raw[10:14])
	tq := frac >> 24
	fracCount := frac & 0x00FFFFFF
	tb := cfg.timeBase
	if tb == 0 {
		tb = 1_000_000
	}
	nanos := int64(fracCount) * int64(time.Second) / int64(tb)
	ts := time.Unix(int64(soc), nanos).UTC()

	p := raw[14 : len(raw)-2]
	o := 0
	if len(p) < 2 {
		return nil, fmt.Errorf("empty payload")
	}
	stat := binary.BigEndian.Uint16(p[o:])
	o += 2

	readF32 := func() (float64, error) {
		if o+4 > len(p) {
			return 0, fmt.Errorf("short float at offset %d", o)
		}
		v := math.Float32frombits(binary.BigEndian.Uint32(p[o : o+4]))
		o += 4
		return float64(v), nil
	}
	readI16 := func() (float64, error) {
		if o+2 > len(p) {
			return 0, fmt.Errorf("short int16 at offset %d", o)
		}
		v := int16(binary.BigEndian.Uint16(p[o : o+2]))
		o += 2
		return float64(v), nil
	}

	phasors := make([][2]float64, cfg.phnmr)
	for i := 0; i < cfg.phnmr; i++ {
		var a, b float64
		var err error
		if cfg.phFloat {
			a, err = readF32()
			if err != nil {
				return nil, err
			}
			b, err = readF32()
			if err != nil {
				return nil, err
			}
		} else {
			a, err = readI16()
			if err != nil {
				return nil, err
			}
			b, err = readI16()
			if err != nil {
				return nil, err
			}
		}
		if cfg.polar() {
			// polar float: magnitude, angle(radians per IEEE)
			phasors[i] = [2]float64{a, b * 180.0 / math.Pi}
		} else {
			mag := math.Hypot(a, b)
			ang := math.Atan2(b, a) * 180.0 / math.Pi
			phasors[i] = [2]float64{mag, ang}
		}
	}

	var freq, rocof float64
	var err error
	if cfg.freqFloat {
		freq, err = readF32()
		if err != nil {
			return nil, err
		}
		rocof, err = readF32()
		if err != nil {
			return nil, err
		}
	} else {
		freq, err = readI16()
		if err != nil {
			return nil, err
		}
		rocof, err = readI16()
		if err != nil {
			return nil, err
		}
		freq = float64(cfg.fnom) + freq/1000.0
		rocof = rocof / 100.0
	}

	analogs := make([]float64, cfg.annmr)
	for i := 0; i < cfg.annmr; i++ {
		if cfg.anFloat {
			analogs[i], err = readF32()
		} else {
			analogs[i], err = readI16()
		}
		if err != nil {
			return nil, err
		}
	}

	var dig uint16
	if cfg.dgnmr > 0 {
		if o+2 > len(p) {
			return nil, fmt.Errorf("short digital")
		}
		dig = binary.BigEndian.Uint16(p[o:])
		o += 2
	}

	_ = tq
	return &row{
		isoTime: ts.Format(time.RFC3339Nano),
		soc:     soc,
		frac:    fracCount,
		idcode:  id,
		stat:    stat,
		crcOK:   crcOK,
		freq:    freq,
		rocof:   rocof,
		phasors: phasors,
		analogs: analogs,
		digital: dig,
		rawSize: len(raw),
	}, nil
}

func f64(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

func main() {
	addr := flag.String("addr", "172.24.105.87:4713", "PMU host:port")
	idcode := flag.Uint("idcode", 1, "command IDCODE")
	duration := flag.Duration("duration", time.Minute, "capture duration")
	outPath := flag.String("out", "", "output CSV path (default data/pmu_<ts>.csv)")
	timeout := flag.Duration("timeout", 10*time.Second, "dial/read timeout during handshake")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Join("data", fmt.Sprintf("pmu_capture_%s.csv", time.Now().Format("20060102_150405")))
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("target %s for %s -> %s\n", *addr, *duration, *outPath)

	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create csv: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	var (
		cfg            *cfg2Info
		headerWritten  bool
		nOK, nBad, sess int
		deadline       = time.Now().Add(*duration)
	)

	for time.Now().Before(deadline) {
		sess++
		conn, err := net.DialTimeout("tcp", *addr, *timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session %d dial failed: %v — retry in 2s\n", sess, err)
			time.Sleep(2 * time.Second)
			continue
		}
		fmt.Printf("session %d connected local=%s\n", sess, conn.LocalAddr())

		_ = conn.SetDeadline(time.Now().Add(*timeout))
		if _, err := conn.Write(buildCMD(uint16(*idcode), 0x0005)); err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "CFG2 cmd: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		cfgRaw, err := readFrame(conn)
		if err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "CFG2 read: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		parsed, err := parseCFG2(cfgRaw)
		if err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "CFG2 parse: %v\n", err)
			time.Sleep(time.Second)
			continue
		}
		cfg = parsed
		if !headerWritten {
			fmt.Printf("CFG2 unpacked: station=%q id=%d rate=%d fnom=%dHz format=0x%04X polar=%v ph=%d an=%d dg=%d\n",
				cfg.station, cfg.idcode, cfg.dataRate, cfg.fnom, cfg.format, cfg.polar(), cfg.phnmr, cfg.annmr, cfg.dgnmr)
			header := []string{"timestamp_utc", "soc", "fracsec", "idcode", "stat_hex", "crc_ok", "frequency_hz", "rocof_hz_s", "frame_bytes"}
			for _, name := range cfg.phasorNames() {
				header = append(header, name+"_mag", name+"_angle_deg")
			}
			for _, name := range cfg.analogNames() {
				header = append(header, name)
			}
			if cfg.dgnmr > 0 {
				header = append(header, "digital_hex")
			}
			_ = w.Write(header)
			headerWritten = true
		}

		_ = conn.SetDeadline(time.Now().Add(*timeout))
		if _, err := conn.Write(buildCMD(uint16(*idcode), 0x0002)); err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "DATA_ON: %v\n", err)
			time.Sleep(time.Second)
			continue
		}

		sessFrames := 0
		for time.Now().Before(deadline) {
			remain := time.Until(deadline)
			if remain > 2*time.Second {
				remain = 2 * time.Second
			}
			_ = conn.SetReadDeadline(time.Now().Add(remain))
			raw, err := readFrame(conn)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					if sessFrames == 0 {
						continue
					}
					// idle after data started — keep waiting until overall deadline
					continue
				}
				fmt.Printf("session %d ended after %d frames: %v\n", sess, sessFrames, err)
				break
			}
			r, err := unpackData(cfg, raw)
			if err != nil {
				nBad++
				continue
			}
			rec := []string{
				r.isoTime,
				strconv.FormatUint(uint64(r.soc), 10),
				strconv.FormatUint(uint64(r.frac), 10),
				strconv.FormatUint(uint64(r.idcode), 10),
				fmt.Sprintf("0x%04X", r.stat),
				strconv.FormatBool(r.crcOK),
				f64(r.freq),
				f64(r.rocof),
				strconv.Itoa(r.rawSize),
			}
			for _, ph := range r.phasors {
				rec = append(rec, f64(ph[0]), f64(ph[1]))
			}
			for _, a := range r.analogs {
				rec = append(rec, f64(a))
			}
			if cfg.dgnmr > 0 {
				rec = append(rec, fmt.Sprintf("0x%04X", r.digital))
			}
			_ = w.Write(rec)
			nOK++
			sessFrames++
			if nOK == 1 || nOK%150 == 0 {
				fmt.Printf("… %d frames  last f=%.4f Hz\n", nOK, r.freq)
			}
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = conn.Write(buildCMD(uint16(*idcode), 0x0001))
		conn.Close()
		w.Flush()
		if time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	w.Flush()
	fmt.Printf("done: wrote %d rows (%d bad) across %d sessions -> %s\n", nOK, nBad, sess, *outPath)
	if nOK == 0 {
		os.Exit(2)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
