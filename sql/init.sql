-- TimescaleDB init for PDC config + history.
-- Mounted at /docker-entrypoint-initdb.d/ so it runs once on first volume create.

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS pmu_config (
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
);

CREATE TABLE IF NOT EXISTS pmu_readings (
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
);

SELECT create_hypertable('pmu_readings', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS pmu_readings_entity_time_idx
    ON pmu_readings (entity_id, time DESC);
