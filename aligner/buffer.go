package aligner

// buffer.go — one notebook per PMU, keyed by measurement time.
//
// When a frame arrives we write it under its timestamp (ms).
// The publisher later asks: "what does this PMU have at time T?"

import (
	"sort"
	"sync"
	"time"

	"pdc/parser"
)

// DefaultCapacity is how many timestamps one PMU may keep in RAM.
// Think of it as a short sticky-note pad: when it is full, the oldest note is thrown away.
const DefaultCapacity = 200

// pmuBuffer is one PMU's notebook: each page is keyed by measurement time (ms).
// recent keeps the last N frames in arrival order for the sheets dumps
// (TakeTick removes from byTS for the publisher, but must not erase inspection history).
type pmuBuffer struct {
	byTS     map[int64]parser.Reading
	recent   []parser.Reading
	capacity int
}

func newPMUBuffer(capacity int) *pmuBuffer {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &pmuBuffer{
		byTS:     make(map[int64]parser.Reading, capacity),
		recent:   make([]parser.Reading, 0, capacity),
		capacity: capacity,
	}
}

// put stores a reading. If the table is full and this is a new timestamp,
// the oldest timestamp is dropped to make room.
func (b *pmuBuffer) put(ts int64, r parser.Reading) {
	if ts == 0 {
		return
	}
	if _, exists := b.byTS[ts]; !exists && len(b.byTS) >= b.capacity {
		if oldest, ok := b.oldestTS(); ok {
			delete(b.byTS, oldest)
		}
	}
	b.byTS[ts] = r

	b.recent = append(b.recent, r)
	if len(b.recent) > b.capacity {
		b.recent = append([]parser.Reading(nil), b.recent[len(b.recent)-b.capacity:]...)
	}
}

func (b *pmuBuffer) oldestTS() (int64, bool) {
	if len(b.byTS) == 0 {
		return 0, false
	}
	first := true
	var min int64
	for ts := range b.byTS {
		if first || ts < min {
			min = ts
			first = false
		}
	}
	return min, true
}

func (b *pmuBuffer) newestTS() (int64, bool) {
	if len(b.byTS) == 0 {
		return 0, false
	}
	first := true
	var max int64
	for ts := range b.byTS {
		if first || ts > max {
			max = ts
			first = false
		}
	}
	return max, true
}

// takeNearest removes and returns the sample at target, or the closest sample
// within ±halfWindow ms (ties → earlier timestamp). halfWindow < 0 means exact only.
func (b *pmuBuffer) takeNearest(target, halfWindow int64) (parser.Reading, bool) {
	if b == nil || len(b.byTS) == 0 {
		return parser.Reading{}, false
	}
	if halfWindow < 0 {
		halfWindow = 0
	}
	if r, ok := b.byTS[target]; ok {
		delete(b.byTS, target)
		return r, true
	}
	if halfWindow == 0 {
		return parser.Reading{}, false
	}
	var bestTS, bestDist int64
	found := false
	for ts := range b.byTS {
		d := ts - target
		if d < 0 {
			d = -d
		}
		if d > halfWindow {
			continue
		}
		if !found || d < bestDist || (d == bestDist && ts < bestTS) {
			bestTS, bestDist, found = ts, d, true
		}
	}
	if !found {
		return parser.Reading{}, false
	}
	r := b.byTS[bestTS]
	delete(b.byTS, bestTS)
	return r, true
}

func (b *pmuBuffer) sortedKeys() []int64 {
	keys := make([]int64, 0, len(b.byTS))
	for ts := range b.byTS {
		keys = append(keys, ts)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// Bank is the shelf of notebooks — one notebook per PMU we care about.
//
// New readings are written into the notebooks (Ingest).
// The Publisher walks a time ruler and pulls one row at a time (TakeTick).
type Bank struct {
	mu       sync.Mutex
	capacity int
	buffers  map[string]*pmuBuffer
	expected []string // which PMUs belong on the shelf

	// onReset runs after SetExpected (add/remove PMU) so charts can start over.
	onReset func()
}

// NewBank creates an empty bank. Call SetExpected when PMUs connect.
func NewBank(capacity int) *Bank {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &Bank{
		capacity: capacity,
		buffers:  make(map[string]*pmuBuffer),
	}
}

// Capacity is how many timestamps each PMU table may hold.
func (b *Bank) Capacity() int {
	if b == nil {
		return DefaultCapacity
	}
	return b.capacity
}

// OnReset registers a callback fired after SetExpected (PMU add/remove).
// Multiple registrations chain in order.
func (b *Bank) OnReset(fn func()) {
	if b == nil || fn == nil {
		return
	}
	b.mu.Lock()
	prev := b.onReset
	b.onReset = func() {
		if prev != nil {
			prev()
		}
		fn()
	}
	b.mu.Unlock()
}

// Expected returns a copy of the active PMU names.
func (b *Bank) Expected() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.expected...)
}

// SetExpected rebuilds buffers for the active PMU set (add / delete / reconnect).
func (b *Bank) SetExpected(names []string) {
	b.mu.Lock()

	next := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		next = append(next, name)
	}
	sort.Strings(next)

	buffers := make(map[string]*pmuBuffer, len(next))
	for _, name := range next {
		buffers[name] = newPMUBuffer(b.capacity)
	}
	b.expected = next
	b.buffers = buffers
	cb := b.onReset
	b.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// MinOldest = oldest timestamp sitting in any PMU buffer right now.
// We use this to pick where the chart ruler should start.
func (b *Bank) MinOldest() (int64, bool) {
	if b == nil {
		return 0, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.minOldestLocked()
}

func (b *Bank) minOldestLocked() (int64, bool) {
	var candidate int64
	have := false
	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil {
			continue
		}
		oldest, ok := buf.oldestTS()
		if !ok {
			continue
		}
		if !have || oldest < candidate {
			candidate = oldest
			have = true
		}
	}
	return candidate, have
}

// MaxNewest = newest timestamp sitting in any PMU buffer right now.
func (b *Bank) MaxNewest() (int64, bool) {
	if b == nil {
		return 0, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var candidate int64
	have := false
	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil {
			continue
		}
		newest, ok := buf.newestTS()
		if !ok {
			continue
		}
		if !have || newest > candidate {
			candidate = newest
			have = true
		}
	}
	return candidate, have
}

// MinNewest = earliest "newest" among ALL expected PMUs.
//
// Every expected PMU must still have at least one sample in the work-set.
// If we skipped empty buffers, a drained PMU would let the ruler race ahead
// on the others and paint permanent holes on the chart (disconnected lines).
func (b *Bank) MinNewest() (int64, bool) {
	if b == nil {
		return 0, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.expected) == 0 {
		return 0, false
	}
	var candidate int64
	have := false
	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil || len(buf.byTS) == 0 {
			return 0, false // someone is caught up / empty — wait for their next frame
		}
		newest, ok := buf.newestTS()
		if !ok {
			return 0, false
		}
		if !have || newest < candidate {
			candidate = newest
			have = true
		}
	}
	return candidate, have
}

// TakeTick looks at one time mark on the ruler (exact timestamp only).
// Prefer TakeTickWindow when PMU stamps are slightly off the period grid.
func (b *Bank) TakeTick(ts int64) (present map[string]parser.Reading, missing []string) {
	return b.TakeTickWindow(ts, 0)
}

// TakeTickWindow is like TakeTick but each PMU may contribute its nearest sample
// within ±halfWindow ms of ts (fixes 25 fps devices whose stamps jitter 1 ms
// off the 40 ms grid, which used to paint permanent chart holes).
func (b *Bank) TakeTickWindow(ts, halfWindow int64) (present map[string]parser.Reading, missing []string) {
	present = make(map[string]parser.Reading)
	if b == nil || ts == 0 {
		return present, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil {
			missing = append(missing, name)
			continue
		}
		if r, ok := buf.takeNearest(ts, halfWindow); ok {
			present[name] = r
			continue
		}
		missing = append(missing, name)
	}
	return present, missing
}

// PeekTick is like TakeTick but does not remove samples (used while waiting).
func (b *Bank) PeekTick(ts int64) (present map[string]parser.Reading, missing []string) {
	present = make(map[string]parser.Reading)
	if b == nil || ts == 0 {
		return present, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil {
			missing = append(missing, name)
			continue
		}
		if r, ok := buf.byTS[ts]; ok {
			present[name] = r
			continue
		}
		missing = append(missing, name)
	}
	return present, missing
}

// PruneBefore drops work-set samples older than ts (keep-tail hygiene after a push).
func (b *Bank) PruneBefore(ts int64) {
	if b == nil || ts == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, name := range b.expected {
		buf := b.buffers[name]
		if buf == nil {
			continue
		}
		for k := range buf.byTS {
			if k < ts {
				delete(buf.byTS, k)
			}
		}
	}
}

// Ingest drops one quality-checked reading into that PMU's timestamp table.
func (b *Bank) Ingest(r parser.Reading) {
	if b == nil || r.PMUName == "" {
		return
	}
	ts := TimestampKey(r)
	if ts == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	buf, ok := b.buffers[r.PMUName]
	if !ok {
		// Unknown / not in expected set — ignore for alignment.
		return
	}
	buf.put(ts, r)
}

// --- Snapshot types (for the HTTP dump — so you can SEE the buffers) ---

// SlotView is one cell in a PMU's table (one timestamp).
// Reading is the full parsed frame stored in the buffer, not a short preview.
type SlotView struct {
	TSMs    int64          `json:"tsMs"`
	TimeUTC string         `json:"timeUtc"`
	Reading parser.Reading `json:"reading"`
}

// PMUBufferView is the whole table for one PMU.
type PMUBufferView struct {
	Name     string     `json:"name"`
	Count    int        `json:"count"`
	Capacity int        `json:"capacity"`
	OldestMs int64      `json:"oldestMs,omitempty"`
	NewestMs int64      `json:"newestMs,omitempty"`
	Slots    []SlotView `json:"slots"`
}

// BankSnapshot is what /conversation/aligner-buffers returns.
type BankSnapshot struct {
	AtUTC      string          `json:"atUtc"`
	Capacity   int             `json:"capacityPerPMU"`
	Expected   []string        `json:"expected"`
	Note       string          `json:"note"`
	PMUs       []PMUBufferView `json:"pmus"`
	// Hint: next tick the publisher will emit (min oldest across PMUs).
	CandidatePublishMs int64 `json:"candidatePublishMs,omitempty"`
}

// Snapshot copies buffer contents for debugging / the dashboard dump page.
// Slots are the last N frames by ARRIVAL (recent ring), so receive-time windows
// across PMUs are comparable. The publisher's time-keyed work set (byTS) is separate.
func (b *Bank) Snapshot() BankSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := BankSnapshot{
		AtUTC:    time.Now().UTC().Format(time.RFC3339Nano),
		Capacity: b.capacity,
		Expected: append([]string(nil), b.expected...),
		Note:     "slots = last N by arrival; publish nearest±half-period (skip empty ticks)",
		PMUs:     make([]PMUBufferView, 0, len(b.expected)),
	}

	candidate, haveCandidate := b.minOldestLocked()

	for _, name := range b.expected {
		buf := b.buffers[name]
		view := PMUBufferView{
			Name:     name,
			Capacity: b.capacity,
			Slots:    nil,
		}
		if buf == nil {
			out.PMUs = append(out.PMUs, view)
			continue
		}
		view.Count = len(buf.recent)
		view.Slots = make([]SlotView, 0, len(buf.recent))
		var oldestMs, newestMs int64
		for i, r := range buf.recent {
			ts := TimestampKey(r)
			if i == 0 || ts < oldestMs {
				oldestMs = ts
			}
			if i == 0 || ts > newestMs {
				newestMs = ts
			}
			view.Slots = append(view.Slots, SlotView{
				TSMs:    ts,
				TimeUTC: time.UnixMilli(ts).UTC().Format(time.RFC3339Nano),
				Reading: r,
			})
		}
		if view.Count > 0 {
			view.OldestMs = oldestMs
			view.NewestMs = newestMs
		}
		out.PMUs = append(out.PMUs, view)
	}

	if haveCandidate {
		out.CandidatePublishMs = candidate
	}
	return out
}
