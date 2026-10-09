// Package store keeps the list of PMUs in Postgres and ensures the shared
// Timescale schema (address book + history tables).
//
// Frame/event ingest is not in this package yet (storage writer phase).
//
// If Postgres is down at startup, NewStore still returns a usable Store in
// degraded mode (Ready()==false) and reconnects in the background. The live
// PDC path must not Fatalf on address-book unavailability.
package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/config"
)

// ErrUnavailable means the address-book DB is not connected yet (or reconnecting).
var ErrUnavailable = errors.New("postgres address book unavailable")

// schemaStatements are applied on every successful connect (idempotent).
var schemaStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS timescaledb`,
	`CREATE TABLE IF NOT EXISTS pmu_config (
    name           TEXT PRIMARY KEY,
    ip             TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 0,
    tcp_port       INTEGER NOT NULL DEFAULT 0,
    idcode         INTEGER NOT NULL DEFAULT 0,
    protocol       TEXT NOT NULL DEFAULT 'tcp',
    timeout_sec    INTEGER NOT NULL DEFAULT 60,
    reconnect_sec  INTEGER NOT NULL DEFAULT 5,
    region         TEXT NOT NULL DEFAULT '',
    lat            DOUBLE PRECISION NOT NULL DEFAULT 0,
    lon            DOUBLE PRECISION NOT NULL DEFAULT 0,
    station        TEXT NOT NULL DEFAULT '',
    timestamp_tz   TEXT NOT NULL DEFAULT '',
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`,
	`ALTER TABLE pmu_config ADD COLUMN IF NOT EXISTS station TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE pmu_config ADD COLUMN IF NOT EXISTS timestamp_tz TEXT NOT NULL DEFAULT ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS pmu_config_endpoint_uidx
		ON pmu_config (lower(ip), port, lower(protocol))
		WHERE active = TRUE`,

	`CREATE TABLE IF NOT EXISTS pmu_cfg_version (
    cfg_id       BIGSERIAL PRIMARY KEY,
    pmu_id       TEXT NOT NULL,
    layout_hash  TEXT NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    station      TEXT NOT NULL DEFAULT '',
    idcode       INTEGER NOT NULL DEFAULT 0,
    data_rate    INTEGER NOT NULL DEFAULT 0,
    time_base    INTEGER NOT NULL DEFAULT 0,
    channels     JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_cfg      BYTEA,
    UNIQUE (pmu_id, layout_hash)
)`,

	`CREATE TABLE IF NOT EXISTS pmu_event (
    time    TIMESTAMPTZ NOT NULL,
    pmu_id  TEXT NOT NULL DEFAULT '',
    type    TEXT NOT NULL,
    detail  JSONB NOT NULL DEFAULT '{}'::jsonb
)`,
	`CREATE INDEX IF NOT EXISTS pmu_event_pmu_time_idx
		ON pmu_event (pmu_id, time DESC)`,
	`CREATE INDEX IF NOT EXISTS pmu_event_type_time_idx
		ON pmu_event (type, time DESC)`,

	`CREATE TABLE IF NOT EXISTS pmu_frame (
    time           TIMESTAMPTZ NOT NULL,
    received_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    pmu_id         TEXT NOT NULL,
    cfg_id         BIGINT,
    quality_ok     BOOLEAN NOT NULL DEFAULT TRUE,
    reject_reason  TEXT,
    idcode         INTEGER NOT NULL DEFAULT 0,
    stat           INTEGER NOT NULL DEFAULT 0,
    time_quality   INTEGER NOT NULL DEFAULT 0,
    freq           DOUBLE PRECISION NOT NULL DEFAULT 0,
    freq_dev       DOUBLE PRECISION NOT NULL DEFAULT 0,
    rocof          DOUBLE PRECISION NOT NULL DEFAULT 0,
    va_mag         DOUBLE PRECISION NOT NULL DEFAULT 0,
    va_ang         DOUBLE PRECISION NOT NULL DEFAULT 0,
    vb_mag         DOUBLE PRECISION NOT NULL DEFAULT 0,
    vb_ang         DOUBLE PRECISION NOT NULL DEFAULT 0,
    vc_mag         DOUBLE PRECISION NOT NULL DEFAULT 0,
    vc_ang         DOUBLE PRECISION NOT NULL DEFAULT 0,
    ia_mag         DOUBLE PRECISION NOT NULL DEFAULT 0,
    ia_ang         DOUBLE PRECISION NOT NULL DEFAULT 0,
    phasor_mag     DOUBLE PRECISION[],
    phasor_ang     DOUBLE PRECISION[],
    analogs        DOUBLE PRECISION[],
    digitals       INTEGER[]
)`,
	`SELECT create_hypertable(
		'pmu_frame',
		'time',
		chunk_time_interval => INTERVAL '1 hour',
		if_not_exists => TRUE
	)`,
	`CREATE INDEX IF NOT EXISTS pmu_frame_pmu_time_idx
		ON pmu_frame (pmu_id, time DESC)`,
	`CREATE INDEX IF NOT EXISTS pmu_frame_quality_time_idx
		ON pmu_frame (quality_ok, time DESC)`,
}

// compressionStatements are best-effort (log + continue on failure).
var compressionStatements = []string{
	`ALTER TABLE pmu_frame SET (
		timescaledb.compress,
		timescaledb.compress_segmentby = 'pmu_id',
		timescaledb.compress_orderby = 'time'
	)`,
	`SELECT add_compression_policy(
		'pmu_frame',
		compress_after => INTERVAL '2 hours',
		if_not_exists => TRUE
	)`,
}

type Store struct {
	mu     sync.RWMutex
	pool   *pgxpool.Pool
	cancel context.CancelFunc
	done   chan struct{}
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func reconnectInterval() time.Duration {
	v := env("POSTGRES_RECONNECT_SEC", "5s")
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	// Plain integer seconds, e.g. "5".
	if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
		return n
	}
	return 5 * time.Second
}

// DSN is the Postgres connection string (compose maps host 5433 → container 5432).
func DSN() string {
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable"
}

// NewStore opens the address book. It never fails closed: if Postgres is down,
// Ready() is false and a background loop retries until Close.
func NewStore() *Store {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if err := s.tryConnect(); err != nil {
		log.Printf("postgres (PMU config): unavailable at startup: %v (degraded; will retry)", err)
	} else {
		log.Printf("postgres (PMU config): connected")
	}
	go s.reconnectLoop(ctx)
	return s
}

func (s *Store) reconnectLoop(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(reconnectInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Ready() {
				continue
			}
			if err := s.tryConnect(); err != nil {
				log.Printf("postgres (PMU config): reconnect failed: %v", err)
				continue
			}
			log.Printf("postgres (PMU config): reconnected")
		}
	}
}

func (s *Store) tryConnect() error {
	s.mu.RLock()
	already := s.pool != nil
	s.mu.RUnlock()
	if already {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, DSN())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping: %w", err)
	}

	tmp := &Store{pool: pool}
	if err := tmp.ensureSchema(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("schema: %w", err)
	}
	if err := tmp.migrateEndpointIdentities(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("identity migrate: %w", err)
	}

	s.mu.Lock()
	if s.pool != nil {
		s.mu.Unlock()
		pool.Close()
		return nil
	}
	s.pool = pool
	s.mu.Unlock()
	return nil
}

// Ready reports whether the address book has a live pool.
func (s *Store) Ready() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool != nil
}

func (s *Store) getPool() (*pgxpool.Pool, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	return s.pool, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	for _, stmt := range compressionStatements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			log.Printf("postgres schema: compression setup skipped: %v", err)
		}
	}
	return nil
}

// migrateEndpointIdentities rewrites legacy nickname PKs to endpoint identities.
func (s *Store) migrateEndpointIdentities(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT name, ip, port, tcp_port, idcode, protocol,
		       timeout_sec, reconnect_sec, region, lat, lon, station, timestamp_tz
		FROM pmu_config
		WHERE active = TRUE`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		cfg config.PMUConfig
	}
	var list []row
	for rows.Next() {
		var r row
		var idcode int
		if err := rows.Scan(
			&r.cfg.Name, &r.cfg.IP, &r.cfg.Port, &r.cfg.TCPPort, &idcode, &r.cfg.Protocol,
			&r.cfg.TimeoutSec, &r.cfg.ReconnectSec, &r.cfg.Region, &r.cfg.Lat, &r.cfg.Lon,
			&r.cfg.Station, &r.cfg.TimestampTZ,
		); err != nil {
			return err
		}
		r.cfg.IDCode = uint16(idcode)
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range list {
		old := r.cfg.Name
		r.cfg.Normalize()
		if r.cfg.Name == "" || r.cfg.Name == old {
			continue
		}
		tag, err := s.pool.Exec(ctx, `
			UPDATE pmu_config SET name = $1, updated_at = NOW()
			WHERE name = $2 AND NOT EXISTS (
				SELECT 1 FROM pmu_config WHERE name = $1
			)`, r.cfg.Name, old)
		if err != nil {
			return fmt.Errorf("rename %q → %q: %w", old, r.cfg.Name, err)
		}
		if tag.RowsAffected() == 0 {
			if _, err := s.pool.Exec(ctx,
				`UPDATE pmu_config SET active = FALSE, updated_at = NOW() WHERE name = $1`, old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
	}
	s.mu.Lock()
	if s.pool != nil {
		s.pool.Close()
		s.pool = nil
	}
	s.mu.Unlock()
}

func (s *Store) SavePMU(ctx context.Context, cfg config.PMUConfig) error {
	pool, err := s.getPool()
	if err != nil {
		return err
	}
	cfg.Normalize()
	if cfg.Name == "" {
		return fmt.Errorf("invalid PMU endpoint (need ip and port)")
	}
	const q = `
		INSERT INTO pmu_config (
			name, ip, port, tcp_port, idcode, protocol,
			timeout_sec, reconnect_sec, region, lat, lon, station, timestamp_tz, active, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, TRUE, NOW())
		ON CONFLICT (name) DO UPDATE SET
			ip = EXCLUDED.ip,
			port = EXCLUDED.port,
			tcp_port = EXCLUDED.tcp_port,
			idcode = EXCLUDED.idcode,
			protocol = EXCLUDED.protocol,
			timeout_sec = EXCLUDED.timeout_sec,
			reconnect_sec = EXCLUDED.reconnect_sec,
			region = EXCLUDED.region,
			lat = EXCLUDED.lat,
			lon = EXCLUDED.lon,
			station = CASE
				WHEN EXCLUDED.station <> '' THEN EXCLUDED.station
				ELSE pmu_config.station
			END,
			timestamp_tz = CASE
				WHEN EXCLUDED.timestamp_tz <> '' THEN EXCLUDED.timestamp_tz
				ELSE pmu_config.timestamp_tz
			END,
			active = TRUE,
			updated_at = NOW()`
	_, err = pool.Exec(ctx, q,
		cfg.Name, cfg.IP, cfg.Port, cfg.TCPPort, int(cfg.IDCode), cfg.Protocol,
		cfg.TimeoutSec, cfg.ReconnectSec, cfg.Region, cfg.Lat, cfg.Lon, cfg.Station, cfg.TimestampTZ,
	)
	return err
}

// UpdateStation stores the CFG-2 STN label for an endpoint identity.
func (s *Store) UpdateStation(ctx context.Context, name, station string) error {
	pool, err := s.getPool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		UPDATE pmu_config SET station = $2, updated_at = NOW()
		WHERE name = $1 AND active = TRUE`, name, station)
	return err
}

func (s *Store) DeletePMU(ctx context.Context, name string) error {
	return s.SetPMUActive(ctx, name, false)
}

// SetPMUActive marks a registered PMU as operator-enabled (true) or disconnected (false).
func (s *Store) SetPMUActive(ctx context.Context, name string, active bool) error {
	pool, err := s.getPool()
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx,
		`UPDATE pmu_config SET active = $2, updated_at = NOW() WHERE name = $1`,
		name, active,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("PMU %s not found", name)
	}
	return nil
}

// GetPMU returns one registered PMU (active or disconnected).
func (s *Store) GetPMU(ctx context.Context, name string) (config.PMUConfig, error) {
	pool, err := s.getPool()
	if err != nil {
		return config.PMUConfig{}, err
	}
	var cfg config.PMUConfig
	var idcode int
	err = pool.QueryRow(ctx, `
		SELECT name, ip, port, tcp_port, idcode, protocol,
		       timeout_sec, reconnect_sec, region, lat, lon, station, timestamp_tz, active
		FROM pmu_config
		WHERE name = $1`, name).Scan(
		&cfg.Name, &cfg.IP, &cfg.Port, &cfg.TCPPort, &idcode, &cfg.Protocol,
		&cfg.TimeoutSec, &cfg.ReconnectSec, &cfg.Region, &cfg.Lat, &cfg.Lon,
		&cfg.Station, &cfg.TimestampTZ, &cfg.Active,
	)
	if err != nil {
		return config.PMUConfig{}, err
	}
	cfg.IDCode = uint16(idcode)
	cfg.Normalize()
	return cfg, nil
}

// GetActivePMUs returns operator-enabled PMUs (startup / auto-start set).
func (s *Store) GetActivePMUs(ctx context.Context) ([]config.PMUConfig, error) {
	all, err := s.GetAllPMUs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]config.PMUConfig, 0, len(all))
	for _, p := range all {
		if p.Active {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Store) GetAllPMUs(ctx context.Context) ([]config.PMUConfig, error) {
	pool, err := s.getPool()
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT name, ip, port, tcp_port, idcode, protocol,
		       timeout_sec, reconnect_sec, region, lat, lon, station, timestamp_tz, active
		FROM pmu_config
		ORDER BY active DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pmus []config.PMUConfig
	for rows.Next() {
		var cfg config.PMUConfig
		var idcode int
		if err := rows.Scan(
			&cfg.Name, &cfg.IP, &cfg.Port, &cfg.TCPPort, &idcode, &cfg.Protocol,
			&cfg.TimeoutSec, &cfg.ReconnectSec, &cfg.Region, &cfg.Lat, &cfg.Lon,
			&cfg.Station, &cfg.TimestampTZ, &cfg.Active,
		); err != nil {
			return nil, err
		}
		cfg.IDCode = uint16(idcode)
		cfg.Normalize()
		pmus = append(pmus, cfg)
	}
	return pmus, rows.Err()
}
