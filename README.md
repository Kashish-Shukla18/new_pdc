# PDC

PDC is a Go-based synchrophasor processing service. It reads PMU streams from the configuration in `config/pmus.yaml`, validates frames, publishes data, and exposes Prometheus metrics.

## Requirements

- Go 1.26 or newer
- Docker Desktop if you want to run the supporting services from `docker-compose.yaml`

## Start the application

Run the service from the repository root:

```bash
go run . -config config/pmus.yaml -metrics-addr :2112
```

## Start the PMU simulator

The default PMU config points at `127.0.0.1:4712`, so start the simulator first in another terminal:

```bash
python stimulator.py --tcp-port 4712 --udp-port 4713
```

## Run 3 simulators + plot graphs

1. Start three simulator instances:

```bash
python run_3_simulators.py
```

This launcher starts the three PMUs with slightly different signal biases so the graph shows three distinct traces instead of overlapping lines.

2. Start PDC with the 3-PMU config:

```bash
go run . -config config/pmus_3.yaml -metrics-addr :2112
```

Note: `pmus_3.yaml` is inside the `config` directory, so `-config pmus_3.yaml` from repo root will fail.

3. Open live graphs (Frequency, MW, MVAR):

```bash
python plot_pmu_graphs.py
```

4. Stop everything with `Ctrl+C` in each terminal.

## Stop the application

The process shuts down cleanly when you press `Ctrl+C` in the terminal.

## Stop the PMU simulator

Press `Ctrl+C` in the simulator terminal.

## Start the supporting stack

If you want Kafka, Redis, InfluxDB, Grafana, and Prometheus locally, start the compose stack:

```bash
docker compose up -d
```

If Kafka starts slowly, wait 10-20 seconds before starting PDC.

### Kafka topic quick fix

If you see `Unknown Topic Or Partition`, create the topic manually once:

```bash
docker compose exec kafka kafka-topics --create --if-not-exists --topic pmu.readings --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1
```

Verify it exists:

```bash
docker compose exec kafka kafka-topics --list --bootstrap-server localhost:9092
```

## Stop the supporting stack

```bash
docker compose down
```

## Configuration

- Main PMU configuration: `config/pmus.yaml`
- Metrics endpoint: `http://localhost:2112/metrics`
- Compose ports:
  - Kafka: `localhost:9093`
  - Redis: `localhost:6380`
  - InfluxDB: `localhost:8087`
  - Grafana: `http://localhost:3000`
  - Prometheus: `http://localhost:9090`

## Notes

- The application uses `127.0.0.1` defaults in the sample config to avoid Windows localhost resolution issues.
- If you run multiple instances, make sure they do not point at the same spool files.

## No-Data-Loss structure (heavy load)

This project now follows the same reliability flow you shared:

1. PMU connections: one connection per PMU, reconnect loop per device.
2. Start command + stream: handshake requests configuration and data stream.
3. Parse stage: frames are parsed and quality-checked.
4. Queue stage: parsed readings are written to Kafka.
5. Time sync + quality gate: invalid data is flagged and tracked.
6. Dual sink: writes to Redis (live view) and InfluxDB (history).
7. Dashboard: Grafana reads live/historical views.
8. Monitoring: Prometheus tracks errors, spool backlog, and replay.

To protect against data loss during outages and load spikes:

- Kafka writes are synchronous (`Async=false`) with retry attempts and durable acks (`KAFKA_REQUIRED_ACKS=all` by default).
- Failed Kafka/Redis/Influx writes are buffered to local disk spool files.
- Spool appends are fsynced by default (`SPOOL_FSYNC=true`) so crash/power-loss windows are minimized.
- Spool replay is streaming-based, so large spool files do not need full in-memory loading.

Recommended runtime settings for heavy load:

```powershell
$env:KAFKA_REQUIRED_ACKS="all"
$env:KAFKA_MAX_ATTEMPTS="10"
$env:KAFKA_WRITE_TIMEOUT_MS="15000"
$env:KAFKA_READ_TIMEOUT_MS="15000"
$env:KAFKA_ENSURE_TOPIC="true"
$env:KAFKA_TOPIC_PARTITIONS="3"
$env:KAFKA_TOPIC_REPLICATION_FACTOR="1"
$env:SPOOL_FSYNC="true"
go run . -config config/pmus_3.yaml -metrics-addr :2112
```

Important production note:

- A single Kafka broker can still be a single point of failure.
- For stronger no-loss guarantees, run Kafka with replication (3 brokers), topic replication factor >= 3, and min in-sync replicas >= 2.