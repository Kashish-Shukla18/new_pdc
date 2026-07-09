// capture-pmu connects to a registered PMU, unpacks CFG2+DATA frames, and writes CSV.
//
// Usage:
//   go run ./cmd/capture-pmu -api http://127.0.0.1:8081 -duration 1m
//   go run ./cmd/capture-pmu -addr 172.24.105.87:4713 -name PMU.001 -duration 1m
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pdc/parser"
)

func cmdCRC(data []byte) uint16 {
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
	f[2], f[3] = 0, 18
	f[4] = byte(idcode >> 8)
	f[5] = byte(idcode)
	now := uint32(time.Now().Unix())
	f[6] = byte(now >> 24)
	f[7] = byte(now >> 16)
	f[8] = byte(now >> 8)
	f[9] = byte(now)
	f[14] = byte(cmd >> 8)
	f[15] = byte(cmd)
	crc := cmdCRC(f[:16])
	f[16] = byte(crc >> 8)
	f[17] = byte(crc)
	return f
}

func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := int(hdr[2])<<8 | int(hdr[3])
	if n < 4 || n > 65535 {
		return nil, fmt.Errorf("bad frame size %d", n)
	}
	rest := make([]byte, n-4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	return append(hdr, rest...), nil
}

type pmuConfig struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	IDCode  int    `json:"idcode"`
	Region  string `json:"region"`
}

func loadFromAPI(apiURL, wantName string) (addr string, name string, idcode uint16, err error) {
	resp, err := http.Get(strings.TrimRight(apiURL, "/") + "/api/pmus")
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("api status %d", resp.StatusCode)
	}
	var list []pmuConfig
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", "", 0, err
	}
	if len(list) == 0 {
		return "", "", 0, fmt.Errorf("no PMUs registered in API")
	}
	var pick *pmuConfig
	for i := range list {
		if wantName != "" && list[i].Name == wantName {
			pick = &list[i]
			break
		}
	}
	if pick == nil {
		pick = &list[0]
	}
	id := uint16(1)
	if pick.IDCode > 0 {
		id = uint16(pick.IDCode)
	}
	return fmt.Sprintf("%s:%d", pick.IP, pick.Port), pick.Name, id, nil
}

func f64(v float32) string { return strconv.FormatFloat(float64(v), 'f', 6, 64) }

func main() {
	apiURL := flag.String("api", "http://127.0.0.1:8081", "PDC API base URL (loads registered PMU)")
	addr := flag.String("addr", "", "PMU host:port (overrides -api)")
	name := flag.String("name", "", "PMU name filter when using -api")
	idcodeFlag := flag.Uint("idcode", 0, "command IDCODE (0 = from API)")
	duration := flag.Duration("duration", time.Minute, "capture duration")
	outPath := flag.String("out", "", "output CSV path")
	timeout := flag.Duration("timeout", 10*time.Second, "handshake timeout")
	flag.Parse()

	targetAddr := *addr
	pmuName := *name
	idcode := uint16(*idcodeFlag)

	if targetAddr == "" {
		var err error
		targetAddr, pmuName, idcode, err = loadFromAPI(*apiURL, *name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load PMU from API: %v\n", err)
			os.Exit(1)
		}
	}
	if pmuName == "" {
		pmuName = "PMU"
	}
	if idcode == 0 {
		idcode = 1
	}
	if *outPath == "" {
		safe := strings.ReplaceAll(pmuName, " ", "_")
		*outPath = filepath.Join("data", fmt.Sprintf("%s_%s.csv", safe, time.Now().Format("20060102_150405")))
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("capture %s (%s) for %s -> %s\n", pmuName, targetAddr, *duration, *outPath)
	fmt.Println("note: stop PDC / Connection Tester if the PMU allows only one TCP client")

	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create csv: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	var (
		profile       parser.Profile
		headerWritten bool
		nOK, nBad, sess int
		deadline      = time.Now().Add(*duration)
	)

	for time.Now().Before(deadline) {
		sess++
		conn, err := net.DialTimeout("tcp", targetAddr, *timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "session %d dial: %v\n", sess, err)
			time.Sleep(2 * time.Second)
			continue
		}
		fmt.Printf("session %d connected\n", sess)

		_ = conn.SetDeadline(time.Now().Add(*timeout))
		_, _ = conn.Write(buildCMD(idcode, 0x0005))
		cfgRaw, err := readFrame(conn)
		if err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "CFG2 read: %v\n", err)
			continue
		}
		profile, err = parser.ParseCFG2Frame(cfgRaw)
		if err != nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "CFG2 parse: %v\n", err)
			continue
		}
		parser.SetProfile(pmuName, profile)

		if !headerWritten {
			fmt.Printf("CFG2: station=%q rate=%d fnom=%dHz ph=%d an=%d\n",
				profile.Station, profile.DataRate, profile.FnomHz, profile.Phnmr, profile.Annmr)
			header := []string{
				"pmu_name", "timestamp_utc", "soc", "fracsec", "idcode", "stat_hex", "crc_ok",
				"frequency_hz", "rocof_hz_s", "mw", "mvar", "digital_hex", "frame_bytes",
				"va_mag", "va_angle_deg", "vb_mag", "vb_angle_deg", "vc_mag", "vc_angle_deg", "ia_mag", "ia_angle_deg",
			}
			_ = w.Write(header)
			headerWritten = true
		}

		_, _ = conn.Write(buildCMD(idcode, 0x0002))
		sessFrames := 0
		for time.Now().Before(deadline) {
			remain := time.Until(deadline)
			if remain > 2*time.Second {
				remain = 2 * time.Second
			}
			_ = conn.SetReadDeadline(time.Now().Add(remain))
			raw, err := readFrame(conn)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() && sessFrames > 0 {
					continue
				}
				if sessFrames > 0 {
					fmt.Printf("session %d ended after %d frames: %v\n", sess, sessFrames, err)
				}
				break
			}
			reading, err := parser.ParseDataFrame(pmuName, raw)
			if err != nil {
				nBad++
				continue
			}
			_ = w.Write([]string{
				pmuName,
				reading.Timestamp.Format(time.RFC3339Nano),
				strconv.FormatUint(uint64(reading.SOC), 10),
				strconv.FormatUint(uint64(reading.FracSecCount), 10),
				strconv.FormatUint(uint64(reading.IDCode), 10),
				fmt.Sprintf("0x%04X", reading.Stat),
				strconv.FormatBool(reading.ChecksumValid),
				f64(reading.Frequency),
				f64(reading.ROCOF),
				f64(reading.MW),
				f64(reading.MVAR),
				fmt.Sprintf("0x%04X", reading.Digital),
				strconv.Itoa(reading.FrameBytes),
				f64(reading.VA.Magnitude), f64(reading.VA.PhaseDegrees),
				f64(reading.VB.Magnitude), f64(reading.VB.PhaseDegrees),
				f64(reading.VC.Magnitude), f64(reading.VC.PhaseDegrees),
				f64(reading.IA.Magnitude), f64(reading.IA.PhaseDegrees),
			})
			nOK++
			sessFrames++
			if nOK == 1 || nOK%150 == 0 {
				fmt.Printf("… %d frames  f=%.4f Hz\n", nOK, reading.Frequency)
			}
		}
		_, _ = conn.Write(buildCMD(idcode, 0x0001))
		conn.Close()
		w.Flush()
		if time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	w.Flush()
	fmt.Printf("done: %d rows (%d bad) in %d sessions -> %s\n", nOK, nBad, sess, *outPath)
	if nOK == 0 {
		os.Exit(2)
	}
}
