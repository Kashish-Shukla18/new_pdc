-- First-boot schema for the PDC.
-- This is the PMU address book only.
-- Reading history (pmu_readings) stays parked with the storage sink in output/.

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS pmu_config (
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
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- PARKED: readings history (re-enable with the storage sink in output/).
-- CREATE TABLE IF NOT EXISTS pmu_readings ( ... );
-- SELECT create_hypertable('pmu_readings', 'time', if_not_exists => TRUE);
