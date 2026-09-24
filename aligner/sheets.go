package aligner

// sheets.go — turn buffer snapshots into HTML / Excel so humans can inspect them.
// Open /conversation/aligner-buffers/sheets in a browser.

import (
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"pdc/parser"
)

// fixedSheetHeaders are the same columns on every PMU sheet.
// Extra CFG channel names are appended after these.
var fixedSheetHeaders = []string{
	"ts_ms", "time_utc", "pmu_name", "idcode",
	"soc", "fracsec_raw", "fracsec_count", "timestamp",
	"received_at_ns", "received_at_utc",
	"checksum", "checksum_valid", "frame_size", "frame_bytes",
	"stat_raw", "data_error_code", "data_error", "cfg_change", "trigger",
	"sort_by_arrival", "sync_unlocked", "pmu_time_quality", "unlocked_duration", "trigger_reason",
	"msg_tq_raw", "leap_second_direction", "leap_second_occurred", "leap_second_pending", "time_quality_code",
	"frequency_hz", "frequency_deviation_hz", "rocof_hz_per_sec",
	"va_real", "va_imag", "va_mag", "va_angle_deg",
	"vb_real", "vb_imag", "vb_mag", "vb_angle_deg",
	"vc_real", "vc_imag", "vc_mag", "vc_angle_deg",
	"ia_real", "ia_imag", "ia_mag", "ia_angle_deg",
	"voltage_imbalance_percent", "vab_phase_diff_deg", "vbc_phase_diff_deg", "vca_phase_diff_deg",
	"sequence_positive_rms", "sequence_negative_rms", "sequence_zero_rms",
	"mw", "mvar", "mva", "power_factor", "power_factor_direction",
	"total_power_real_w", "total_power_imag_var",
	"digital", "digitals",
}

// sheetColumns is the header row for one snapshot (fixed fields + any extra channels).
func sheetColumns(snap BankSnapshot) (phasorNames, analogNames, headers []string) {
	phSeen := map[string]struct{}{}
	anSeen := map[string]struct{}{}
	for _, pmu := range snap.PMUs {
		for _, slot := range pmu.Slots {
			for _, ph := range slot.Reading.Phasors {
				if ph.Name != "" {
					phSeen[ph.Name] = struct{}{}
				}
			}
			for _, an := range slot.Reading.Analogs {
				if an.Name != "" {
					anSeen[an.Name] = struct{}{}
				}
			}
		}
	}
	phasorNames = sortedKeys(phSeen)
	analogNames = sortedKeys(anSeen)
	headers = append([]string{}, fixedSheetHeaders...)
	for _, name := range phasorNames {
		headers = append(headers, name+"_mag", name+"_angle_deg")
	}
	for _, name := range analogNames {
		headers = append(headers, name)
	}
	return phasorNames, analogNames, headers
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sheetRow(ts int64, timeUTC string, r parser.Reading, phasorNames, analogNames []string) []string {
	phByName := map[string]parser.Phasor{}
	for _, ph := range r.Phasors {
		phByName[ph.Name] = ph.Phasor
	}
	anByName := map[string]float32{}
	for _, an := range r.Analogs {
		anByName[an.Name] = an.Value
	}
	digs := make([]string, len(r.Digitals))
	for i, d := range r.Digitals {
		digs[i] = strconv.FormatUint(uint64(d), 10)
	}

	row := []string{
		strconv.FormatInt(ts, 10),
		timeUTC,
		r.PMUName,
		strconv.FormatUint(uint64(r.IDCode), 10),
		strconv.FormatUint(uint64(r.SOC), 10),
		strconv.FormatUint(uint64(r.FracSecRaw), 10),
		strconv.FormatUint(uint64(r.FracSecCount), 10),
		r.Timestamp.UTC().Format(time.RFC3339Nano),
		receivedAtNS(r),
		receivedAtUTC(r),
		strconv.FormatUint(uint64(r.Checksum), 10),
		strconv.FormatBool(r.ChecksumValid),
		strconv.Itoa(r.FrameSize),
		strconv.Itoa(r.FrameBytes),
		strconv.FormatUint(uint64(r.Stat), 10),
		strconv.FormatUint(uint64(r.StatDetail.DataErrorCode), 10),
		strconv.FormatBool(r.StatDetail.DataError),
		strconv.FormatBool(r.StatDetail.CFGChange),
		strconv.FormatBool(r.StatDetail.TriggerDetected),
		strconv.FormatBool(r.StatDetail.SortMethod),
		strconv.FormatBool(r.StatDetail.PMUSyncStatus),
		strconv.FormatUint(uint64(r.StatDetail.PMUTimeQuality), 10),
		strconv.FormatUint(uint64(r.StatDetail.UnlockedDuration), 10),
		strconv.FormatUint(uint64(r.StatDetail.PMUTriggerReason), 10),
		strconv.FormatUint(uint64(r.MsgTQ.Raw), 10),
		strconv.FormatBool(r.MsgTQ.LeapSecondDirection),
		strconv.FormatBool(r.MsgTQ.LeapSecondOccurred),
		strconv.FormatBool(r.MsgTQ.LeapSecondPending),
		strconv.FormatUint(uint64(r.MsgTQ.TimeQualityCode), 10),
		f32(r.Frequency),
		f32(r.FrequencyDeviation),
		f32(r.ROCOF),
		f32(r.VA.Real), f32(r.VA.Imag), f32(r.VA.Magnitude), f32(r.VA.PhaseDegrees),
		f32(r.VB.Real), f32(r.VB.Imag), f32(r.VB.Magnitude), f32(r.VB.PhaseDegrees),
		f32(r.VC.Real), f32(r.VC.Imag), f32(r.VC.Magnitude), f32(r.VC.PhaseDegrees),
		f32(r.IA.Real), f32(r.IA.Imag), f32(r.IA.Magnitude), f32(r.IA.PhaseDegrees),
		f32(r.VoltageImbalancePercent),
		f32(r.VAB_PhaseAngleDifference),
		f32(r.VBC_PhaseAngleDifference),
		f32(r.VCA_PhaseAngleDifference),
		f32(r.SequencePos), f32(r.SequenceNeg), f32(r.SequenceZero),
		f32(r.MW), f32(r.MVAR), f32(r.MVA), f32(r.PowerFactor), r.PowerFactorLeadLag,
		f32(r.TotalPowerReal), f32(r.TotalPowerImag),
		strconv.FormatUint(uint64(r.Digital), 10),
		strings.Join(digs, " "),
	}
	for _, name := range phasorNames {
		ph := phByName[name]
		row = append(row, f32(ph.Magnitude), f32(ph.PhaseDegrees))
	}
	for _, name := range analogNames {
		row = append(row, f32(anByName[name]))
	}
	return row
}

func f32(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

func receivedAtNS(r parser.Reading) string {
	if r.Trace.ReceivedAtUnixNano == 0 {
		return ""
	}
	return strconv.FormatInt(r.Trace.ReceivedAtUnixNano, 10)
}

func receivedAtUTC(r parser.Reading) string {
	if r.Trace.ReceivedAtUnixNano == 0 {
		return ""
	}
	return time.Unix(0, r.Trace.ReceivedAtUnixNano).UTC().Format(time.RFC3339Nano)
}

// WriteSheetsHTML renders one spreadsheet-style table per PMU.
func WriteSheetsHTML(w io.Writer, snap BankSnapshot) error {
	phNames, anNames, headers := sheetColumns(snap)
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Aligner buffers</title>
<style>
body{font-family:Segoe UI,sans-serif;margin:16px;background:#f6f7f9;color:#1c2430}
h1{font-size:18px;margin:0 0 4px} p{color:#5c6b7a;font-size:13px}
a{color:#1d4f91}
section{background:#fff;border:1px solid #d7dee7;border-radius:8px;margin:16px 0;padding:12px}
h2{font-size:15px;margin:0 0 8px}
.wrap{overflow:auto;max-height:70vh}
table{border-collapse:collapse;font-size:12px;white-space:nowrap}
th,td{border:1px solid #e3e8ee;padding:4px 8px;text-align:right}
th{position:sticky;top:0;background:#eef3f8;text-align:left}
td:nth-child(-n+3),th:nth-child(-n+3){text-align:left}
</style></head><body>`)
	fmt.Fprintf(&b, "<h1>Aligner buffers</h1><p>%s · capacity %d · candidate %d · <a href=\"/conversation/aligner-buffers/sheets.xls\">Download Excel</a> · <a href=\"/conversation/aligner-buffers\">JSON</a></p>",
		html.EscapeString(snap.AtUTC), snap.Capacity, snap.CandidatePublishMs)
	if len(snap.PMUs) == 0 {
		b.WriteString("<p>No PMUs in the aligner yet.</p>")
	}
	for _, pmu := range snap.PMUs {
		fmt.Fprintf(&b, "<section><h2>%s · %d / %d slots</h2><div class=\"wrap\"><table><thead><tr>",
			html.EscapeString(pmu.Name), pmu.Count, pmu.Capacity)
		for _, h := range headers {
			fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(h))
		}
		b.WriteString("</tr></thead><tbody>")
		for _, slot := range pmu.Slots {
			b.WriteString("<tr>")
			for _, cell := range sheetRow(slot.TSMs, slot.TimeUTC, slot.Reading, phNames, anNames) {
				fmt.Fprintf(&b, "<td>%s</td>", html.EscapeString(cell))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table></div></section>")
	}
	b.WriteString("</body></html>")
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteSheetsExcel writes a workbook Excel can open, one worksheet per PMU.
func WriteSheetsExcel(w io.Writer, snap BankSnapshot) error {
	phNames, anNames, headers := sheetColumns(snap)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<?mso-application progid="Excel.Sheet"?>` + "\n")
	b.WriteString(`<Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet">` + "\n")
	if len(snap.PMUs) == 0 {
		writeWorksheet(&b, "empty", headers, nil)
	}
	for _, pmu := range snap.PMUs {
		rows := make([][]string, 0, len(pmu.Slots))
		for _, slot := range pmu.Slots {
			rows = append(rows, sheetRow(slot.TSMs, slot.TimeUTC, slot.Reading, phNames, anNames))
		}
		writeWorksheet(&b, safeSheetName(pmu.Name), headers, rows)
	}
	b.WriteString("</Workbook>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeWorksheet(b *strings.Builder, name string, headers []string, rows [][]string) {
	fmt.Fprintf(b, `<Worksheet ss:Name="%s"><Table>`, xmlEsc(name))
	b.WriteString("<Row>")
	for _, h := range headers {
		fmt.Fprintf(b, `<Cell><Data ss:Type="String">%s</Data></Cell>`, xmlEsc(h))
	}
	b.WriteString("</Row>")
	for _, row := range rows {
		b.WriteString("<Row>")
		for _, cell := range row {
			fmt.Fprintf(b, `<Cell><Data ss:Type="String">%s</Data></Cell>`, xmlEsc(cell))
		}
		b.WriteString("</Row>")
	}
	b.WriteString("</Table></Worksheet>\n")
}

func safeSheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case ':', '\\', '/', '?', '*', '[', ']':
			return '_'
		default:
			return r
		}
	}, name)
	if name == "" {
		name = "pmu"
	}
	if len(name) > 31 {
		name = name[:31]
	}
	return name
}

func xmlEsc(s string) string {
	return html.EscapeString(s)
}
