package manager

import (
	"testing"
	"time"
)

func TestMarkLiveNotLiveUpdatesAlignerHook(t *testing.T) {
	m := NewPMUManager(func(string, []byte, time.Time) {})
	var got [][]string
	m.SetAlignerHook(func(live []string) {
		cp := append([]string(nil), live...)
		got = append(got, cp)
	})

	// Simulate configured receiver without going through StartPMU network path.
	m.mu.Lock()
	m.receivers["a"] = &pmuRun{cancel: func() {}, done: make(chan struct{})}
	m.receivers["b"] = &pmuRun{cancel: func() {}, done: make(chan struct{})}
	m.receivers["c"] = &pmuRun{cancel: func() {}, done: make(chan struct{})}
	m.mu.Unlock()

	m.markLive("a")
	m.markLive("b")
	m.markLive("c")
	if len(m.LiveNames()) != 3 {
		t.Fatalf("live=%v want 3", m.LiveNames())
	}

	m.markNotLive("c")
	live := m.LiveNames()
	if len(live) != 2 || live[0] != "a" || live[1] != "b" {
		t.Fatalf("after c disconnect live=%v want [a b]", live)
	}
	cfg := m.ConfiguredNames()
	if len(cfg) != 3 {
		t.Fatalf("configured should still be 3, got %v", cfg)
	}

	// Idempotent.
	m.markNotLive("c")
	if len(m.LiveNames()) != 2 {
		t.Fatal("second markNotLive should be no-op")
	}

	m.markLive("c")
	if len(m.LiveNames()) != 3 {
		t.Fatalf("reconnect should restore live, got %v", m.LiveNames())
	}

	if len(got) < 4 {
		t.Fatalf("aligner hook should have fired on live changes, got %d calls: %v", len(got), got)
	}
	last := got[len(got)-1]
	if len(last) != 3 {
		t.Fatalf("last hook payload=%v want 3 live", last)
	}
}

func TestMarkLiveIgnoredIfNotConfigured(t *testing.T) {
	m := NewPMUManager(nil)
	called := false
	m.SetAlignerHook(func([]string) { called = true })
	m.markLive("ghost")
	if called {
		t.Fatal("markLive must not notify when PMU is not configured")
	}
	if len(m.LiveNames()) != 0 {
		t.Fatal("ghost must not enter live set")
	}
}
