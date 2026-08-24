package aligner

import (
	"sort"
	"sync"
	"time"

	"pdc/parser"
)

// WaitMode selects how the concentrator wait timer is started.
type WaitMode int

const (
	// WaitRelative starts the clock when the first PMU arrives for a timestamp.
	// Does not require a UTC-synced PDC clock (IEEE C37.118.2 relative wait).
	WaitRelative WaitMode = iota
	// WaitAbsolute starts the clock at the measurement timestamp itself
	// (timestamp + wait). Requires a UTC-synced PDC clock.
	WaitAbsolute
)

// BucketKeyMode selects how frames are grouped into alignment buckets.
type BucketKeyMode int

const (
	// BucketSOC groups by raw PMU measurement timestamp (needs GPS-synced PMUs).
	BucketSOC BucketKeyMode = iota
	// BucketCorrected groups by SOC + per-PMU clock offset (works when PMU clocks drift).
	BucketCorrected
	// BucketReceive groups by PDC receive time (lab / unsynced free-running clocks).
	BucketReceive
)

// Config controls in-memory multi-PMU time alignment.
type Config struct {
	Enabled bool
	Mode    WaitMode
	// BucketKey selects the time axis used to match frames across PMUs.
	BucketKey BucketKeyMode
	// OffsetFunc returns estimated (receive − SOC) for a PMU; used by BucketCorrected.
	OffsetFunc func(pmu string) time.Duration
	// Quantize snaps bucket keys to this grid (e.g. 20ms @ 50 FPS, ~33ms @ 30 FPS).
	Quantize time.Duration
	// Wait is the cutoff after which a partial set is emitted (missing = absent).
	Wait time.Duration
	// BufferDepth is per-PMU history length (MatPDC "n") and max open timestamp buckets.
	BufferDepth int
	// ActiveTTL: PMUs with no frames for this long leave the expected set.
	ActiveTTL time.Duration
	// EmittedTTL: how long to remember flushed timestamps so late frames cannot reopen them.
	EmittedTTL time.Duration
}

// AlignedSet is one time-aligned aggregation across PMUs for a shared timestamp.
type AlignedSet struct {
	Timestamp time.Time
	Present   map[string]parser.Reading
	Missing   []string
	Complete  bool
	Mode      WaitMode
	Waited    time.Duration
	Forced    bool // true when flushed due to buffer depth / shutdown
}

// EmitFunc is called once per aligned (or passthrough) set.
type EmitFunc func(AlignedSet)

// Concentrator is a MatPDC-style time aligner.
//
// Each PMU keeps a depth-n history buffer. Frames are indexed by measurement
// timestamp (millisecond). Relative wait starts when the first PMU contributes
// a new timestamp. The set is emitted when all active PMUs are present or the
// wait expires. Emitted timestamps are tombstoned so late arrivals cannot open
// a new singleton bucket. Sets are released in timestamp order.
type Concentrator struct {
	cfg  Config
	emit EmitFunc

	mu       sync.Mutex
	active   map[string]time.Time // last Push wall time
	buffers  map[string]*pmuBuf   // per-PMU history
	buckets  map[int64]*timeBucket
	emitted  map[int64]time.Time // tombstones: key → wall time marked emitted
	closed   bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

type pmuBuf struct {
	order []int64 // oldest → newest keys
	byTS  map[int64]parser.Reading
}

func newPMUBuf() *pmuBuf {
	return &pmuBuf{byTS: make(map[int64]parser.Reading)}
}

func (b *pmuBuf) put(key int64, r parser.Reading, depth int) {
	if _, exists := b.byTS[key]; !exists {
		b.order = append(b.order, key)
	}
	b.byTS[key] = r
	for depth > 0 && len(b.order) > depth {
		old := b.order[0]
		b.order = b.order[1:]
		delete(b.byTS, old)
	}
}

func (b *pmuBuf) get(key int64) (parser.Reading, bool) {
	r, ok := b.byTS[key]
	return r, ok
}

type timeBucket struct {
	ts           time.Time
	readings     map[string]parser.Reading
	firstArrival time.Time
	timer        *time.Timer
	ready        bool // wait expired or force-flush requested
	forced       bool // ready due to depth/shutdown rather than wait/complete
	emitted      bool
}

// NewConcentrator builds a time aligner. emit must be non-nil.
func NewConcentrator(cfg Config, emit EmitFunc) *Concentrator {
	if emit == nil {
		panic("aligner: EmitFunc is required")
	}
	if cfg.Wait <= 0 {
		cfg.Wait = 200 * time.Millisecond
	}
	if cfg.BufferDepth <= 0 {
		cfg.BufferDepth = 50
	}
	if cfg.ActiveTTL <= 0 {
		cfg.ActiveTTL = 5 * time.Second
	}
	if cfg.EmittedTTL <= 0 {
		cfg.EmittedTTL = 2 * time.Second
	}
	if cfg.Quantize <= 0 {
		cfg.Quantize = 20 * time.Millisecond
	}
	return &Concentrator{
		cfg:     cfg,
		emit:    emit,
		active:  make(map[string]time.Time),
		buffers: make(map[string]*pmuBuf),
		buckets: make(map[int64]*timeBucket),
		emitted: make(map[int64]time.Time),
		stopCh:  make(chan struct{}),
	}
}

// Push stores a reading in the PMU history and may emit aligned sets.
func (c *Concentrator) Push(r parser.Reading) {
	if c == nil {
		return
	}
	if r.Timestamp.IsZero() {
		c.emitPassthrough(r)
		return
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}

	now := time.Now()
	rawKey := c.bucketKey(r, now)
	key := c.resolveBucketKeyLocked(rawKey)

	// Always keep per-PMU history (MatPDC buffer depth n).
	buf := c.buffers[r.PMUName]
	if buf == nil {
		buf = newPMUBuf()
		c.buffers[r.PMUName] = buf
	}
	buf.put(key, r, c.cfg.BufferDepth)

	c.active[r.PMUName] = now
	c.pruneActiveLocked(now)
	c.pruneEmittedLocked(now)
	expected := c.expectedNamesLocked(now)

	if !c.cfg.Enabled || len(expected) <= 1 {
		c.markEmittedLocked(key, now)
		c.mu.Unlock()
		c.emitPassthrough(r)
		return
	}

	// Late frame for an already-released timestamp: keep in history, do not reopen.
	if _, done := c.emitted[key]; done {
		c.mu.Unlock()
		return
	}

	b := c.buckets[key]
	if b == nil {
		b = &timeBucket{
			ts:           time.UnixMilli(key).UTC(),
			readings:     make(map[string]parser.Reading, len(expected)),
			firstArrival: now,
		}
		c.hydrateBucketLocked(b, key, expected)
		b.readings[r.PMUName] = r
		c.buckets[key] = b

		wait := c.waitDurationLocked(b, now)
		if wait <= 0 {
			b.ready = true
		} else {
			b.timer = time.AfterFunc(wait, func() { c.onWaitExpired(key) })
		}
	} else if !b.emitted {
		b.readings[r.PMUName] = r
		c.hydrateBucketLocked(b, key, expected)
	}

	if b.emitted {
		c.mu.Unlock()
		return
	}

	if c.isCompleteLocked(b, expected) {
		b.ready = true
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
	}

	c.enforceDepthLocked(now)
	toEmit := c.drainReadyLocked(now)
	c.mu.Unlock()

	for _, set := range toEmit {
		c.emit(set)
	}
}

// Close flushes remaining buckets in order and stops timers.
func (c *Concentrator) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.stopCh)

	now := time.Now()
	for _, b := range c.buckets {
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		if !b.emitted {
			b.ready = true
			b.forced = true
		}
	}
	toEmit := c.drainReadyLocked(now)
	c.mu.Unlock()

	for _, set := range toEmit {
		c.emit(set)
	}
	c.wg.Wait()
}

func (c *Concentrator) emitPassthrough(r parser.Reading) {
	c.emit(AlignedSet{
		Timestamp: r.Timestamp,
		Present:   map[string]parser.Reading{r.PMUName: r},
		Missing:   nil,
		Complete:  true,
		Mode:      c.cfg.Mode,
		Waited:    0,
	})
}

// bucketKey maps a reading onto the alignment grid.
func (c *Concentrator) bucketKey(r parser.Reading, now time.Time) int64 {
	var t time.Time
	switch c.cfg.BucketKey {
	case BucketReceive:
		if r.Trace.ReceivedAtUnixNano > 0 {
			t = time.Unix(0, r.Trace.ReceivedAtUnixNano)
		} else {
			t = now
		}
	case BucketCorrected:
		t = r.Timestamp
		if c.cfg.OffsetFunc != nil {
			if off := c.cfg.OffsetFunc(r.PMUName); off != 0 {
				t = t.Add(off)
			} else if r.Trace.ReceivedAtUnixNano > 0 {
				// First frames: no EMA yet — fall back to receive time.
				t = time.Unix(0, r.Trace.ReceivedAtUnixNano)
			}
		}
	default: // BucketSOC
		t = r.Timestamp
	}
	return quantize(t, c.cfg.Quantize).UnixMilli()
}

func quantize(t time.Time, step time.Duration) time.Time {
	if step <= 0 || t.IsZero() {
		return t.UTC()
	}
	ns := t.UnixNano()
	q := int64(step)
	return time.Unix(0, (ns/q)*q).UTC()
}

// matchWindowMs is how far a frame may snap into an already-open bucket.
func (c *Concentrator) matchWindowMs() int64 {
	w := c.cfg.Wait
	if c.cfg.Quantize > w {
		w = c.cfg.Quantize
	}
	if w <= 0 {
		return 0
	}
	return w.Milliseconds()
}

// resolveBucketKeyLocked snaps corrected/receive frames onto the nearest open
// bucket within the wait window so staggered arrivals still align.
func (c *Concentrator) resolveBucketKeyLocked(rawKey int64) int64 {
	if c.cfg.BucketKey == BucketSOC {
		return rawKey
	}
	window := c.matchWindowMs()
	if window <= 0 {
		return rawKey
	}

	bestKey := rawKey
	bestDist := window + 1
	foundOpen := false
	for k, b := range c.buckets {
		if b == nil || b.emitted {
			continue
		}
		d := k - rawKey
		if d < 0 {
			d = -d
		}
		if d <= window && d < bestDist {
			bestKey, bestDist, foundOpen = k, d, true
		}
	}
	if foundOpen {
		return bestKey
	}
	return rawKey
}

// hydrateBucketLocked pulls peer frames from history whose keys fall in the match window.
func (c *Concentrator) hydrateBucketLocked(b *timeBucket, key int64, expected []string) {
	if b == nil {
		return
	}
	window := c.matchWindowMs()
	for _, name := range expected {
		if _, ok := b.readings[name]; ok {
			continue
		}
		rr := c.buffers[name]
		if rr == nil {
			continue
		}
		if got, ok := rr.get(key); ok {
			b.readings[name] = got
			continue
		}
		if window <= 0 {
			continue
		}
		var (
			best    parser.Reading
			bestDist int64 = window + 1
			found   bool
		)
		for k, got := range rr.byTS {
			d := k - key
			if d < 0 {
				d = -d
			}
			if d <= window && d < bestDist {
				best, bestDist, found = got, d, true
			}
		}
		if found {
			b.readings[name] = best
		}
	}
}

func (c *Concentrator) onWaitExpired(key int64) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	b := c.buckets[key]
	if b == nil || b.emitted {
		c.mu.Unlock()
		return
	}
	b.ready = true
	b.timer = nil
	toEmit := c.drainReadyLocked(time.Now())
	c.mu.Unlock()
	for _, set := range toEmit {
		c.emit(set)
	}
}

func (c *Concentrator) waitDurationLocked(b *timeBucket, now time.Time) time.Duration {
	switch c.cfg.Mode {
	case WaitAbsolute:
		return b.ts.Add(c.cfg.Wait).Sub(now)
	default:
		return b.firstArrival.Add(c.cfg.Wait).Sub(now)
	}
}

func (c *Concentrator) pruneActiveLocked(now time.Time) {
	for name, last := range c.active {
		if now.Sub(last) > c.cfg.ActiveTTL {
			delete(c.active, name)
		}
	}
}

func (c *Concentrator) pruneEmittedLocked(now time.Time) {
	for key, at := range c.emitted {
		if now.Sub(at) > c.cfg.EmittedTTL {
			delete(c.emitted, key)
		}
	}
}

func (c *Concentrator) markEmittedLocked(key int64, now time.Time) {
	c.emitted[key] = now
}

func (c *Concentrator) expectedNamesLocked(now time.Time) []string {
	names := make([]string, 0, len(c.active))
	for name, last := range c.active {
		if now.Sub(last) <= c.cfg.ActiveTTL {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (c *Concentrator) isCompleteLocked(b *timeBucket, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	for _, name := range expected {
		if _, ok := b.readings[name]; !ok {
			return false
		}
	}
	return true
}

func (c *Concentrator) sortedBucketKeysLocked() []int64 {
	keys := make([]int64, 0, len(c.buckets))
	for k := range c.buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// drainReadyLocked emits every leading ready bucket in timestamp order.
// A newer complete bucket is held until older open buckets are released.
func (c *Concentrator) drainReadyLocked(now time.Time) []AlignedSet {
	c.pruneActiveLocked(now)
	expected := c.expectedNamesLocked(now)
	var out []AlignedSet

	for {
		keys := c.sortedBucketKeysLocked()
		if len(keys) == 0 {
			break
		}
		oldest := keys[0]
		b := c.buckets[oldest]
		if b == nil || b.emitted {
			delete(c.buckets, oldest)
			continue
		}
		complete := c.isCompleteLocked(b, expected)
		if complete {
			b.ready = true
			if b.timer != nil {
				b.timer.Stop()
				b.timer = nil
			}
		}
		if !b.ready {
			break // preserve time order
		}
		set := c.takeBucketLocked(oldest, now, b.forced)
		if set != nil {
			out = append(out, *set)
		}
	}
	return out
}

func (c *Concentrator) takeBucketLocked(key int64, now time.Time, forced bool) *AlignedSet {
	b := c.buckets[key]
	if b == nil || b.emitted {
		return nil
	}
	expected := c.expectedNamesLocked(now)
	// Final hydrate from histories before emit.
	c.hydrateBucketLocked(b, key, expected)

	present := make(map[string]parser.Reading, len(b.readings))
	for name, r := range b.readings {
		present[name] = r
	}
	missing := make([]string, 0)
	for _, name := range expected {
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	waited := now.Sub(b.firstArrival)
	if waited < 0 {
		waited = 0
	}

	b.emitted = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	delete(c.buckets, key)
	c.markEmittedLocked(key, now)

	return &AlignedSet{
		Timestamp: b.ts,
		Present:   present,
		Missing:   missing,
		Complete:  len(missing) == 0 && len(expected) > 0,
		Mode:      c.cfg.Mode,
		Waited:    waited,
		Forced:    forced,
	}
}

func (c *Concentrator) enforceDepthLocked(now time.Time) {
	if len(c.buckets) <= c.cfg.BufferDepth {
		return
	}
	keys := c.sortedBucketKeysLocked()
	for len(keys) > c.cfg.BufferDepth {
		oldest := keys[0]
		keys = keys[1:]
		b := c.buckets[oldest]
		if b == nil || b.emitted {
			continue
		}
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		b.ready = true
		b.forced = true
	}
}

// Stats returns open-bucket and active-PMU counts (for monitoring).
func (c *Concentrator) Stats() (openBuckets, activePMUs int) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buckets), len(c.active)
}
