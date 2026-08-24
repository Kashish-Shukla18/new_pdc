package output

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/parser"
)

// PostgresHistory batches synchrophasor readings into TimescaleDB / Postgres.
type PostgresHistory struct {
	pool         *pgxpool.Pool
	batchSize    int
	flushEvery   time.Duration
	mu           sync.Mutex
	buf          []parser.Reading
	stopCh       chan struct{}
	doneCh       chan struct{}
	flushReq     chan struct{}
}

// DefaultPostgresDSN matches docker-compose timescaledb.
func DefaultPostgresDSN() string {
	return env("POSTGRES_DSN", "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable")
}

// NewPostgresHistoryFromEnv connects and ensures the readings hypertable exists.
func NewPostgresHistoryFromEnv(ctx context.Context) (*PostgresHistory, error) {
	pool, err := pgxpool.New(ctx, DefaultPostgresDSN())
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	if err := ensureReadingsSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	batchSize := envInt("POSTGRES_BATCH_SIZE", 500)
	if batchSize < 1 {
		batchSize = 500
	}
	flushMs := envInt("POSTGRES_FLUSH_INTERVAL_MS", 1000)
	if flushMs < 50 {
		flushMs = 50
	}

	h := &PostgresHistory{
		pool:       pool,
		batchSize:  batchSize,
		flushEvery: time.Duration(flushMs) * time.Millisecond,
		buf:        make([]parser.Reading, 0, batchSize),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		flushReq:   make(chan struct{}, 1),
	}
	go h.loop()
	log.Printf("postgres history ready: dsn_host=127.0.0.1:5433 batch_size=%d flush_interval=%dms",
		batchSize, flushMs)
	return h, nil
}

func ensureReadingsSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`)
	if err != nil {
		// Plain Postgres without Timescale still works as a normal table.
		log.Printf("postgres: timescaledb extension unavailable (%v) — using plain table", err)
	}
	_, err = pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS pmu_readings (
    time            TIMESTAMPTZ NOT NULL,
    pmu             TEXT NOT NULL,
    idcode          INT NOT NULL DEFAULT 0,
    frequency       REAL,
    frequency_dev   REAL,
    rocof           REAL,
    va_mag          REAL,
    va_phase_deg    REAL,
    vb_mag          REAL,
    vb_phase_deg    REAL,
    vc_mag          REAL,
    vc_phase_deg    REAL,
    ia_mag          REAL,
    ia_phase_deg    REAL,
    voltage_imbalance REAL,
    mw              REAL,
    mvar            REAL,
    mva             REAL,
    power_factor    REAL,
    digital         INT,
    stat            INT,
    crc_valid       BOOLEAN,
    time_quality    INT,
    soc             BIGINT,
    fracsec_count   INT,
    PRIMARY KEY (time, pmu)
)`)
	if err != nil {
		return fmt.Errorf("create pmu_readings: %w", err)
	}
	_, _ = pool.Exec(ctx, `SELECT create_hypertable('pmu_readings', 'time', if_not_exists => TRUE)`)
	_, _ = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS pmu_readings_pmu_time_idx ON pmu_readings (pmu, time DESC)`)
	return nil
}

// Enqueue buffers a reading for batch insert.
func (h *PostgresHistory) Enqueue(r parser.Reading) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.buf = append(h.buf, r)
	full := len(h.buf) >= h.batchSize
	h.mu.Unlock()
	if full {
		select {
		case h.flushReq <- struct{}{}:
		default:
		}
	}
}

func (h *PostgresHistory) loop() {
	defer close(h.doneCh)
	t := time.NewTicker(h.flushEvery)
	defer t.Stop()
	for {
		select {
		case <-h.stopCh:
			h.flush()
			return
		case <-t.C:
			h.flush()
		case <-h.flushReq:
			h.flush()
		}
	}
}

func (h *PostgresHistory) flush() {
	h.mu.Lock()
	if len(h.buf) == 0 {
		h.mu.Unlock()
		return
	}
	batch := h.buf
	h.buf = make([]parser.Reading, 0, h.batchSize)
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := h.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pmu_readings"},
		[]string{
			"time", "pmu", "idcode",
			"frequency", "frequency_dev", "rocof",
			"va_mag", "va_phase_deg", "vb_mag", "vb_phase_deg",
			"vc_mag", "vc_phase_deg", "ia_mag", "ia_phase_deg",
			"voltage_imbalance", "mw", "mvar", "mva", "power_factor",
			"digital", "stat", "crc_valid", "time_quality",
			"soc", "fracsec_count",
		},
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			r := batch[i]
			ts := r.Timestamp
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			return []any{
				ts.UTC(), r.PMUName, int(r.IDCode),
				r.Frequency, r.FrequencyDeviation, r.ROCOF,
				r.VA.Magnitude, r.VA.PhaseDegrees,
				r.VB.Magnitude, r.VB.PhaseDegrees,
				r.VC.Magnitude, r.VC.PhaseDegrees,
				r.IA.Magnitude, r.IA.PhaseDegrees,
				r.VoltageImbalancePercent,
				r.MW, r.MVAR, r.MVA, r.PowerFactor,
				int(r.Digital), int(r.Stat), r.ChecksumValid, int(r.TimeQuality),
				int64(r.SOC), int(r.FracSecCount),
			}, nil
		}),
	)
	if err != nil {
		log.Printf("[postgres] copy insert error (%d rows): %v", len(batch), err)
	}
}

// Flush writes any buffered rows immediately.
func (h *PostgresHistory) Flush() {
	if h == nil {
		return
	}
	h.flush()
}

// Close flushes and closes the pool.
func (h *PostgresHistory) Close() {
	if h == nil {
		return
	}
	close(h.stopCh)
	<-h.doneCh
	h.pool.Close()
}
