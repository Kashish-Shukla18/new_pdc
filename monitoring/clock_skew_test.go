package monitoring

import (
	"testing"
	"time"
)

func TestClockOffsetAndCorrectedLag(t *testing.T) {
	pmu := "skew-test"
	pmuTime := time.Now().UTC().Add(-650 * time.Millisecond)
	receivedAt := time.Now().UTC()

	UpdateClockOffset(pmu, receivedAt, pmuTime)
	offset := ClockOffset(pmu)
	if offset < 600*time.Millisecond || offset > 700*time.Millisecond {
		t.Fatalf("offset=%s want ~650ms", offset)
	}

	raw := time.Since(pmuTime)
	corrected := CorrectedPMULag(pmu, pmuTime)
	if raw-corrected < 500*time.Millisecond {
		t.Fatalf("raw=%s corrected=%s; corrected should be much smaller than raw", raw, corrected)
	}
	if corrected > 100*time.Millisecond {
		t.Fatalf("corrected=%s should be near-zero immediately after offset update", corrected)
	}
}

func TestClockOffsetRejectsGarbage(t *testing.T) {
	pmu := "garbage"
	UpdateClockOffset(pmu, time.Now(), time.Now().Add(-1*time.Hour))
	if ClockOffset(pmu) != 0 {
		t.Fatal("should reject 1h skew sample as garbage on first sample")
	}
}
