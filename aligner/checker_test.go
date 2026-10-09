package aligner

import (
	"strings"
	"testing"
	"time"

	"pdc/parser"
)

func TestCheckerRejectsUnsynchronized(t *testing.T) {
	c := NewChecker(5 * time.Second)
	r := parser.Reading{
		Timestamp:  time.Now().UTC(),
		Frequency:  50,
		Stat:       1 << 10,
		StatDetail: parser.STATDecoded{PMUSyncStatus: true},
	}
	err := c.Validate(r)
	if err == nil || !strings.Contains(err.Error(), "unsynchronized") {
		t.Fatalf("want unsync reject, got %v", err)
	}
}

func TestCheckerAcceptsGoodReading(t *testing.T) {
	c := NewChecker(5 * time.Second)
	r := parser.Reading{
		Timestamp: time.Now().UTC(),
		Frequency: 50.01,
		Stat:      0,
	}
	if err := c.Validate(r); err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
}

func TestCheckerRejectsSkewBeyondDefault(t *testing.T) {
	c := NewChecker(5 * time.Second)
	r := parser.Reading{
		Timestamp: time.Now().UTC().Add(-30 * time.Second),
		Frequency: 50,
		Stat:      0,
	}
	err := c.Validate(r)
	if err == nil || !strings.Contains(err.Error(), "skew") {
		t.Fatalf("want skew reject, got %v", err)
	}
}
