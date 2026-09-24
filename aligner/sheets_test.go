package aligner

import (
	"strings"
	"testing"
	"time"

	"pdc/parser"
)

func TestSheetsIncludeFullReadingColumns(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"pmu-1"})
	r := parser.Reading{
		PMUName:   "pmu-1",
		Timestamp: time.UnixMilli(1000).UTC(),
		Frequency: 60.1,
		VA:        parser.Phasor{Magnitude: 95, PhaseDegrees: 10},
		MW:        1.5,
		Phasors:   []parser.NamedPhasor{{Name: "VA", Phasor: parser.Phasor{Magnitude: 95, PhaseDegrees: 10}}},
	}
	bank.Ingest(r)
	snap := bank.Snapshot()
	_, _, headers := sheetColumns(snap)
	joined := strings.Join(headers, ",")
	if !strings.Contains(joined, "frequency_hz") || !strings.Contains(joined, "mw") || !strings.Contains(joined, "VA_mag") || !strings.Contains(joined, "received_at_utc") {
		t.Fatalf("headers missing fields: %s", joined)
	}
	row := sheetRow(snap.PMUs[0].Slots[0].TSMs, snap.PMUs[0].Slots[0].TimeUTC, snap.PMUs[0].Slots[0].Reading, []string{"VA"}, nil)
	foundFreq := false
	for _, cell := range row {
		if cell == "60.1" {
			foundFreq = true
		}
	}
	if !foundFreq {
		t.Fatalf("row missing frequency: %#v", row)
	}
}
