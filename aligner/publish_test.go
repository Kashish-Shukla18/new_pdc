package aligner

import (
	"context"
	"sync"
	"testing"
	"time"

	"pdc/parser"
)

func TestPublisherEmitsCompletePartialAndGap(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"a", "b"})

	type batch struct {
		ts       int64
		nPresent int
		nMissing int
		reason   string
	}
	var mu sync.Mutex
	var got []batch

	pub := NewPublisher(bank, 50, 0, func(ts int64, present map[string]parser.Reading, missing []string, complete bool, reason string) {
		mu.Lock()
		got = append(got, batch{ts: ts, nPresent: len(present), nMissing: len(missing), reason: reason})
		mu.Unlock()
	})

	bank.Ingest(makeReading("a", 1000))
	bank.Ingest(makeReading("b", 1000))
	bank.Ingest(makeReading("a", 1050))
	bank.Ingest(makeReading("a", 1100))
	bank.Ingest(makeReading("b", 1100))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pub.Run(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 3 {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("timed out waiting for batches, got %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if got[0].ts != 1000 || got[0].nPresent != 2 || got[0].reason != "complete" {
		t.Fatalf("batch0=%+v want ts=1000 complete both", got[0])
	}
	if got[1].ts != 1050 || got[1].nPresent != 1 || got[1].nMissing != 1 {
		t.Fatalf("batch1=%+v want ts=1050 partial", got[1])
	}
	if got[2].ts != 1100 || got[2].nPresent != 2 {
		t.Fatalf("batch2=%+v want ts=1100 both", got[2])
	}
}

func TestPublisherResetOnSetExpected(t *testing.T) {
	bank := NewBank(50)
	var mu sync.Mutex
	var ticks []int64
	pub := NewPublisher(bank, 50, 0, func(ts int64, _ map[string]parser.Reading, _ []string, _ bool, _ string) {
		mu.Lock()
		ticks = append(ticks, ts)
		mu.Unlock()
	})

	bank.SetExpected([]string{"a"})
	bank.Ingest(makeReading("a", 2000))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pub.Run(ctx)

	waitFor := func(min int) {
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(ticks)
			mu.Unlock()
			if n >= min {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("want >= %d ticks", min)
	}
	waitFor(1)

	bank.SetExpected([]string{"a", "b"})
	bank.Ingest(makeReading("a", 3000))
	bank.Ingest(makeReading("b", 3000))
	waitFor(2)

	cancel()
	mu.Lock()
	defer mu.Unlock()
	found3000 := false
	for _, ts := range ticks {
		if ts == 3000 {
			found3000 = true
		}
	}
	if !found3000 {
		t.Fatalf("expected re-anchor at 3000, ticks=%v", ticks)
	}
}

func TestPublisherDoesNotRaceAheadOfSlowPMU(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"a", "b"})

	var mu sync.Mutex
	var ticks []int64
	pub := NewPublisher(bank, 50, 0, func(ts int64, _ map[string]parser.Reading, _ []string, _ bool, _ string) {
		mu.Lock()
		ticks = append(ticks, ts)
		mu.Unlock()
	})

	bank.Ingest(makeReading("a", 1000))
	bank.Ingest(makeReading("b", 1000))
	bank.Ingest(makeReading("a", 1050))
	bank.Ingest(makeReading("a", 2000))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go pub.Run(ctx)

	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	last := int64(0)
	if len(ticks) > 0 {
		last = ticks[len(ticks)-1]
	}
	mu.Unlock()
	if last > 1000 {
		t.Fatalf("publisher raced to ts=%d with b still at 1000; ticks=%v", last, ticks)
	}

	bank.Ingest(makeReading("b", 1050))
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen1050 := false
		for _, ts := range ticks {
			if ts == 1050 {
				seen1050 = true
			}
		}
		mu.Unlock()
		if seen1050 {
			cancel()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("expected tick 1050 after b caught up, ticks=%v", ticks)
}

// Typhoon-style 25 fps: samples at …159, …200, …240 on a 40 ms ruler.
// Exact matching emitted empty "gap" rows (~54% complete). Nearest+skip should
// publish only real samples as complete.
func TestPublisherOffGridSinglePMU(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"t"})

	var mu sync.Mutex
	var got []struct {
		ts       int64
		nPresent int
		reason   string
	}
	pub := NewPublisher(bank, 40, 0, func(ts int64, present map[string]parser.Reading, _ []string, _ bool, reason string) {
		mu.Lock()
		got = append(got, struct {
			ts       int64
			nPresent int
			reason   string
		}{ts, len(present), reason})
		mu.Unlock()
	})

	for _, ts := range []int64{9159, 9200, 9240, 9280} {
		bank.Ingest(makeReading("t", ts))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pub.Run(ctx)
	}()

	deadline := time.Now().Add(800 * time.Millisecond)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 4 {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 4 {
		t.Fatalf("got %d emits want >= 4: %+v", len(got), got)
	}
	for i, b := range got {
		if b.nPresent != 1 || b.reason != "complete" {
			t.Fatalf("emit[%d]=%+v want complete with 1 present", i, b)
		}
	}
}
