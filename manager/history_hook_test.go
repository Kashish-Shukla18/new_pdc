package manager

import (
	"sync"
	"testing"
)

type fakeHistory struct {
	mu   sync.Mutex
	calls []string
}

func (f *fakeHistory) NoteBecameLive(pmu string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "became_live:"+pmu)
}
func (f *fakeHistory) NoteLeftLive(pmu string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "left_live:"+pmu)
}
func (f *fakeHistory) NoteOperatorStart(pmu string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "operator_start:"+pmu)
}
func (f *fakeHistory) NoteOperatorStop(pmu string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "operator_stop:"+pmu)
}
func (f *fakeHistory) NoteProfile(pmu string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "profile:"+pmu)
}

func TestHistoryHooksOnLiveTransitions(t *testing.T) {
	h := &fakeHistory{}
	m := NewPMUManager(nil)
	m.SetHistoryHooks(h)
	m.receivers["a"] = &pmuRun{done: make(chan struct{})}
	close(m.receivers["a"].done)

	m.markLive("a")
	m.markNotLive("a")

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) < 2 || h.calls[0] != "became_live:a" || h.calls[1] != "left_live:a" {
		t.Fatalf("calls=%v", h.calls)
	}
}
