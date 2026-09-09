// dump-frames writes the PDC ring buffer (last N raw + parsed frames) to CSV.
//
// Usage:
//
//	go run ./cmd/dump-frames
//	go run ./cmd/dump-frames -count 1500 -raw-out data/last_1500_combined_raw.csv -parsed-out data/last_1500_combined_parsed.csv
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:2112", "PDC metrics/conversation base URL")
	count := flag.Int("count", 1500, "number of most recent frames to dump")
	pmu := flag.String("pmu", "", "optional PMU name filter")
	rawOut := flag.String("raw-out", filepath.Join("data", "last_1500_combined_raw.csv"), "raw frames CSV output path")
	parsedOut := flag.String("parsed-out", filepath.Join("data", "last_1500_combined_parsed.csv"), "parsed rows CSV output path")
	flag.Parse()

	u, err := url.Parse(strings.TrimRight(*addr, "/") + "/conversation/frame-capture/dump")
	if err != nil {
		fmt.Fprintf(os.Stderr, "url: %v\n", err)
		os.Exit(1)
	}
	q := u.Query()
	q.Set("count", fmt.Sprintf("%d", *count))
	q.Set("raw_out", *rawOut)
	q.Set("parsed_out", *parsedOut)
	if *pmu != "" {
		q.Set("pmu", *pmu)
	}
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "dump failed (%d): %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		os.Exit(1)
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		fmt.Fprintf(os.Stderr, "decode: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)

	if m, ok := out["buffered_by_pmu"].(map[string]any); ok && len(m) > 0 {
		fmt.Println("\nPer-PMU samples in capture:")
		total := 0
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			var n int
			switch v := m[name].(type) {
			case float64:
				n = int(v)
			case int:
				n = v
			default:
				continue
			}
			fmt.Printf("  %s: %d\n", name, n)
			total += n
		}
		fmt.Printf("  total: %d\n", total)
	}
}
