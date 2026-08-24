-- Sample queries for local TimescaleDB / Postgres (PDC history)
-- Connect:  psql "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable"
-- Or:       docker compose exec timescaledb psql -U pdc -d pdc

-- Latest 20 rows across all PMUs
SELECT time, pmu, frequency, rocof, va_mag, va_phase_deg, soc, fracsec_count
FROM pmu_readings
ORDER BY time DESC
LIMIT 20;

-- Per-PMU counts (last 5 minutes)
SELECT pmu, count(*) AS samples, min(time) AS first_ts, max(time) AS last_ts
FROM pmu_readings
WHERE time > NOW() - INTERVAL '5 minutes'
GROUP BY pmu
ORDER BY pmu;

-- One PMU frequency trend (last 30 seconds)
SELECT time, frequency, frequency_dev, rocof
FROM pmu_readings
WHERE pmu = 'pmu-1'
  AND time > NOW() - INTERVAL '30 seconds'
ORDER BY time;

-- Active PMU config
SELECT name, ip, port, idcode, region, active, updated_at
FROM pmu_config
WHERE active
ORDER BY name;
