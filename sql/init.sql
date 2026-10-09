-- First-boot schema for the PDC (TimescaleDB).
-- Applied on empty volume via docker-entrypoint; also ensured at runtime by store.NewStore.

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ── Address book ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS pmu_config (
    name           TEXT PRIMARY KEY,  -- endpoint identity (ip:port or udp:ip:port)
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
    station        TEXT NOT NULL DEFAULT '',  -- CFG-2 STN label (display only)
    timestamp_tz   TEXT NOT NULL DEFAULT '',  -- '' = fleet default (IST); UTC for lab sims
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS pmu_config_endpoint_uidx
    ON pmu_config (lower(ip), port, lower(protocol))
    WHERE active = TRUE;

-- ── CFG-2 layout history ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS pmu_cfg_version (
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
);

-- ── Sparse lifecycle / quality events ──────────────────────────────────────
CREATE TABLE IF NOT EXISTS pmu_event (
    time    TIMESTAMPTZ NOT NULL,
    pmu_id  TEXT NOT NULL DEFAULT '',
    type    TEXT NOT NULL,
    detail  JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS pmu_event_pmu_time_idx
    ON pmu_event (pmu_id, time DESC);

CREATE INDEX IF NOT EXISTS pmu_event_type_time_idx
    ON pmu_event (type, time DESC);

-- ── Accepted + quality-flagged DATA frames (hypertable) ────────────────────
CREATE TABLE IF NOT EXISTS pmu_frame (
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
);

SELECT create_hypertable(
    'pmu_frame',
    'time',
    chunk_time_interval => INTERVAL '1 hour',
    if_not_exists => TRUE
);

CREATE INDEX IF NOT EXISTS pmu_frame_pmu_time_idx
    ON pmu_frame (pmu_id, time DESC);

CREATE INDEX IF NOT EXISTS pmu_frame_quality_time_idx
    ON pmu_frame (quality_ok, time DESC);

ALTER TABLE pmu_frame SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'pmu_id',
    timescaledb.compress_orderby = 'time'
);

SELECT add_compression_policy(
    'pmu_frame',
    compress_after => INTERVAL '2 hours',
    if_not_exists => TRUE
);
