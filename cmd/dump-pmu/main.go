// dump-pmu connects to a PMU, prints full CFG-2 profile and one decoded DATA reading.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"pdc/parser"
)

func crc(data []byte) uint16 {
	c := uint16(0xFFFF)
	for _, b := range data {
		c ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if c&0x8000 != 0 {
				c = (c << 1) ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func cmd(id, word uint16) []byte {
	f := make([]byte, 18)
	f[0], f[1] = 0xAA, 0x42
	binary.BigEndian.PutUint16(f[2:], 18)
	binary.BigEndian.PutUint16(f[4:], id)
	binary.BigEndian.PutUint32(f[6:], uint32(time.Now().Unix()))
	binary.BigEndian.PutUint16(f[14:], word)
	binary.BigEndian.PutUint16(f[16:], crc(f[:16]))
	return f
}

func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[2:4]))
	if n < 16 {
		return nil, fmt.Errorf("bad size %d", n)
	}
	rest := make([]byte, n-4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	raw := append(hdr, rest...)
	want := binary.BigEndian.Uint16(raw[n-2:])
	got := crc(raw[:n-2])
	if want != got {
		return nil, fmt.Errorf("CRC mismatch want=0x%04X got=0x%04X", want, got)
	}
	return raw, nil
}

func readUntilType(conn net.Conn, want byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return nil, fmt.Errorf("timeout waiting for type 0x%02X", want)
		}
		_ = conn.SetReadDeadline(time.Now().Add(remain))
		raw, err := readFrame(conn)
		if err != nil {
			return nil, err
		}
		if raw[1]&0x70 == want {
			return raw, nil
		}
	}
}

func main() {
	addr := flag.String("addr", "172.24.105.87:4713", "PMU host:port")
	idcode := flag.Uint("idcode", 1, "command IDCODE")
	out := flag.String("out", "data/device_frame_dump.json", "output JSON path")
	flag.Parse()

	conn, err := net.DialTimeout("tcp", *addr, 8*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Printf("connected to %s\n", conn.RemoteAddr())

	id := uint16(*idcode)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = conn.Write(cmd(id, 0x0001)) // DATA_OFF
	time.Sleep(200 * time.Millisecond)

	_, _ = conn.Write(cmd(id, 0x0005)) // CFG2
	cfgRaw, err := readUntilType(conn, 0x30, 8*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CFG2: %v\n", err)
		os.Exit(1)
	}
	prof, err := parser.ParseCFG2Frame(cfgRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse CFG2: %v\n", err)
		os.Exit(1)
	}
	parser.SetProfile("dump", prof)

	_, _ = conn.Write(cmd(id, 0x0002)) // DATA_ON
	dataRaw, err := readUntilType(conn, 0x00, 8*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DATA: %v\n", err)
		os.Exit(1)
	}
	reading, err := parser.ParseDataFrame("dump", dataRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse DATA: %v\n", err)
		os.Exit(1)
	}
	_, _ = conn.Write(cmd(id, 0x0001)) // DATA_OFF

	type phUnitOut struct {
		Index     int     `json:"index"`
		IsCurrent bool    `json:"is_current"`
		Factor    float64 `json:"factor"`
	}
	type anUnitOut struct {
		Index  int     `json:"index"`
		Type   uint8   `json:"type"`
		Factor float64 `json:"factor"`
	}
	type digUnitOut struct {
		Index      int    `json:"index"`
		NormalMask string `json:"normal_mask"`
		ValidMask  string `json:"valid_mask"`
	}

	phOut := make([]phUnitOut, len(prof.PhUnits))
	for i, u := range prof.PhUnits {
		phOut[i] = phUnitOut{Index: i, IsCurrent: u.IsCurrent, Factor: u.Factor}
	}
	anOut := make([]anUnitOut, len(prof.AnUnits))
	for i, u := range prof.AnUnits {
		anOut[i] = anUnitOut{Index: i, Type: u.Type, Factor: u.Factor}
	}
	dgOut := make([]digUnitOut, len(prof.DigUnits))
	for i, u := range prof.DigUnits {
		dgOut[i] = digUnitOut{
			Index: i, NormalMask: fmt.Sprintf("0x%04X", u.NormalMask), ValidMask: fmt.Sprintf("0x%04X", u.ValidMask),
		}
	}

	doc := map[string]any{
		"source": map[string]any{
			"addr":          *addr,
			"idcode_cmd":    id,
			"cfg2_bytes":    len(cfgRaw),
			"data_bytes":    len(dataRaw),
			"captured_at":   time.Now().UTC().Format(time.RFC3339),
		},
		"configuration_frame": map[string]any{
			"station":                   prof.Station,
			"idcode":                    prof.IDCode,
			"time_base":                 prof.TimeBase,
			"num_pmu":                   prof.NumPMU,
			"format_raw":                fmt.Sprintf("0x%04X", prof.Format),
			"polar":                     prof.Polar,
			"phasor_float":              prof.PhFloat,
			"analog_float":              prof.AnFloat,
			"freq_float":                prof.FreqFloat,
			"phnmr":                     prof.Phnmr,
			"annmr":                     prof.Annmr,
			"dgnmr":                     prof.Dgnmr,
			"fnom_hz":                   prof.FnomHz,
			"fnom_word":                 fmt.Sprintf("0x%04X", prof.FnomWord),
			"cfgcnt":                    prof.CfgCnt,
			"data_rate":                 prof.DataRate,
			"channels":                  prof.Channels,
			"ph_units":                  phOut,
			"an_units":                  anOut,
			"dig_units":                 dgOut,
			"data_payload_bytes":        prof.DataPayloadBytes,
			"total_data_payload_bytes":  prof.TotalDataPayloadBytes,
			"header_text":               prof.HeaderText,
		},
		"data_frame": reading,
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *out)
	fmt.Println(string(b))
}
