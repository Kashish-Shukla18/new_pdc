-- PDC TimescaleDB / Postgres init (mounted into docker entrypoint)
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- PMU registry (replaces Influx pmu_config measurement)
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
);

-- Synchrophasor history (replaces Influx pmu_readings)
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
);

SELECT create_hypertable('pmu_readings', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS pmu_readings_pmu_time_idx
    ON pmu_readings (pmu, time DESC);
