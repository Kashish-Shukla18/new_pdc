package aligner

import (
	"testing"
	"time"

	"pdc/parser"
)

func makeReading(name string, tsMs int64) parser.Reading {
	return parser.Reading{
		PMUName:   name,
		Timestamp: time.UnixMilli(tsMs).UTC(),
		SOC:       uint32(tsMs / 1000),
		Frequency: 50,
	}
}

func TestTimestampKeyIsParsedMeasurementTime(t *testing.T) {
	meas := time.UnixMilli(1_700_000_000_040).UTC()
	r := parser.Reading{
		PMUName:   "pmu-t",
		Timestamp: meas,
		// Receive time must NOT affect the key — only SOC+FRACSEC.
		Trace: parser.LatencyTrace{ReceivedAtUnixNano: meas.Add(5 * time.Hour).UnixNano()},
	}
	got := TimestampKey(r)
	want := meas.UnixMilli()
	if got != want {
		t.Fatalf("TimestampKey=%d want parsed meas %d", got, want)
	}
	if got == time.Unix(0, r.Trace.ReceivedAtUnixNano).UnixMilli() {
		t.Fatal("key must not follow receive/capture time")
	}
}

func TestPMUBufferDropsOldestWhenFull(t *testing.T) {
	b := newPMUBuffer(3)
	b.put(100, makeReading("a", 100))
	b.put(200, makeReading("a", 200))
	b.put(300, makeReading("a", 300))
	b.put(400, makeReading("a", 400)) // drops 100

	if len(b.byTS) != 3 {
		t.Fatalf("len=%d want 3", len(b.byTS))
	}
	if _, ok := b.byTS[100]; ok {
		t.Fatal("oldest 100 should have been dropped")
	}
	if _, ok := b.byTS[400]; !ok {
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
		t.Fatalf("candidate=%d want 1000 (oldest across PMUs)", snap.CandidatePublishMs)
	}
	if snap.PMUs[0].Count != 1 || snap.PMUs[1].Count != 1 {
		t.Fatalf("counts: %#v %#v", snap.PMUs[0], snap.PMUs[1])
	}

	// Sheets keep arrival history even after the publisher takes the tick.
	present, missing := bank.TakeTick(1000)
	if len(present) != 1 || len(missing) != 1 {
		t.Fatalf("TakeTick present=%d missing=%d", len(present), len(missing))
	}
	snap2 := bank.Snapshot()
	if snap2.PMUs[0].Count != 1 {
		t.Fatalf("arrival ring should still show pmu-1 after TakeTick, got count=%d", snap2.PMUs[0].Count)
	}
}

func TestTakeTickWindowNearest(t *testing.T) {
	bank := NewBank(50)
	bank.SetExpected([]string{"pmu-t"})
	// Off-grid stamp like Typhoon 25 fps (…159 next to a 40 ms ruler mark …160).
	bank.Ingest(makeReading("pmu-t", 9159))

	present, missing := bank.TakeTickWindow(9160, 20)
	if len(present) != 1 || len(missing) != 0 {
		t.Fatalf("nearest window present=%d missing=%d", len(present), len(missing))
	}
	if _, ok := present["pmu-t"]; !ok {
		t.Fatal("expected pmu-t")
	}
	// Consumed — exact TakeTick should now miss.
	present2, missing2 := bank.TakeTick(9159)
	if len(present2) != 0 || len(missing2) != 1 {
		t.Fatalf("after take: present=%d missing=%d", len(present2), len(missing2))
	}
}
