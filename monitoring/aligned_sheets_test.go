package monitoring

import (
	"strings"
	"testing"
)

func TestAlignedSheetsRowFromBatch(t *testing.T) {
	batches := []AlignedBatch{{
		TS:       1_700_000_000_000,
		Complete: true,
		Reason:   "complete",
		Missing:  nil,
		Points: map[string]AlignedPoint{
			"pmu-a": {
				Frequency:    50.01,
				FrequencyDev: 0.01,
				ROCOF:        0.02,
				VA:           110.5,
				VAAngle:      -12.3,
				Analogs:      map[string]float64{"Analog1": 1.25},
			},
		},
	}}
	analogs := alignedAnalogNames(batches)
	if len(analogs) != 1 || analogs[0] != "Analog1" {
		t.Fatalf("analogs=%v", analogs)
	}
	headers := alignedHeaders(analogs)
	joined := strings.Join(headers, ",")
	if !strings.Contains(joined, "frequency_hz") || !strings.Contains(joined, "Analog1") {
		t.Fatalf("headers=%s", joined)
	}
	rows := alignedRows(batches, analogs)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	row := strings.Join(rows[0], ",")
	if !strings.Contains(row, "pmu-a") || !strings.Contains(row, "50.01") || !strings.Contains(row, "1.25") {
		t.Fatalf("row=%s", row)
	}
}
