package aligner

import (
	"sync"
	"testing"
	"time"

	"pdc/parser"
)

func reading(pmu string, ts time.Time) parser.Reading {
	return parser.Reading{
		PMUName:   pmu,
		Timestamp: ts,
		Frequency: 50,
	}
}

func warmTwo(c *Concentrator, a, b string) {
	t0 := time.Unix(1_699_000_000, 0).UTC()
	c.Push(reading(a, t0))
	c.Push(reading(b, t0))
	c.Push(reading(a, t0)) // complete partial from B after A was alone
}

func TestRelativeWaitEmitsPartialOnTimeout(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		Wait:        35 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})
	warmTwo(c, "A", "B")

	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := time.Unix(1_700_000_200, 0).UTC()
	c.Push(reading("A", slot))

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(sets)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sets) < 1 {
		t.Fatal("expected aligned set after relative wait")
	}
	last := sets[len(sets)-1]
	if !last.Timestamp.Equal(slot) {
		t.Fatalf("timestamp: got %v want %v", last.Timestamp, slot)
	}
	if _, ok := last.Present["A"]; !ok {
		t.Fatalf("expected A present: %+v", last.Present)
	}
	foundMissing := false
	for _, m := range last.Missing {
		if m == "B" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("expected B missing, got %+v", last.Missing)
	}
	c.Close()
}

func TestRelativeWaitEarlyComplete(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		Wait:        200 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})
	warmTwo(c, "A", "B")

	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := time.Unix(1_700_001_100, 0).UTC()
	start := time.Now()
	c.Push(reading("A", slot))
	c.Push(reading("B", slot))
	elapsed := time.Since(start)

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != 1 {
		t.Fatalf("expected 1 early-complete set, got %d (%+v)", len(sets), sets)
	}
	if !sets[0].Complete {
		t.Fatalf("expected complete set: %+v", sets[0])
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("early complete should not wait full window: elapsed=%s", elapsed)
	}
	c.Close()
}

func TestLateArrivalDoesNotReopenEmittedTimestamp(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		Wait:        25 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
		EmittedTTL:  time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})
	warmTwo(c, "A", "B")
	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := time.Unix(1_700_002_000, 0).UTC()
	c.Push(reading("A", slot))

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(sets)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	nBefore := len(sets)
	mu.Unlock()
	if nBefore < 1 {
		t.Fatal("expected timeout emit before late arrival")
	}

	// Late peer for the same timestamp must NOT create another set.
	c.Push(reading("B", slot))
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != nBefore {
		t.Fatalf("late reopen created extra sets: before=%d after=%d last=%+v", nBefore, len(sets), sets[len(sets)-1])
	}
	c.Close()
}

func TestSkewedPeerJoinsWithinWait(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		Wait:        150 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})
	warmTwo(c, "A", "B")
	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := time.Unix(1_700_003_000, 0).UTC()
	c.Push(reading("A", slot))
	time.Sleep(80 * time.Millisecond) // skewed, but inside wait
	c.Push(reading("B", slot))

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != 1 {
		t.Fatalf("expected 1 complete set, got %d (%+v)", len(sets), sets)
	}
	if !sets[0].Complete || len(sets[0].Present) != 2 {
		t.Fatalf("expected complete 2-PMU set: %+v", sets[0])
	}
	c.Close()
}

func TestCorrectedKeyGroupsSkewedClocks(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	offsets := map[string]time.Duration{
		"A": 0,
		"B": 1500 * time.Millisecond, // B's SOC lags by 1.5s
	}
	c := NewConcentrator(Config{
		Enabled:   true,
		Mode:      WaitRelative,
		BucketKey: BucketCorrected,
		OffsetFunc: func(pmu string) time.Duration {
			return offsets[pmu]
		},
		Quantize:    20 * time.Millisecond,
		Wait:        100 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})

	// Warm membership with matching corrected times.
	base := time.Unix(1_700_010_000, 0).UTC()
	c.Push(reading("A", base))
	c.Push(reading("B", base.Add(-1500*time.Millisecond)))
	c.Push(reading("A", base))

	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := time.Unix(1_700_010_100, 0).UTC()
	c.Push(reading("A", slot))
	c.Push(reading("B", slot.Add(-1500*time.Millisecond)))

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != 1 {
		t.Fatalf("expected 1 set, got %d (%+v)", len(sets), sets)
	}
	if !sets[0].Complete || len(sets[0].Present) != 2 {
		t.Fatalf("expected complete set from corrected keys: %+v", sets[0])
	}
	c.Close()
}

func TestNearestBucketJoinsWithinWait(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		BucketKey:   BucketReceive,
		Quantize:    20 * time.Millisecond,
		Wait:        150 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})

	// Simulate receive-time keys 40ms apart (would be different 20ms quanta).
	t0 := time.Unix(0, 1_700_020_000_000*int64(time.Millisecond))
	rA := reading("A", t0)
	rA.Trace.ReceivedAtUnixNano = t0.UnixNano()
	rB := reading("B", t0.Add(40*time.Millisecond))
	rB.Trace.ReceivedAtUnixNano = t0.Add(40 * time.Millisecond).UnixNano()

	c.Push(rA)
	c.Push(rB) // alone → membership
	c.Push(rA) // establish 2 active with nearby keys

	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	slot := t0.Add(time.Second)
	rA2 := reading("A", slot)
	rA2.Trace.ReceivedAtUnixNano = slot.UnixNano()
	rB2 := reading("B", slot)
	rB2.Trace.ReceivedAtUnixNano = slot.Add(40 * time.Millisecond).UnixNano()

	c.Push(rA2)
	c.Push(rB2)

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != 1 {
		t.Fatalf("expected 1 joined set, got %d (%+v)", len(sets), sets)
	}
	if !sets[0].Complete || len(sets[0].Present) != 2 {
		t.Fatalf("expected complete nearest-bucket join: %+v", sets[0])
	}
	c.Close()
}

func TestDisabledPassthrough(t *testing.T) {
	var got AlignedSet
	c := NewConcentrator(Config{Enabled: false, Wait: time.Millisecond}, func(set AlignedSet) {
		got = set
	})
	ts := time.Unix(42, 0).UTC()
	c.Push(reading("X", ts))
	if len(got.Present) != 1 || got.Present["X"].PMUName != "X" {
		t.Fatalf("passthrough failed: %+v", got)
	}
	c.Close()
}

func TestOrderedEmitHoldsNewerUntilOlderReady(t *testing.T) {
	var mu sync.Mutex
	var sets []AlignedSet
	c := NewConcentrator(Config{
		Enabled:     true,
		Mode:        WaitRelative,
		Wait:        80 * time.Millisecond,
		BufferDepth: 50,
		ActiveTTL:   time.Second,
	}, func(set AlignedSet) {
		mu.Lock()
		sets = append(sets, set)
		mu.Unlock()
	})
	warmTwo(c, "A", "B")
	mu.Lock()
	sets = sets[:0]
	mu.Unlock()

	t0 := time.Unix(1_700_004_000, 0).UTC()
	t1 := t0.Add(20 * time.Millisecond)

	// Older timestamp incomplete (only A); newer completes first.
	c.Push(reading("A", t0))
	c.Push(reading("A", t1))
	c.Push(reading("B", t1))

	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	nMid := len(sets)
	mu.Unlock()
	if nMid != 0 {
		t.Fatalf("newer set must not emit before older is ready, got %d sets", nMid)
	}

	// Complete older → both should drain in order.
	c.Push(reading("B", t0))

	mu.Lock()
	defer mu.Unlock()
	if len(sets) != 2 {
		t.Fatalf("expected 2 ordered sets, got %d (%+v)", len(sets), sets)
	}
	if !sets[0].Timestamp.Equal(t0) || !sets[1].Timestamp.Equal(t1) {
		t.Fatalf("out of order: %v then %v", sets[0].Timestamp, sets[1].Timestamp)
	}
	c.Close()
}
