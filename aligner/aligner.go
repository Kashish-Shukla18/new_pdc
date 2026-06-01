package aligner

import (
	"fmt"
	"math"
	"time"

	"pdc/parser"
)

// Checker validates time sync and basic quality before data is persisted.
type Checker struct {
	MaxClockSkew time.Duration
}

func NewChecker(maxClockSkew time.Duration) *Checker {
	if maxClockSkew <= 0 {
		maxClockSkew = 5 * time.Second
	}
	return &Checker{MaxClockSkew: maxClockSkew}
}

// Validate returns an error when the reading should be treated as bad/missing.
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

	// STAT bits 15..14 represent data error flags. 0 means valid data.
	if q := (r.Stat >> 14) & 0x3; q != 0 {
		return fmt.Errorf("bad status flag: %d", q)
	}

	f := float64(r.Frequency)
	if math.IsNaN(f) || f < 45 || f > 65 {
		return fmt.Errorf("frequency out of range: %.3f", r.Frequency)
	}

	return nil
}
