package history

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"pdc/output/postgres"
	"pdc/parser"
)

type fakeSink struct {
	mu     sync.Mutex
	events []postgres.EventRow
	frames []postgres.FrameRow
	cfgs   []postgres.CfgVersion
	nextID int64
	fail   bool
}

func (f *fakeSink) EnsureCfgVersion(_ context.Context, v postgres.CfgVersion) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, context.DeadlineExceeded
	}
	for i, c := range f.cfgs {
		if c.PMUID == v.PMUID && c.LayoutHash == v.LayoutHash {
			return int64(i + 1), nil
		}
	}
	f.cfgs = append(f.cfgs, v)
	f.nextID++
	return f.nextID, nil
}

func (f *fakeSink) EnqueueEvent(row postgres.EventRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, row)
}

func (f *fakeSink) EnqueueFrame(row postgres.FrameRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, row)
}

func (f *fakeSink) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, e := range f.events {
		out[i] = e.Type
	}
	return out
}

func TestLayoutHashStable(t *testing.T) {
	p := parser.Profile{
		Station: "STN", IDCode: 1, TimeBase: 1_000_000,
		Phnmr: 1, DataRate: 50, Channels: []string{"VA"},
		PhFloat: true, FnomHz: 50,
	}
	a := LayoutHash(p)
	b := LayoutHash(p)
	if a == "" || a != b {
		t.Fatalf("hash unstable %q %q", a, b)
	}
	p.DataRate = 60
	if LayoutHash(p) == a {
		t.Fatal("hash should change when data rate changes")
	}
}

func TestNoteQualityEdgesOnly(t *testing.T) {
	f := &fakeSink{}
	r := NewRecorder(f)
	r.NoteQuality("p1", false, "skew")
	r.NoteQuality("p1", false, "skew")
	r.NoteQuality("p1", false, "unsync")
	r.NoteQuality("p1", true, "")
	r.NoteQuality("p1", true, "")
	got := f.eventTypes()
	want := []string{"quality_degraded", "quality_recovered"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events=%v want %v", got, want)
	}
}

func TestNoteProfilePersistsAndCfgChanged(t *testing.T) {
	f := &fakeSink{}
	r := NewRecorder(f)
	parser.SetProfile("p1", parser.Profile{
		Station: "A", IDCode: 2, TimeBase: 1_000_000,
		Phnmr: 1, DataRate: 50, Channels: []string{"VA"},
	})
	t.Cleanup(func() { parser.SetProfile("p1", parser.Profile{}) })

	r.NoteProfile("p1")
	if f.nextID != 1 || r.CfgID("p1") != 1 {
		t.Fatalf("cfg_id=%d next=%d", r.CfgID("p1"), f.nextID)
	}
	if len(f.eventTypes()) != 0 {
		t.Fatalf("no cfg_changed on first profile, got %v", f.eventTypes())
	}

	parser.SetProfile("p1", parser.Profile{
		Station: "A", IDCode: 2, TimeBase: 1_000_000,
		Phnmr: 1, DataRate: 60, Channels: []string{"VA"},
	})
	r.NoteProfile("p1")
	types := f.eventTypes()
	if len(types) != 1 || types[0] != "cfg_changed" {
		t.Fatalf("events=%v", types)
	}
	if r.CfgID("p1") != 2 {
		t.Fatalf("cfg_id=%d", r.CfgID("p1"))
	}
}

func TestNoteProfilePersistFailed(t *testing.T) {
	f := &fakeSink{fail: true}
	r := NewRecorder(f)
	parser.SetProfile("p2", parser.Profile{Station: "B", Channels: []string{"VA"}, Phnmr: 1})
	t.Cleanup(func() { parser.SetProfile("p2", parser.Profile{}) })
	r.NoteProfile("p2")
	types := f.eventTypes()
	if len(types) != 1 || types[0] != "cfg_persist_failed" {
		t.Fatalf("events=%v", types)
	}
}

func TestLifecycleEvents(t *testing.T) {
	f := &fakeSink{}
	r := NewRecorder(f)
	parser.SetProfile("p3", parser.Profile{Station: "C", Phnmr: 1, Channels: []string{"VA"}, DataRate: 30})
	t.Cleanup(func() { parser.SetProfile("p3", parser.Profile{}) })

	r.NoteOperatorStart("p3")
	r.NoteBecameLive("p3")
	r.NoteLeftLive("p3")
	r.NoteOperatorStop("p3")
	got := f.eventTypes()
	// became_live also NoteProfile (no event on first)
	wantPrefix := []string{"operator_start", "became_live", "left_live", "operator_stop"}
	if len(got) < 4 {
		t.Fatalf("events=%v", got)
	}
	for i, w := range wantPrefix {
		if got[i] != w {
			t.Fatalf("events[%d]=%s want %s full=%v", i, got[i], w, got)
		}
	}
	if r.CfgID("p3") == 0 {
		t.Fatal("cfg should be cached after became_live")
	}
}

func TestNoteFrameUsesCfgIDAndQualityFlag(t *testing.T) {
	f := &fakeSink{}
	r := NewRecorder(f)
	parser.SetProfile("p4", parser.Profile{Station: "D", Phnmr: 1, Channels: []string{"VA"}, DataRate: 50})
	t.Cleanup(func() { parser.SetProfile("p4", parser.Profile{}) })
	r.NoteProfile("p4")
	cfgID := r.CfgID("p4")
	if cfgID == 0 {
		t.Fatal("expected cfg id")
	}

	okReading := parser.Reading{PMUName: "p4", Frequency: 50, Timestamp: time.Unix(1, 0).UTC()}
	badReading := parser.Reading{PMUName: "p4", Frequency: 47.2, Timestamp: time.Unix(2, 0).UTC()}

	r.NoteFrame(okReading, true, "")
	r.NoteFrame(badReading, false, "clock skew")

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) != 2 {
		t.Fatalf("frames=%d", len(f.frames))
	}
	if !f.frames[0].QualityOK || f.frames[0].CfgID != cfgID || f.frames[0].Freq < 49.9 || f.frames[0].Freq > 50.1 {
		t.Fatalf("ok frame=%+v", f.frames[0])
	}
	if f.frames[1].QualityOK || f.frames[1].RejectReason != "clock skew" || f.frames[1].Freq < 47.1 || f.frames[1].Freq > 47.3 {
		t.Fatalf("bad frame=%+v", f.frames[1])
	}
}

// liveDropIndependent documents phase-4 contract: history still gets rejected
// frames even when the live path would drop them.
func TestHistoryKeepsRejectedWhenLiveWouldDrop(t *testing.T) {
	f := &fakeSink{}
	r := NewRecorder(f)
	dropQualityRejected := true // live-path knob

	reading := parser.Reading{PMUName: "p5", Frequency: 46, Timestamp: time.Unix(3, 0).UTC()}
	qerr := errors.New("STAT unsynchronized")

	qualityOK := qerr == nil
	rejectReason := ""
	if !qualityOK {
		rejectReason = qerr.Error()
	}
	r.NoteQuality("p5", qualityOK, rejectReason)
	r.NoteFrame(reading, qualityOK, rejectReason)

	continueLive := qualityOK || !dropQualityRejected
	if continueLive {
		t.Fatal("live path should stop when dropQualityRejected and qerr set")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) != 1 || f.frames[0].QualityOK {
		t.Fatalf("history must keep rejected frame: %+v", f.frames)
	}
	if len(f.events) != 1 || f.events[0].Type != "quality_degraded" {
		t.Fatalf("events=%+v", f.events)
	}
}
