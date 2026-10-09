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
	if got[0].ts != us(1000) || got[0].nPresent != 2 || got[0].reason != "complete" {
		t.Fatalf("batch0=%+v want ts=%d complete both", got[0], us(1000))
	}
	if got[1].ts != us(1050) || got[1].nPresent != 1 || got[1].nMissing != 1 {
		t.Fatalf("batch1=%+v want ts=%d partial", got[1], us(1050))
	}
	if got[2].ts != us(1100) || got[2].nPresent != 2 {
		t.Fatalf("batch2=%+v want ts=%d both", got[2], us(1100))
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
		if ts == us(3000) {
			found3000 = true
		}
	}
	if !found3000 {
		t.Fatalf("expected re-anchor at %d, ticks=%v", us(3000), ticks)
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
	if last > us(1000) {
		t.Fatalf("publisher raced to ts=%d with b still at 1000; ticks=%v", last, ticks)
	}

	bank.Ingest(makeReading("b", 1050))
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen1050 := false
		for _, ts := range ticks {
			if ts == us(1050) {
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
	t.Fatalf("expected tick %d after b caught up, ticks=%v", us(1050), ticks)
}

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

func TestPublisherContinuesAfterLivePMURemoved(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"a", "b", "c"})

	var mu sync.Mutex
	var completeAfterRemove int
	var sawPartialForC bool
	removed := false

	pub := NewPublisher(bank, 50, 0, func(ts int64, present map[string]parser.Reading, missing []string, complete bool, reason string) {
		mu.Lock()
		defer mu.Unlock()
		if !removed {
			return
		}
		if complete && len(present) == 2 && len(missing) == 0 {
			completeAfterRemove++
		}
		for _, m := range missing {
			if m == "c" {
				sawPartialForC = true
			}
		}
		_ = ts
		_ = reason
	})

	bank.Ingest(makeReading("a", 1000))
	bank.Ingest(makeReading("b", 1000))
	bank.Ingest(makeReading("c", 1000))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pub.Run(ctx)
	}()

	time.Sleep(40 * time.Millisecond)
	bank.RemoveExpected("c")
	mu.Lock()
	removed = true
	mu.Unlock()

	for ts := int64(1050); ts <= 1300; ts += 50 {
		bank.Ingest(makeReading("a", ts))
		bank.Ingest(makeReading("b", ts))
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := completeAfterRemove
		mu.Unlock()
		if n >= 3 {
			cancel()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	if completeAfterRemove < 3 {
		t.Fatalf("want >=3 complete A+B ticks after C left live set, got %d (sawPartialForC=%v)", completeAfterRemove, sawPartialForC)
	}
	if sawPartialForC {
		t.Fatal("C should not appear as missing after RemoveExpected — it left the live set")
	}
}

func TestDerivePeriodUsSixtyFPS(t *testing.T) {
	parser.SetProfile("s60", parser.Profile{DataRate: 60})
	t.Cleanup(func() { parser.SetProfile("s60", parser.Profile{}) })

	got := DerivePeriodUs([]string{"s60"}, DefaultPeriodUs)
	want := int64(16667) // Round(1e6/60)
	if got != want {
		t.Fatalf("60fps period=%dµs want %d (not 17000)", got, want)
	}
	// Old ms rounding would be 17 ms = 17000 µs — must not regress.
	if got == 17_000 {
		t.Fatal("still using millisecond rounding")
	}
}

func TestPublisherSixtyFPSEmitsNearInputRate(t *testing.T) {
	parser.SetProfile("t60", parser.Profile{DataRate: 60})
	t.Cleanup(func() { parser.SetProfile("t60", parser.Profile{}) })

	bank := NewBank(200)
	bank.SetExpected([]string{"t60"})

	var mu sync.Mutex
	var emits int
	pub := NewPublisher(bank, DerivePeriodUs([]string{"t60"}, DefaultPeriodUs), 0,
		func(ts int64, present map[string]parser.Reading, _ []string, complete bool, reason string) {
			mu.Lock()
			if complete && reason == "complete" && len(present) == 1 {
				emits++
			}
			mu.Unlock()
			_ = ts
		})

	// One second of 60 fps stamps on the µs grid (offset away from 0 — key 0 is ignored).
	const n = 60
	const base int64 = 1_000_000_000_000 // 1e12 µs
	for i := 0; i < n; i++ {
		usec := base + int64(float64(i)*1_000_000.0/60.0+0.5)
		bank.Ingest(parser.Reading{
			PMUName:   "t60",
			Timestamp: time.UnixMicro(usec).UTC(),
			Frequency: 50,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pub.Run(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		e := emits
		mu.Unlock()
		if e >= n {
			cancel()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-done

	mu.Lock()
	defer mu.Unlock()
	// Old 17 ms grid lost ~2%. µs grid should keep essentially all.
	if emits < n-1 {
		t.Fatalf("60fps complete emits=%d want >= %d (µs grid)", emits, n-1)
	}
}
