package store

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/config"
)

var schemaStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS timescaledb`,
	`CREATE TABLE IF NOT EXISTS pmu_config (
    name           TEXT PRIMARY KEY,
    ip             TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 0,
    tcp_port       INTEGER NOT NULL DEFAULT 0,
    idcode         INTEGER NOT NULL DEFAULT 0,
    protocol       TEXT NOT NULL DEFAULT 'tcp',
    timeout_sec    INTEGER NOT NULL DEFAULT 5,
    reconnect_sec  INTEGER NOT NULL DEFAULT 2,
    region         TEXT NOT NULL DEFAULT '',
    lat            DOUBLE PRECISION NOT NULL DEFAULT 0,
    lon            DOUBLE PRECISION NOT NULL DEFAULT 0,
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`,
	`CREATE TABLE IF NOT EXISTS pmu_readings (
    time           TIMESTAMPTZ NOT NULL,
    entity_id      TEXT NOT NULL,
    idcode         INTEGER,
    freq           DOUBLE PRECISION,
    freq_dev       DOUBLE PRECISION,
    rocof          DOUBLE PRECISION,
    mw             DOUBLE PRECISION,
    mvar           DOUBLE PRECISION,
    mva            DOUBLE PRECISION,
    power_factor   DOUBLE PRECISION,
    va_mag         DOUBLE PRECISION,
    va_ang         DOUBLE PRECISION,
    vb_mag         DOUBLE PRECISION,
    vb_ang         DOUBLE PRECISION,
    vc_mag         DOUBLE PRECISION,
    vc_ang         DOUBLE PRECISION,
    ia_mag         DOUBLE PRECISION,
    ia_ang         DOUBLE PRECISION,
    stat           INTEGER,
    digital        INTEGER,
    crc_valid      BOOLEAN,
    time_quality   INTEGER,
    PRIMARY KEY (time, entity_id)
)`,
	`SELECT create_hypertable('pmu_readings', 'time', if_not_exists => TRUE)`,
	`CREATE INDEX IF NOT EXISTS pmu_readings_entity_time_idx
    ON pmu_readings (entity_id, time DESC)`,
}

type Store struct {
	pool *pgxpool.Pool
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

// DSN returns POSTGRES_DSN (compose maps host 5433 → container 5432).
func DSN() string {
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		return v
	}
	return env("POSTGRES_DSN", "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable")
}

func NewStore() (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, DSN())
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	s := &Store{pool: pool}
	if err := s.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return s, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) SavePMU(ctx context.Context, cfg config.PMUConfig) error {
	const q = `
		INSERT INTO pmu_config (
			name, ip, port, tcp_port, idcode, protocol,
			timeout_sec, reconnect_sec, region, lat, lon, active, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, TRUE, NOW())
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
			active = TRUE,
			updated_at = NOW()`
	_, err := s.pool.Exec(ctx, q,
		cfg.Name, cfg.IP, cfg.Port, cfg.TCPPort, int(cfg.IDCode), cfg.Protocol,
		cfg.TimeoutSec, cfg.ReconnectSec, cfg.Region, cfg.Lat, cfg.Lon,
	)
	return err
}

func (s *Store) DeletePMU(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE pmu_config SET active = FALSE, updated_at = NOW() WHERE name = $1`,
		name,
	)
	return err
}

func (s *Store) GetAllPMUs(ctx context.Context) ([]config.PMUConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, ip, port, tcp_port, idcode, protocol,
		       timeout_sec, reconnect_sec, region, lat, lon
		FROM pmu_config
		WHERE active = TRUE
		ORDER BY name`)
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
		); err != nil {
			return nil, err
		}
		cfg.IDCode = uint16(idcode)
		pmus = append(pmus, cfg)
	}
	return pmus, rows.Err()
}
