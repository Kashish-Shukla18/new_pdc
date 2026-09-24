package monitoring

// clock_skew.go — per-PMU clock model for latency metrics only.
//
// Alignment keys use raw parsed SOC+FRACSEC (see aligner.TimestampKey).
// These offsets still correct e2e lag charts so hour-scale device clocks
// do not look like multi-hour "pipeline delay".

import (
	"log"
	"sync"
	"time"
)

// How wrong a single sample may be before we treat it as corrupt (not "just unsynced").
const maxClockOffsetSample = 48 * time.Hour

// If the estimate jumps by more than this vs the previous value, reset hard
// (device reboot / clock step) instead of crawling there via EMA.
const clockOffsetStepReset = time.Minute

// Warn once per PMU when |offset| exceeds this (operator should fix GPS/time).
const clockOffsetWarnAfter = time.Second

// Per-PMU estimate of (PDC receive time − PMU SOC timestamp).
// Positive offset means the PMU clock lags the PDC wall clock.
var clockSkew = struct {
	mu      sync.Mutex
	pmus    map[string]time.Duration
	warned  map[string]bool
}{
	pmus:   make(map[string]time.Duration),
	warned: make(map[string]bool),
}

// UpdateClockOffset refines the per-PMU clock offset from one frame.
// receivedAt is when the PDC first saw the frame; pmuTime is SOC+FRACSEC.
func UpdateClockOffset(pmu string, receivedAt, pmuTime time.Time) {
	if pmu == "" || receivedAt.IsZero() || pmuTime.IsZero() {
		return
	}
	sample := receivedAt.Sub(pmuTime)
	if sample < -maxClockOffsetSample || sample > maxClockOffsetSample {
		return // absurd / corrupt SOC
	}

	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	prev, ok := clockSkew.pmus[pmu]
	if !ok || absDuration(sample-prev) > clockOffsetStepReset {
		clockSkew.pmus[pmu] = sample
	} else {
		// EMA α=0.05 — stable but tracks slow drift.
		const alpha = 0.05
		clockSkew.pmus[pmu] = time.Duration(float64(prev)*(1-alpha) + float64(sample)*alpha)
	}
	off := clockSkew.pmus[pmu]
	if absDuration(off) >= clockOffsetWarnAfter && !clockSkew.warned[pmu] {
		clockSkew.warned[pmu] = true
		log.Printf("[%s] clock offset %s — aligner will correct timestamps onto PDC receive timeline (fix device GPS/time for true synchrophasor align)",
			pmu, FormatMs(off))
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// ClockOffset returns the estimated PMU→PDC clock offset (0 if unknown).
func ClockOffset(pmu string) time.Duration {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	return clockSkew.pmus[pmu]
}

// HasClockOffset is true after at least one accepted sample for this PMU.
func HasClockOffset(pmu string) bool {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	_, ok := clockSkew.pmus[pmu]
	return ok
}

// CorrectedTime maps a PMU measurement instant onto PDC receive wall time
// for latency display: pmuTime + ClockOffset ≈ receivedAt.
// Not used as an aligner buffer key.
func CorrectedTime(pmu string, pmuTime time.Time) time.Time {
	if pmuTime.IsZero() {
		return pmuTime
	}
	clockSkew.mu.Lock()
	off, ok := clockSkew.pmus[pmu]
	clockSkew.mu.Unlock()
	if !ok {
		return pmuTime.UTC()
	}
	return pmuTime.UTC().Add(off)
}

// CorrectedPMULag subtracts estimated clock skew from (now − pmuTime).
// Result ≈ pipeline delay from TCP receive → dashboard, not wall-clock skew.
func CorrectedPMULag(pmu string, pmuTime time.Time) time.Duration {
	if pmuTime.IsZero() {
		return 0
	}
	return time.Since(CorrectedTime(pmu, pmuTime))
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

// ClearClockOffset drops the estimate (PMU removed / reconnect with new clock).
func ClearClockOffset(pmu string) {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	delete(clockSkew.pmus, pmu)
	delete(clockSkew.warned, pmu)
}

// ClearAllClockOffsets drops every estimate (aligner PMU-set reset).
func ClearAllClockOffsets() {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	clockSkew.pmus = make(map[string]time.Duration)
	clockSkew.warned = make(map[string]bool)
}

// AllClockOffsets returns a copy of per-PMU offset estimates in milliseconds (dashboard).
func AllClockOffsets() map[string]float64 {
	clockSkew.mu.Lock()
	defer clockSkew.mu.Unlock()
	out := make(map[string]float64, len(clockSkew.pmus))
	for pmu, d := range clockSkew.pmus {
		out[pmu] = Ms(d)
	}
	return out
}
