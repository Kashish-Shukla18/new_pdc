// Command frame-diag: sample per-PMU pipeline funnel for a few seconds and print where frames are lost.
//
// Usage (PDC must already be running):
//
//	go run ./cmd/frame-diag -pmu pmu.001 -duration 4s -addr http://127.0.0.1:2112
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type snap struct {
	PMU          string `json:"pmu"`
	Since        time.Time `json:"since"`
	DataRate     int16  `json:"dataRate"`
	TCPComplete  int64  `json:"tcpComplete"`
	CRCFail      int64  `json:"crcFail"`
	HandlerDrop  int64  `json:"handlerDrop"`
	ParseOK      int64  `json:"parseOK"`
	ParseFail    int64  `json:"parseFail"`
	QualityOK    int64  `json:"qualityOK"`
	QualityFlag  int64  `json:"qualityFlag"`
	QualityDrop  int64  `json:"qualityDrop"`
	Dashboard    int64  `json:"dashboard"`
	KafkaOK      int64  `json:"kafkaOK"`
	KafkaFail    int64  `json:"kafkaFail"`
	Losses       []struct {
		Stage        string    `json:"stage"`
		SOC          uint32    `json:"soc"`
		FracSecCount uint32    `json:"fracSecCount"`
		Reason       string    `json:"reason"`
		At           time.Time `json:"at"`
	} `json:"losses"`
}

func fetch(base, pmu string, reset bool) (snap, error) {
	u, err := url.Parse(base + "/conversation/frame-diag")
	if err != nil {
		return snap{}, err
	}
	q := u.Query()
	q.Set("pmu", pmu)
	if reset {
		q.Set("reset", "1")
	}
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return snap{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return snap{}, fmt.Errorf("HTTP %s", resp.Status)
	}
	var s snap
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return snap{}, err
	}
	return s, nil
}

func main() {
	pmu := flag.String("pmu", "pmu.001", "PMU name to diagnose")
	addr := flag.String("addr", "http://127.0.0.1:2112", "PDC metrics/conversation base URL")
	dur := flag.Duration("duration", 4*time.Second, "sample window")
	flag.Parse()

	if _, err := fetch(*addr, *pmu, true); err != nil {
		fmt.Fprintf(os.Stderr, "reset failed: %v\n(is PDC running on %s?)\n", err, *addr)
		os.Exit(1)
	}
	fmt.Printf("Sampling %s for %s …\n\n", *pmu, *dur)
	time.Sleep(*dur)

	s, err := fetch(*addr, *pmu, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot failed: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(s.Since).Seconds()
	expected := int64(0)
	if s.DataRate > 0 && elapsed > 0 {
		expected = int64(float64(s.DataRate) * elapsed)
	} else if s.DataRate < 0 && elapsed > 0 {
		expected = int64(elapsed / float64(-s.DataRate))
	}

	// Funnel: each stage Received = entered; Lost = did not advance.
	tcpRecv := s.TCPComplete + s.CRCFail
	tcpLost := s.CRCFail // incomplete/reconnect gaps are outside this counter
	crcRecv := s.TCPComplete + s.CRCFail
	crcLost := s.CRCFail
	crcPass := s.TCPComplete

	handlerRecv := crcPass
	handlerLost := s.HandlerDrop
	handlerPass := crcPass - s.HandlerDrop
	if handlerPass < 0 {
		handlerPass = 0
	}

	parseRecv := s.ParseOK + s.ParseFail
	parseLost := s.ParseFail
	parsePass := s.ParseOK

	qualRecv := s.QualityOK + s.QualityFlag // every parsed frame is validated
	qualLost := s.QualityDrop
	qualPass := qualRecv - qualLost
	if qualPass < 0 {
		qualPass = 0
	}

	dashRecv := s.Dashboard
	dashLost := int64(0)
	if parsePass-s.QualityDrop > dashRecv {
		dashLost = parsePass - s.QualityDrop - dashRecv
	}

	fmt.Printf("PMU: %s   window: %.1fs   CFG data_rate: %d\n", s.PMU, elapsed, s.DataRate)
	if expected > 0 {
		fmt.Printf("Expected from PMU: %d frames\n", expected)
	}
	fmt.Println()
	fmt.Printf("%-14s %10s %10s\n", "Stage", "Received", "Lost")
	fmt.Println("-----------------------------------")
	fmt.Printf("%-14s %10d %10d\n", "TCP", tcpRecv, tcpLost)
	fmt.Printf("%-14s %10d %10d\n", "CRC", crcRecv, crcLost)
	fmt.Printf("%-14s %10d %10d\n", "Handler", handlerRecv, handlerLost)
	fmt.Printf("%-14s %10d %10d\n", "Parse", parseRecv, parseLost)
	fmt.Printf("%-14s %10d %10d\n", "Quality", qualRecv, qualLost)
	fmt.Printf("%-14s %10d %10d\n", "Dashboard", dashRecv, dashLost)
	fmt.Printf("%-14s %10d %10d\n", "Kafka", s.KafkaOK+s.KafkaFail, s.KafkaFail)
	fmt.Println()

	if s.QualityFlag > 0 && s.QualityDrop == 0 {
		fmt.Printf("Note: quality flagged %d frame(s) but DROP_QUALITY_REJECTED=false — they still reached the next stage.\n\n", s.QualityFlag)
	}

	type cand struct {
		name string
		n    int64
	}
	cands := []cand{
		{"TCP/CRC", s.CRCFail},
		{"Handler pool", s.HandlerDrop},
		{"Parse", s.ParseFail},
		{"Quality check", s.QualityDrop},
		{"Dashboard gap", dashLost},
		{"Kafka", s.KafkaFail},
	}
	main := cand{"(none — no counted losses)", 0}
	for _, c := range cands {
		if c.n > main.n {
			main = c
		}
	}

	fmt.Println("Conclusion:")
	if main.n == 0 {
		gap := int64(0)
		if expected > tcpRecv {
			gap = expected - tcpRecv
		}
		if gap > 0 {
			fmt.Printf("  No in-process stage drops. %d fewer frames than CFG DATA_RATE expect (%d fps advertised, ~%.0f fps arrived).\n  That gap is before/at the PMU wire — not Quality/Parse/Dashboard loss. Inventory should use live rate, not inflated CFG.\n", gap, s.DataRate, float64(tcpRecv)/elapsed)
		} else {
			fmt.Println("  No frame loss counted inside the PDC pipeline for this window.")
		}
	} else {
		fmt.Printf("  MAIN LOSS: %s\n  Lost: %d frames\n  Investigate this stage.\n", main.name, main.n)
	}

	if len(s.Losses) > 0 {
		fmt.Println("\nRecent lost/flagged frames (SOC / frac_count / stage):")
		for _, l := range s.Losses {
			fmt.Printf("  %s  soc=%d frac=%d  [%s] %s\n", l.At.Format(time.RFC3339Nano), l.SOC, l.FracSecCount, l.Stage, l.Reason)
		}
	}
}
