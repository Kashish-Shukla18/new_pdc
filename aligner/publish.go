package aligner

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"pdc/monitoring"
	"pdc/parser"
)

// DefaultPeriodUs = ruler step when CFG rate is unknown (50 ms → 20 fps).
const DefaultPeriodUs int64 = 50_000

// DefaultPeriodMs is DefaultPeriodUs in milliseconds (legacy env / display).
const DefaultPeriodMs int64 = DefaultPeriodUs / 1000

// EmitFunc hands one finished tick to the dashboard.
// tsUs is the aligner tick in Unix microseconds.
type EmitFunc func(tsUs int64, present map[string]parser.Reading, missing []string, complete bool, reason string)

// Publisher walks time in microsecond steps and builds one chart row per step.
//
// Using µs (not ms) lets 60 fps use period ≈ 16667 µs instead of rounded 17 ms,
// so the ruler has ~60 marks/second instead of ~59.
//
// No wait: as soon as the slowest live PMU has reached tick T (MinNewest),
// we emit whatever is present right now and move on.
type Publisher struct {
	bank     *Bank
	emit     EmitFunc
	periodUs int64

	mu       sync.Mutex
	nextTick int64 // 0 means "we have not picked a start time yet" (Unix µs)
	gen      uint64
}

// NewPublisher builds a publisher. periodUs is the ruler step in microseconds.
// waitUs is ignored (kept so call sites stay stable); emit is immediate.
// If periodUs looks like a legacy millisecond value (< 1000), it is treated as ms.
func NewPublisher(bank *Bank, periodUs, waitUs int64, emit EmitFunc) *Publisher {
	_ = waitUs
	periodUs = normalizePeriodUs(periodUs)
	p := &Publisher{
		bank:     bank,
		emit:     emit,
		periodUs: periodUs,
	}
	if bank != nil {
		bank.OnReset(p.Reset)
	}
	return p
}

func normalizePeriodUs(periodUs int64) int64 {
	if periodUs < 1 {
		return DefaultPeriodUs
	}
	// Legacy call sites passed milliseconds (e.g. 50 for 20 fps).
	if periodUs < 1000 {
		return periodUs * 1000
	}
	return periodUs
}

// Reset clears the current tick so we pick a new start after a PMU set change.
func (p *Publisher) Reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.nextTick = 0
	p.gen++
	p.mu.Unlock()
}

// PeriodUs is the ruler step in microseconds.
func (p *Publisher) PeriodUs() int64 {
	if p == nil {
		return DefaultPeriodUs
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.periodUs
}

// PeriodMs is PeriodUs/1000 (for display / legacy callers).
func (p *Publisher) PeriodMs() int64 {
	return p.PeriodUs() / 1000
}

// WaitMs is always 0 in no-wait mode.
func (p *Publisher) WaitMs() int64 { return 0 }

// SetPeriodUs changes the ruler step (microseconds).
func (p *Publisher) SetPeriodUs(us int64) {
	if p == nil {
		return
	}
	us = normalizePeriodUs(us)
	p.mu.Lock()
	p.periodUs = us
	p.mu.Unlock()
}

// SetPeriodMs sets the step from a millisecond value (converted to µs).
func (p *Publisher) SetPeriodMs(ms int64) {
	if ms < 1 {
		return
	}
	p.SetPeriodUs(ms * 1000)
}

// SetWaitMs is a no-op in no-wait mode.
func (p *Publisher) SetWaitMs(ms int64) { _ = ms }

// DerivePeriodUs turns CFG DATA_RATE (fps) into microseconds per tick.
// Example: 20 fps → 50000 µs; 60 fps → 16667 µs (not 17 ms).
func DerivePeriodUs(names []string, fallbackUs int64) int64 {
	fallbackUs = normalizePeriodUs(fallbackUs)
	bestFPS := 0.0
	for _, name := range names {
		prof, ok := parser.GetProfile(name)
		if !ok || prof.DataRate == 0 {
			continue
		}
		fps := float64(prof.DataRate)
		if prof.DataRate < 0 {
			fps = 1.0 / float64(-prof.DataRate)
		}
		if fps > bestFPS {
			bestFPS = fps
		}
	}
	if bestFPS < 1 {
		return fallbackUs
	}
	us := int64(math.Round(1_000_000.0 / bestFPS))
	if us < 1 {
		return 1
	}
	return us
}

// DerivePeriodMs is legacy: returns DerivePeriodUs(...)/1000.
func DerivePeriodMs(names []string, fallbackMs int64) int64 {
	return DerivePeriodUs(names, fallbackMs*1000) / 1000
}

// Run keeps publishing ticks until the program shuts down (ctx cancelled).
func (p *Publisher) Run(ctx context.Context) {
	if p == nil || p.bank == nil || p.emit == nil {
		return
	}
	log.Printf("[aligner] publisher on (period=%dµs ≈ %.3fms, no wait, head=MinNewest)",
		p.PeriodUs(), float64(p.PeriodUs())/1000.0)

	for {
		if ctx.Err() != nil {
			return
		}
		if p.stepOnce() {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// stepOnce tries to publish one tick. Returns true if a row was pushed.
func (p *Publisher) stepOnce() bool {
	p.mu.Lock()
	tick := p.nextTick
	period := p.periodUs
	gen := p.gen
	p.mu.Unlock()

	period = normalizePeriodUs(period)

	names := p.bank.Expected()
	if len(names) == 0 {
		return false
	}

	if derived := DerivePeriodUs(names, period); derived != period {
		p.SetPeriodUs(derived)
		period = derived
	}

	// First time: start at the oldest sample we already have.
	if tick == 0 {
		oldest, ok := p.bank.MinOldest()
		if !ok {
			return false
		}
		tick = (oldest / period) * period
		p.mu.Lock()
		if p.gen != gen {
			p.mu.Unlock()
			return false
		}
		p.nextTick = tick
		p.mu.Unlock()
		log.Printf("[aligner] charts start at tick=%dµs (%s)",
			tick, time.UnixMicro(tick).UTC().Format(time.RFC3339Nano))
	}

	// If the work-set moved past us (capacity dropped old samples), jump forward.
	if oldest, ok := p.bank.MinOldest(); ok && tick < oldest {
		tick = (oldest / period) * period
		p.mu.Lock()
		if p.gen != gen {
			p.mu.Unlock()
			return false
		}
		p.nextTick = tick
		p.mu.Unlock()
	}

	// Do not race ahead of the slowest live PMU.
	head, ok := p.bank.MinNewest()
	if !ok || tick > head {
		return false
	}

	// Accept stamps within ±half period (60 fps → ±~8.3 ms).
	half := period / 2
	present, missing := p.bank.TakeTickWindow(tick, half)
	if len(present) == 0 {
		p.mu.Lock()
		if p.gen == gen {
			p.nextTick = tick + period
		}
		p.mu.Unlock()
		return true
	}

	complete := len(names) > 0 && len(missing) == 0
	reason := "partial"
	if complete {
		reason = "complete"
	}

	p.emit(tick, present, missing, complete, reason)
	now := time.Now()
	for name, r := range present {
		if r.Trace.ReceivedAtUnixNano <= 0 {
			continue
		}
		recv := time.Unix(0, r.Trace.ReceivedAtUnixNano)
		if dwell := now.Sub(recv); dwell >= 0 && dwell < 5*time.Minute {
			monitoring.ObserveStage(name, monitoring.StageAlignWait, dwell)
		}
	}
	p.bank.PruneBefore(tick)

	p.mu.Lock()
	if p.gen == gen {
		p.nextTick = tick + period
	}
	p.mu.Unlock()
	return true
}
