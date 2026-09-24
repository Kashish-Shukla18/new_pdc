package monitoring

import (
	"testing"
	"time"
)

func TestClockOffsetAndCorrectedLag(t *testing.T) {
	pmu := "skew-test-ok"
	ClearClockOffset(pmu)
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

func TestClockOffsetAcceptsLargeSimSkew(t *testing.T) {
	pmu := "skew-hours"
	ClearClockOffset(pmu)
	pmuTime := time.Now().UTC().Add(-5 * time.Hour)
	receivedAt := time.Now().UTC()
	UpdateClockOffset(pmu, receivedAt, pmuTime)
	off := ClockOffset(pmu)
	if off < 4*time.Hour || off > 6*time.Hour {
		t.Fatalf("offset=%s want ~5h for sim clock", off)
	}
	got := CorrectedTime(pmu, pmuTime)
	if d := got.Sub(receivedAt); d < -50*time.Millisecond || d > 50*time.Millisecond {
		t.Fatalf("CorrectedTime=%s recv=%s delta=%s want ~0", got, receivedAt, d)
	}
}

func TestClockOffsetRejectsAbsurd(t *testing.T) {
	pmu := "garbage"
	ClearClockOffset(pmu)
	UpdateClockOffset(pmu, time.Now(), time.Now().Add(-72*time.Hour))
	if ClockOffset(pmu) != 0 {
		t.Fatal("should reject 72h skew sample as corrupt")
	}
}

func TestClockOffsetStepReset(t *testing.T) {
	pmu := "step"
	ClearClockOffset(pmu)
	now := time.Now().UTC()
	UpdateClockOffset(pmu, now, now.Add(-100*time.Millisecond))
	UpdateClockOffset(pmu, now, now.Add(-3*time.Hour)) // big step → hard reset
	off := ClockOffset(pmu)
	if off < 2*time.Hour {
		t.Fatalf("after step reset offset=%s want ~3h", off)
	}
}
