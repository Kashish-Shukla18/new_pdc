package store

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/config"
)

// Store persists PMU registry rows in Postgres (pmu_config).
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

// DefaultDSN matches docker-compose timescaledb service.
func DefaultDSN() string {
	return env("POSTGRES_DSN", "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable")
}

func NewStore() (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, DefaultDSN())
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	if err := ensureSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return &Store{pool: pool}, nil
}

func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS pmu_config (
    name          TEXT PRIMARY KEY,
    ip            TEXT NOT NULL DEFAULT '',
    port          INT  NOT NULL DEFAULT 0,
    tcp_port      INT  NOT NULL DEFAULT 0,
    idcode        INT  NOT NULL DEFAULT 0,
    protocol      TEXT NOT NULL DEFAULT 'tcp',
    timeout_sec   INT  NOT NULL DEFAULT 0,
    reconnect_sec INT  NOT NULL DEFAULT 0,
    region        TEXT NOT NULL DEFAULT '',
    lat           DOUBLE PRECISION NOT NULL DEFAULT 0,
    lon           DOUBLE PRECISION NOT NULL DEFAULT 0,
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`)
	return err
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) SavePMU(ctx context.Context, cfg config.PMUConfig) error {
	_, err := s.pool.Exec(ctx, `
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
    updated_at = NOW()`,
		cfg.Name, cfg.IP, cfg.Port, cfg.TCPPort, int(cfg.IDCode), cfg.Protocol,
		cfg.TimeoutSec, cfg.ReconnectSec, cfg.Region, cfg.Lat, cfg.Lon,
	)
	return err
}

func (s *Store) DeletePMU(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, `
UPDATE pmu_config SET active = FALSE, updated_at = NOW() WHERE name = $1`, name)
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
		var (
			cfg    config.PMUConfig
			idcode int
		)
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

// Ping is a health helper for diagnostics.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store not initialized")
	}
	return s.pool.Ping(ctx)
}

// Pool exposes the pool for shared use (optional).
func (s *Store) Pool() *pgxpool.Pool {
	if s == nil {
		return nil
	}
	return s.pool
}