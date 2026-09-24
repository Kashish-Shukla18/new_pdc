package aligner

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"pdc/parser"
)

// DefaultPeriodMs = how far apart each chart "tick" is when we do not know the
// PMU rate yet. 50 ms means 20 ticks per second (like 20 frames per second).
const DefaultPeriodMs int64 = 50

// EmitFunc is the function we call to hand one finished tick to the dashboard.
type EmitFunc func(tsMs int64, present map[string]parser.Reading, missing []string, complete bool, reason string)

// Publisher walks time in small steps and builds one chart row per step.
//
// No wait: as soon as the slowest live PMU has reached tick T (MinNewest),
// we emit whatever is present right now and move on.
//
// Picture a ruler marked every period ms (e.g. 40 ms at 25 fps). For each mark:
//  1. Do not advance past MinNewest (avoids racing the fastest stream).
//  2. If we fell behind the work-set, jump to MinOldest (avoids empty-gap flood).
//  3. Ask every PMU for T (± half period — real stamps often jitter 1 ms).
//  4. If nobody has a sample near T, skip the tick (do not paint a hole).
//  5. Otherwise push that row and keep newer samples (tail).
type Publisher struct {
	bank     *Bank
	emit     EmitFunc
	periodMs int64

	mu       sync.Mutex
	nextTick int64 // 0 means "we have not picked a start time yet"
	gen      uint64
}

// NewPublisher builds a publisher. Call Run in its own goroutine.
// waitMs is ignored (kept so call sites stay stable); emit is immediate.
func NewPublisher(bank *Bank, periodMs, waitMs int64, emit EmitFunc) *Publisher {
	_ = waitMs
	if periodMs < 1 {
		periodMs = DefaultPeriodMs
	}
	p := &Publisher{
		bank:     bank,
		emit:     emit,
		periodMs: periodMs,
	}
	if bank != nil {
		bank.OnReset(p.Reset)
	}
	return p
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

// PeriodMs is the step size of the ruler (milliseconds).
func (p *Publisher) PeriodMs() int64 {
	if p == nil {
		return DefaultPeriodMs
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.periodMs
}

// WaitMs is always 0 in no-wait mode.
func (p *Publisher) WaitMs() int64 { return 0 }

// SetPeriodMs changes the step size (for example after we learn CFG DATA_RATE).
func (p *Publisher) SetPeriodMs(ms int64) {
	if p == nil || ms < 1 {
		return
	}
	p.mu.Lock()
	p.periodMs = ms
	p.mu.Unlock()
}

// SetWaitMs is a no-op in no-wait mode.
func (p *Publisher) SetWaitMs(ms int64) { _ = ms }

// DerivePeriodMs turns "frames per second" from CFG into milliseconds per tick.
// Example: 20 fps → 1000/20 = 50 ms.
func DerivePeriodMs(names []string, fallback int64) int64 {
	if fallback < 1 {
		fallback = DefaultPeriodMs
	}
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
		return fallback
	}
	ms := int64(math.Round(1000.0 / bestFPS))
	if ms < 1 {
		return 1
	}
	return ms
}

// Run keeps publishing ticks until the program shuts down (ctx cancelled).
func (p *Publisher) Run(ctx context.Context) {
	if p == nil || p.bank == nil || p.emit == nil {
		return
	}
	log.Printf("[aligner] publisher on (period=%dms, no wait, head=MinNewest)", p.PeriodMs())

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
	period := p.periodMs
	gen := p.gen
	p.mu.Unlock()

	if period < 1 {
		period = DefaultPeriodMs
	}

	names := p.bank.Expected()
	if len(names) == 0 {
		return false
	}

	if derived := DerivePeriodMs(names, period); derived != period {
		p.SetPeriodMs(derived)
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
		log.Printf("[aligner] charts start at tick=%d (%s)",
			tick, time.UnixMilli(tick).UTC().Format(time.RFC3339Nano))
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

	// Accept stamps within ±half period (25 fps → ±20 ms). Exact-only matching
	// left ~half the ticks empty for Typhoon-style 39/40/41 ms jitter.
	half := period / 2
	present, missing := p.bank.TakeTickWindow(tick, half)
	if len(present) == 0 {
		// Ruler mark with no nearby sample — skip; do not emit a chart hole.
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
	p.bank.PruneBefore(tick)

	p.mu.Lock()
	if p.gen == gen {
		p.nextTick = tick + period
	}
	p.mu.Unlock()
	return true
}
