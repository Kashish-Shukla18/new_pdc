// Package aligner:
//  1. Checker — is this reading's clock / quality okay?
//  2. Bank    — per-PMU timestamp buffers (new time-align, built step by step)
//
// Why a time-indexed map (not a plain circular ring of arrivals)?
//   The publish plan needs: "for timestamp T, does this PMU have data?"
//   A map[timestamp] → reading answers that in one lookup.
//   A circular buffer of "last 50 arrivals" would force a scan every time,
//   and arrivals can repeat or arrive slightly out of order.
//   We still cap at N slots and drop the oldest timestamp — same memory idea,
//   just keyed by time so the algorithm matches the plan.
package aligner

import (
	"fmt"
	"math"
	"time"

	"pdc/parser"
)

// Checker validates time sync and basic quality before data goes further.
type Checker struct {
	MaxClockSkew time.Duration
}

func NewChecker(maxClockSkew time.Duration) *Checker {
	if maxClockSkew <= 0 {
		maxClockSkew = 5 * time.Second
	}
	return &Checker{MaxClockSkew: maxClockSkew}
}

// Validate returns an error when the reading should be treated as bad.
func (c *Checker) Validate(r parser.Reading) error {
	if r.Timestamp.IsZero() {
		return fmt.Errorf("missing timestamp")
	}

	skew := time.Since(r.Timestamp)
	if skew < 0 {
		skew = -skew
	}
	if skew > c.MaxClockSkew {
		return fmt.Errorf("timestamp skew too high: %s", skew)
	}

	// STAT bits 15..14: 0 means valid data.
	if q := (r.Stat >> 14) & 0x3; q != 0 {
		return fmt.Errorf("bad status flag: %d", q)
	}

	f := float64(r.Frequency)
	if math.IsNaN(f) || f < 45 || f > 65 {
		return fmt.Errorf("frequency out of range: %.3f", r.Frequency)
	}

	return nil
}

// TimestampKey is the aligner buffer / publish key for one reading.
// It is exactly the C37.118 measurement instant from the parsed data frame
// (SOC + FRACSEC → Reading.Timestamp). No receive-time remapping: streams
// only share a tick when their device stamps agree (within the publish window).
func TimestampKey(r parser.Reading) int64 {
	if r.Timestamp.IsZero() {
		return 0
	}
	return r.Timestamp.UTC().UnixMilli()
}
