package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestHistorySchemaApplied(t *testing.T) {
	prevReconnect := os.Getenv("POSTGRES_RECONNECT_SEC")
	_ = os.Setenv("POSTGRES_RECONNECT_SEC", "1h")
	t.Cleanup(func() { _ = os.Setenv("POSTGRES_RECONNECT_SEC", prevReconnect) })

	s := NewStore()
	defer s.Close()
	if !s.Ready() {
		t.Skip("postgres not available; skip history schema integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := s.getPool()
	if err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"pmu_config", "pmu_cfg_version", "pmu_event", "pmu_frame"} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("missing table %s", table)
		}
	}

	var isHyper bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM timescaledb_information.hypertables
			WHERE hypertable_name = 'pmu_frame'
		)`).Scan(&isHyper)
	if err != nil {
		t.Fatalf("hypertable check: %v", err)
	}
	if !isHyper {
		t.Fatal("pmu_frame is not a hypertable")
	}

	var intervalTxt string
	err = pool.QueryRow(ctx, `
		SELECT time_interval::text
		FROM timescaledb_information.dimensions
		WHERE hypertable_name = 'pmu_frame' AND column_name = 'time'
		LIMIT 1`).Scan(&intervalTxt)
	if err != nil {
		t.Fatalf("chunk interval: %v", err)
	}
	var h, m, sec int
	if _, err := fmt.Sscanf(intervalTxt, "%d:%d:%d", &h, &m, &sec); err != nil || h != 1 || m != 0 || sec != 0 {
		t.Fatalf("chunk time_interval=%q want 01:00:00 (1 hour)", intervalTxt)
	}

	var compressionOn bool
	err = pool.QueryRow(ctx, `
		SELECT compression_enabled
		FROM timescaledb_information.hypertables
		WHERE hypertable_name = 'pmu_frame'`).Scan(&compressionOn)
	if err != nil {
		t.Fatalf("compression_enabled: %v", err)
	}
	if !compressionOn {
		t.Fatal("pmu_frame compression_enabled=false")
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO pmu_cfg_version (pmu_id, layout_hash, station, idcode, data_rate, time_base, channels)
		VALUES ('test:1', 'hash-phase1', 'TEST', 1, 50, 1000000, '[]'::jsonb)
		ON CONFLICT (pmu_id, layout_hash) DO NOTHING`)
	if err != nil {
		t.Fatalf("insert cfg_version: %v", err)
	}
	var cfgID int64
	err = pool.QueryRow(ctx, `
		SELECT cfg_id FROM pmu_cfg_version WHERE pmu_id = 'test:1' AND layout_hash = 'hash-phase1'`).Scan(&cfgID)
	if err != nil {
		t.Fatalf("select cfg_id: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO pmu_event (time, pmu_id, type, detail)
		VALUES (NOW(), 'test:1', 'became_live', '{"phase":1}'::jsonb)`)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO pmu_frame (
			time, received_at, pmu_id, cfg_id, quality_ok, reject_reason,
			idcode, stat, time_quality, freq, freq_dev, rocof,
			va_mag, va_ang, phasor_mag, phasor_ang, analogs, digitals
		) VALUES (
			NOW(), NOW(), 'test:1', $1, TRUE, NULL,
			1, 0, 0, 50.0, 0.0, 0.0,
			1.0, 0.0, ARRAY[1.0, 1.0]::float8[], ARRAY[0.0, 120.0]::float8[],
			ARRAY[0.1]::float8[], ARRAY[0]::int[]
		)`, cfgID)
	if err != nil {
		t.Fatalf("insert frame: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO pmu_frame (
			time, received_at, pmu_id, cfg_id, quality_ok, reject_reason, freq
		) VALUES (NOW(), NOW(), 'test:1', $1, FALSE, 'clock skew', 47.2)`, cfgID)
	if err != nil {
		t.Fatalf("insert quality-rejected frame: %v", err)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM pmu_frame WHERE pmu_id = 'test:1'`)
	_, _ = pool.Exec(ctx, `DELETE FROM pmu_event WHERE pmu_id = 'test:1'`)
	_, _ = pool.Exec(ctx, `DELETE FROM pmu_cfg_version WHERE pmu_id = 'test:1'`)
}
