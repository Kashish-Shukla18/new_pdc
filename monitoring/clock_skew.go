package monitoring

import (
	"sync"
	"time"
)

// Per-PMU estimate of (PDC receive time − PMU SOC timestamp).
// Positive offset means the PMU clock lags the PDC wall clock.
var clockSkew = struct {
	mu   sync.Mutex
	pmus map[string]time.Duration
}{
	pmus: make(map[string]time.Duration),
}

// UpdateClockOffset refines the per-PMU clock offset from one frame.
// receivedAt is when the PDC first saw the frame; pmuTime is SOC+FRACSEC.
func UpdateClockOffset(pmu string, receivedAt, pmuTime time.Time) {
	if pmu == "" || receivedAt.IsZero() || pmuTime.IsZero() {
		return
	}
	sample := receivedAt.Sub(pmuTime)
	// Ignore garbage (wrong TIME_BASE, leap, corrupt SOC).
	if sample < -2*time.Minute || sample > 2*time.Minute {
		return
	}

	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	prev, ok := clockSkew.pmus[pmu]
	if !ok {
		clockSkew.pmus[pmu] = sample
		return
	}
	// EMA α=0.05 — stable but tracks slow drift.
	const alpha = 0.05
	clockSkew.pmus[pmu] = time.Duration(float64(prev)*(1-alpha) + float64(sample)*alpha)
}

// ClockOffset returns the estimated PMU→PDC clock offset (0 if unknown).
func ClockOffset(pmu string) time.Duration {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	return clockSkew.pmus[pmu]
}

// CorrectedPMULag subtracts estimated clock skew from (now − pmuTime).
// Result ≈ pipeline delay from TCP receive → dashboard, not wall-clock skew.
func CorrectedPMULag(pmu string, pmuTime time.Time) time.Duration {
	if pmuTime.IsZero() {
		return 0
	}
	return time.Since(pmuTime.Add(ClockOffset(pmu)))
}

// ObservePMUClockMetrics updates skew estimate and records raw/corrected PMU E2E.
func ObservePMUClockMetrics(pmu string, receivedAt, pmuTime time.Time) {
	if pmuTime.IsZero() {
		return
	}
	if !receivedAt.IsZero() {
		UpdateClockOffset(pmu, receivedAt, pmuTime)
	}
	offset := ClockOffset(pmu)
	if offset != 0 {
		ObserveStage(pmu, StageClockSkewPMU, offset)
	}
	rawLag := time.Since(pmuTime)
	if rawLag >= 0 && rawLag < 10*time.Second {
		ObserveStage(pmu, StageE2EPMUToDash, rawLag)
	}
	corrected := CorrectedPMULag(pmu, pmuTime)
	if corrected >= 0 && corrected < 5*time.Minute {
		ObserveStage(pmu, StageE2EPMUCorrected, corrected)
	}
}

// AllClockOffsets returns a copy of per-PMU offset estimates (for dashboard).
func AllClockOffsets() map[string]float64 {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	out := make(map[string]float64, len(clockSkew.pmus))
	for pmu, d := range clockSkew.pmus {
		out[pmu] = Ms(d)
	}
	return out
}
