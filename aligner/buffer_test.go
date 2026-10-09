package aligner

import (
	"testing"
	"time"

	"pdc/parser"
)

// makeReading builds a reading stamped at Unix milliseconds (test convenience).
// TimestampKey stores microseconds (= tsMs * 1000).
func makeReading(name string, tsMs int64) parser.Reading {
	return parser.Reading{
		PMUName:   name,
		Timestamp: time.UnixMilli(tsMs).UTC(),
		SOC:       uint32(tsMs / 1000),
		Frequency: 50,
	}
}

func us(ms int64) int64 { return ms * 1000 }

func TestTimestampKeyIsParsedMeasurementTime(t *testing.T) {
	meas := time.UnixMilli(1_700_000_000_040).UTC()
	r := parser.Reading{
		PMUName:   "pmu-t",
		Timestamp: meas,
		Trace:     parser.LatencyTrace{ReceivedAtUnixNano: meas.Add(5 * time.Hour).UnixNano()},
	}
	got := TimestampKey(r)
	want := meas.UnixMicro()
	if got != want {
		t.Fatalf("TimestampKey=%d want parsed meas µs %d", got, want)
	}
	if got == time.Unix(0, r.Trace.ReceivedAtUnixNano).UnixMicro() {
		t.Fatal("key must not follow receive/capture time")
	}
}

func TestPMUBufferDropsOldestWhenFull(t *testing.T) {
	b := newPMUBuffer(3)
	b.put(us(100), makeReading("a", 100))
	b.put(us(200), makeReading("a", 200))
	b.put(us(300), makeReading("a", 300))
	b.put(us(400), makeReading("a", 400)) // drops 100

	if len(b.byTS) != 3 {
		t.Fatalf("len=%d want 3", len(b.byTS))
	}
	if _, ok := b.byTS[us(100)]; ok {
		t.Fatal("oldest 100 should have been dropped")
	}
	if _, ok := b.byTS[us(400)]; !ok {
		t.Fatal("newest 400 should be present")
	}
}

func TestBankIngestAndSnapshot(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"pmu-2", "pmu-1"})
	bank.Ingest(makeReading("pmu-1", 1000))
	bank.Ingest(makeReading("pmu-2", 1100))
	bank.Ingest(makeReading("unknown", 1200)) // ignored

	snap := bank.Snapshot()
	if len(snap.Expected) != 2 || snap.Expected[0] != "pmu-1" {
		t.Fatalf("expected sorted names, got %#v", snap.Expected)
	}
	if snap.CandidatePublishMs != 1000 {
		t.Fatalf("candidate=%d want 1000 ms (oldest across PMUs)", snap.CandidatePublishMs)
	}
	if snap.PMUs[0].Count != 1 || snap.PMUs[1].Count != 1 {
		t.Fatalf("counts: %#v %#v", snap.PMUs[0], snap.PMUs[1])
	}

	present, missing := bank.TakeTick(us(1000))
	if len(present) != 1 || len(missing) != 1 {
		t.Fatalf("TakeTick present=%d missing=%d", len(present), len(missing))
	}
	snap2 := bank.Snapshot()
	if snap2.PMUs[0].Count != 1 {
		t.Fatalf("arrival ring should still show pmu-1 after TakeTick, got count=%d", snap2.PMUs[0].Count)
	}
}

func TestBankAddRemoveKeepsPeerBuffers(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"a", "b", "c"})
	bank.Ingest(makeReading("a", 1000))
	bank.Ingest(makeReading("b", 1000))
	bank.Ingest(makeReading("c", 1000))
	bank.Ingest(makeReading("a", 1050))
	bank.Ingest(makeReading("b", 1050))

	bank.RemoveExpected("c")
	got := bank.Expected()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected [a b] after remove c, got %v", got)
	}
	present, missing := bank.PeekTick(us(1000))
	if len(present) != 2 || len(missing) != 0 {
		t.Fatalf("peer samples should survive remove: present=%d missing=%d", len(present), len(missing))
	}

	bank.AddExpected("c")
	got = bank.Expected()
	if len(got) != 3 {
		t.Fatalf("expected 3 after re-add, got %v", got)
	}
	present, missing = bank.PeekTick(us(1000))
	if _, ok := present["c"]; ok {
		t.Fatal("re-added c must start empty")
	}
	if len(present) != 2 || len(missing) != 1 {
		t.Fatalf("a/b kept, c missing: present=%d missing=%v", len(present), missing)
	}
}

func TestBankSyncExpectedIncremental(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"a", "b", "c"})
	bank.Ingest(makeReading("a", 2000))
	bank.Ingest(makeReading("b", 2000))

	if bank.SyncExpected([]string{"a", "b", "c"}) {
		t.Fatal("identical set should be a no-op")
	}
	if !bank.SyncExpected([]string{"a", "b"}) {
		t.Fatal("removing c should report changed")
	}
	present, missing := bank.PeekTick(us(2000))
	if len(present) != 2 || len(missing) != 0 {
		t.Fatalf("a/b samples must remain after sync remove: present=%d missing=%v", len(present), missing)
	}
}

func TestBankSetCapacityTrims(t *testing.T) {
	bank := NewBank(20)
	bank.SetExpected([]string{"a"})
	for i := int64(1); i <= 20; i++ {
		bank.Ingest(makeReading("a", i*100))
	}
	if got := bank.SetCapacity(12); got != 12 {
		t.Fatalf("SetCapacity=%d want 12", got)
	}
	snap := bank.Snapshot()
	if snap.Capacity != 12 {
		t.Fatalf("snap capacity=%d want 12", snap.Capacity)
	}
	if snap.PMUs[0].Count != 12 {
		t.Fatalf("count=%d want 12 after shrink", snap.PMUs[0].Count)
	}
	if bank.SetCapacity(1) != MinCapacity {
		t.Fatalf("below min should clamp to %d", MinCapacity)
	}
	if bank.SetCapacity(MaxCapacity + 50) != MaxCapacity {
		t.Fatalf("above max should clamp to %d", MaxCapacity)
	}
}

func TestTakeTickWindowNearest(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"pmu-t"})
	bank.Ingest(makeReading("pmu-t", 9159))

	present, missing := bank.TakeTickWindow(us(9160), us(20))
	if len(present) != 1 || len(missing) != 0 {
		t.Fatalf("nearest window present=%d missing=%d", len(present), len(missing))
	}
	if _, ok := present["pmu-t"]; !ok {
		t.Fatal("expected pmu-t")
	}
	present2, missing2 := bank.TakeTick(us(9159))
	if len(present2) != 0 || len(missing2) != 1 {
		t.Fatalf("after take: present=%d missing=%d", len(present2), len(missing2))
	}
}
