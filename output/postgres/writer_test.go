package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/parser"
	"pdc/store"
)

func TestEnqueueDropsWhenFull(t *testing.T) {
	w := &Writer{
		frames: make(chan FrameRow, 2),
		events: make(chan EventRow, 1),
	}
	for i := 0; i < 5; i++ {
		w.EnqueueFrame(FrameRow{PMUID: "a", Time: time.Now()})
	}
	st := w.StatsSnapshot()
	if st.Enqueued != 2 {
		t.Fatalf("enqueued=%d want 2", st.Enqueued)
	}
	if st.Dropped != 3 {
		t.Fatalf("dropped=%d want 3", st.Dropped)
	}
	w.EnqueueEvent(EventRow{Type: "became_live", PMUID: "a"})
	w.EnqueueEvent(EventRow{Type: "left_live", PMUID: "a"})
	st = w.StatsSnapshot()
	if st.EventsIn != 1 || st.EventsDrop != 1 {
		t.Fatalf("events in=%d drop=%d", st.EventsIn, st.EventsDrop)
	}
}

func TestEnqueueNeverBlocks(t *testing.T) {
	w := &Writer{
		frames: make(chan FrameRow, 1),
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			w.EnqueueFrame(FrameRow{PMUID: "x", Time: time.Now()})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EnqueueFrame blocked under backpressure")
	}
	if w.StatsSnapshot().Dropped == 0 {
		t.Fatal("expected drops when consumer is absent")
	}
}

func TestFrameFromReadingQualityFlag(t *testing.T) {
	r := parser.Reading{
		PMUName:   "10.0.0.1:4712",
		Timestamp: time.Unix(1700000000, 0).UTC(),
		IDCode:    7,
		Frequency: 50.1,
		VA:        parser.Phasor{Magnitude: 1.2, PhaseDegrees: 30},
		Phasors: []parser.NamedPhasor{
			{Name: "VA", Phasor: parser.Phasor{Magnitude: 1.2, PhaseDegrees: 30}},
		},
		Trace: parser.LatencyTrace{ReceivedAtUnixNano: time.Unix(1700000001, 0).UnixNano()},
	}
	row := FrameFromReading(r, false, "clock skew", 42)
	if row.QualityOK || row.RejectReason != "clock skew" || row.CfgID != 42 {
		t.Fatalf("row=%+v", row)
	}
	if row.Freq < 50.09 || row.Freq > 50.11 || len(row.PhasorMag) != 1 {
		t.Fatalf("mapping freq=%v phasors=%d", row.Freq, len(row.PhasorMag))
	}
	if !row.ReceivedAt.Equal(time.Unix(1700000001, 0).UTC()) {
		t.Fatalf("received_at=%v", row.ReceivedAt)
	}
}

func TestHistoryEnabledEnv(t *testing.T) {
	t.Setenv("ENABLE_HISTORY", "")
	t.Setenv("ENABLE_SINK", "")
	if historyEnabled() {
		t.Fatal("default should be disabled")
	}
	t.Setenv("ENABLE_HISTORY", "true")
	if !historyEnabled() {
		t.Fatal("ENABLE_HISTORY=true")
	}
	t.Setenv("ENABLE_HISTORY", "")
	t.Setenv("ENABLE_SINK", "1")
	if !historyEnabled() {
		t.Fatal("ENABLE_SINK fallback")
	}
}

func TestNewWriterFromEnvDisabled(t *testing.T) {
	t.Setenv("ENABLE_HISTORY", "false")
	t.Setenv("ENABLE_SINK", "false")
	_, err := NewWriterFromEnv(context.Background())
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestWriterCopyFromIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	probe, err := pgxpool.New(ctx, store.DSN())
	if err != nil {
		t.Skip("postgres not available")
	}
	if err := probe.Ping(ctx); err != nil {
		probe.Close()
		t.Skip("postgres not available")
	}
	probe.Close()

	w, err := NewWriter(ctx, WriterConfig{
		QueueSize:     64,
		EventQueue:    16,
		BatchSize:     10,
		FlushEvery:    100 * time.Millisecond,
		InsertTimeout: 5 * time.Second,
		Reconnect:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !w.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !w.Ready() {
		w.Close()
		t.Fatal("writer not ready")
	}

	cfgID, err := w.EnsureCfgVersion(ctx, CfgVersion{
		PMUID:      "hist-test:1",
		LayoutHash: "phase2-hash",
		Station:    "HTEST",
		IDCode:     9,
		DataRate:   50,
		TimeBase:   1_000_000,
		Channels:   []byte(`[{"name":"VA"}]`),
	})
	if err != nil {
		w.Close()
		t.Fatalf("EnsureCfgVersion: %v", err)
	}
	cfgID2, err := w.EnsureCfgVersion(ctx, CfgVersion{
		PMUID:      "hist-test:1",
		LayoutHash: "phase2-hash",
		Station:    "HTEST",
	})
	if err != nil || cfgID2 != cfgID {
		w.Close()
		t.Fatalf("dedupe cfg_id=%d/%d err=%v", cfgID, cfgID2, err)
	}

	now := time.Now().UTC()
	w.EnqueueFrame(FrameRow{
		Time: now, ReceivedAt: now, PMUID: "hist-test:1", CfgID: cfgID,
		QualityOK: true, Freq: 50.0, PhasorMag: []float64{1}, PhasorAng: []float64{0},
	})
	w.EnqueueFrame(FrameRow{
		Time: now.Add(time.Millisecond), ReceivedAt: now, PMUID: "hist-test:1", CfgID: cfgID,
		QualityOK: false, RejectReason: "unsync", Freq: 47.5,
	})
	w.EnqueueEvent(EventRow{
		Time: now, PMUID: "hist-test:1", Type: "became_live", Detail: []byte(`{"phase":2}`),
	})

	w.Close()

	pool, err := pgxpool.New(ctx, store.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var frames int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pmu_frame WHERE pmu_id = 'hist-test:1'`).Scan(&frames); err != nil {
		t.Fatal(err)
	}
	if frames < 2 {
		t.Fatalf("frames=%d want >= 2", frames)
	}
	var bad float64
	if err := pool.QueryRow(ctx, `
		SELECT freq FROM pmu_frame
		WHERE pmu_id = 'hist-test:1' AND quality_ok = FALSE
		ORDER BY time DESC LIMIT 1`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 47.5 {
		t.Fatalf("rejected freq=%v", bad)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pmu_event WHERE pmu_id = 'hist-test:1'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events < 1 {
		t.Fatalf("events=%d", events)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM pmu_frame WHERE pmu_id = 'hist-test:1'`)
	_, _ = pool.Exec(ctx, `DELETE FROM pmu_event WHERE pmu_id = 'hist-test:1'`)
	_, _ = pool.Exec(ctx, `DELETE FROM pmu_cfg_version WHERE pmu_id = 'hist-test:1'`)
}
