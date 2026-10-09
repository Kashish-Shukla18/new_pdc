package monitoring

// aligned_sheets.go — spreadsheet view of samples AFTER time-alignment.
//
// These are the same ticks already stored for charts (alignedBatches).
// Plotting is untouched: we only snapshot the ring and render HTML/Excel.
//
// Open:  /conversation/aligned-samples/sheets
// Excel: /conversation/aligned-samples/sheets.xls

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fixedAlignedHeaders are always present; CFG analog names are appended after.
var fixedAlignedHeaders = []string{
	"tick_ms", "time_utc", "pmu_name",
	"complete", "reason", "missing",
	"frequency_hz", "frequency_dev_hz", "rocof_hz_per_sec",
	"va_mag", "vb_mag", "vc_mag",
	"ia_mag", "ib_mag", "ic_mag",
	"va_angle_deg", "vb_angle_deg", "vc_angle_deg",
	"ia_angle_deg", "ib_angle_deg", "ic_angle_deg",
}

// SnapshotAlignedBatches copies the ring used by charts (read-only, no emit change).
func SnapshotAlignedBatches() []AlignedBatch {
	conversationBus.mu.Lock()
	defer conversationBus.mu.Unlock()
	out := make([]AlignedBatch, len(conversationBus.alignedBatches))
	for i, b := range conversationBus.alignedBatches {
		pts := make(map[string]AlignedPoint, len(b.Points))
		for name, p := range b.Points {
			if len(p.Analogs) > 0 {
				an := make(map[string]float64, len(p.Analogs))
				for k, v := range p.Analogs {
					an[k] = v
				}
				p.Analogs = an
			}
			pts[name] = p
		}
		out[i] = AlignedBatch{
			TS:       b.TS,
			Points:   pts,
			Missing:  append([]string(nil), b.Missing...),
			Complete: b.Complete,
			Reason:   b.Reason,
		}
	}
	return out
}

func alignedAnalogNames(batches []AlignedBatch) []string {
	seen := map[string]struct{}{}
	for _, b := range batches {
		for _, p := range b.Points {
			for name := range p.Analogs {
				if name != "" {
					seen[name] = struct{}{}
				}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func alignedHeaders(analogNames []string) []string {
	h := append([]string{}, fixedAlignedHeaders...)
	return append(h, analogNames...)
}

func f64(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// alignedRows: one spreadsheet row per (tick, PMU present). Same values charts plot.
func alignedRows(batches []AlignedBatch, analogNames []string) [][]string {
	rows := make([][]string, 0)
	for _, b := range batches {
		names := make([]string, 0, len(b.Points))
		for name := range b.Points {
			names = append(names, name)
		}
		sort.Strings(names)
		timeUTC := time.UnixMilli(b.TS).UTC().Format(time.RFC3339Nano)
		miss := strings.Join(b.Missing, " ")
		for _, name := range names {
			p := b.Points[name]
			row := []string{
				strconv.FormatInt(b.TS, 10),
				timeUTC,
				name,
				strconv.FormatBool(b.Complete),
				b.Reason,
				miss,
				f64(p.Frequency),
				f64(p.FrequencyDev),
				f64(p.ROCOF),
				f64(p.VA), f64(p.VB), f64(p.VC),
				f64(p.IA), f64(p.IB), f64(p.IC),
				f64(p.VAAngle), f64(p.VBAngle), f64(p.VCAngle),
				f64(p.IAAngle), f64(p.IBAngle), f64(p.ICAngle),
			}
			for _, an := range analogNames {
				if p.Analogs != nil {
					row = append(row, f64(p.Analogs[an]))
				} else {
					row = append(row, "")
				}
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// WriteAlignedSheetsHTML renders the aligned chart samples as one HTML table.
func WriteAlignedSheetsHTML(w io.Writer, batches []AlignedBatch) error {
	analogs := alignedAnalogNames(batches)
	headers := alignedHeaders(analogs)
	rows := alignedRows(batches, analogs)

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Aligned samples</title>
<style>
body{font-family:Segoe UI,sans-serif;margin:16px;background:#f6f7f9;color:#1c2430}
h1{font-size:18px;margin:0 0 4px} p{color:#5c6b7a;font-size:13px}
a{color:#1d4f91}
section{background:#fff;border:1px solid #d7dee7;border-radius:8px;margin:16px 0;padding:12px}
.wrap{overflow:auto;max-height:75vh}
table{border-collapse:collapse;font-size:12px;white-space:nowrap}
th,td{border:1px solid #e3e8ee;padding:4px 8px;text-align:right}
th{position:sticky;top:0;background:#eef3f8;text-align:left}
td:nth-child(-n+3),th:nth-child(-n+3){text-align:left}
</style></head><body>`)
	fmt.Fprintf(&b, `<h1>Aligned samples (post-align, pre-plot)</h1>
<p>%d ticks in ring · same data charts use · plotting unchanged ·
<a href="/conversation/aligned-samples/sheets.xls">Download Excel</a> ·
<a href="/conversation/aligned-samples">JSON</a> ·
<a href="/conversation/aligner-buffers/sheets">Pre-align buffers</a></p>
<section><div class="wrap"><table><thead><tr>`, len(batches))
	for _, h := range headers {
		fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(h))
	}
	b.WriteString("</tr></thead><tbody>")
	if len(rows) == 0 {
		b.WriteString(`<tr><td colspan="99">No aligned ticks yet — wait for the publisher to emit.</td></tr>`)
	}
	for _, row := range rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(&b, "<td>%s</td>", html.EscapeString(cell))
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></div></section></body></html>")
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteAlignedSheetsExcel writes one worksheet Excel can open.
func WriteAlignedSheetsExcel(w io.Writer, batches []AlignedBatch) error {
	analogs := alignedAnalogNames(batches)
	headers := alignedHeaders(analogs)
	rows := alignedRows(batches, analogs)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<?mso-application progid="Excel.Sheet"?>` + "\n")
	b.WriteString(`<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">` + "\n")
	b.WriteString(`<Worksheet ss:Name="aligned"><Table>`)
	b.WriteString("<Row>")
	for _, h := range headers {
		fmt.Fprintf(&b, `<Cell><Data ss:Type="String">%s</Data></Cell>`, html.EscapeString(h))
	}
	b.WriteString("</Row>")
	for _, row := range rows {
		b.WriteString("<Row>")
		for _, cell := range row {
			fmt.Fprintf(&b, `<Cell><Data ss:Type="String">%s</Data></Cell>`, html.EscapeString(cell))
		}
		b.WriteString("</Row>")
	}
	b.WriteString("</Table></Worksheet>\n</Workbook>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func registerAlignedSampleHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/conversation/aligned-samples/sheets.xls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.ms-excel")
		w.Header().Set("Content-Disposition", `attachment; filename="aligned-samples.xls"`)
		_ = WriteAlignedSheetsExcel(w, SnapshotAlignedBatches())
	})
	mux.HandleFunc("/conversation/aligned-samples/sheets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = WriteAlignedSheetsHTML(w, SnapshotAlignedBatches())
	})
	mux.HandleFunc("/conversation/aligned-samples", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"note":    "post-align chart ticks; plotting unchanged",
			"batches": SnapshotAlignedBatches(),
		})
	})
}
