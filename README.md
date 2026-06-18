# PDC

PDC is a Go-based synchrophasor processing service. It connects to PMUs over IEEE C37.118, parses and validates frames, publishes readings to Kafka, stores live state in Redis and history in InfluxDB, and exposes a real-time **React dashboard** for operators.

## Architecture

```
PMU → Receiver → Parse → Quality gate → Kafka (queue)
                                      ↘ Redis (live) + InfluxDB (history)
                                              ↓
                                    React dashboard (SSE + REST API)
```

| Component | Role |
|-----------|------|
| **Go PDC** | Ingestion, parsing, quality checks, Kafka publish, Redis/Influx sink |
| **Kafka** | Durable message queue (`pmu.readings`) |
| **Redis** | Latest reading + short rolling timeline per PMU |
| **InfluxDB** | Historical time-series + PMU configuration |
| **Prometheus** | Pipeline metrics (`/metrics` on `:2112`) |
| **React dashboard** | Live operator UI — overview, devices, data frames, connectivity, analytics |

Grafana is **not** used. All visualization and PMU management is handled by the React app in `dashboard/`.

## Requirements

- Go 1.26 or newer
- Node.js 20+ (for the React dashboard)
- Docker Desktop (for Kafka, Redis, InfluxDB, Prometheus)
- Python 3 (optional, for PMU simulators)

## Quick start

### 1. Start the supporting stack

```bash
docker compose up -d
```

Wait 10–20 seconds for Kafka to become ready.

Services:

| Service | URL / port |
|---------|------------|
| Kafka | `localhost:9093` |
| Redis | `localhost:6380` |
| InfluxDB | `http://localhost:8087` |
| Prometheus | `http://localhost:9090` |

### 2. Start PDC

```bash
go run . -metrics-addr :2112 -api-addr :8080
```

- Metrics + live conversation state: `http://localhost:2112`
- REST API (PMU CRUD): `http://localhost:8080`

PMU connections are loaded from InfluxDB on startup and can be added at runtime via the dashboard or `POST /api/pmus`.

### 3. Start the React dashboard

```bash
cd dashboard
npm install
npm run dev
```

Open **http://localhost:5173** (Vite dev server).

The dev server proxies API calls to the Go backend:

- `/conversation/*` → `:2112` (live state + SSE events)
- `/api/*` → `:8080` (PMU management)
- `/metrics` → `:2112` (Prometheus)

### 4. Start PMU simulators (optional)

Single simulator:

```bash
python stimulator.py --tcp-port 4712 --udp-port 4713
```

Three or more simulators with distinct traces:

```bash
python run_3_simulators.py
```

Register each simulator in the dashboard (**Devices → Register New PMU**) or via the API. Reference PMU definitions are in `config/pmus.yaml` and `config/pmus_3.yaml`.

## Dashboard pages

| Page | Description |
|------|-------------|
| **Overview** | System KPIs, frequency trend, map, alerts, regional health |
| **Devices** | PMU inventory, filters, region/voltage/vendor charts |
| **Data Frames** | Live frame details, phasor quantities, frequency & ROCOF chart |
| **Connectivity** | RTT chart, per-PMU matrix, recommendations |
| **Analytics** | Angle differences, oscillation detection, operator recommendations |
| **Help / Docs** | In-app documentation |

## Stop services

- PDC / dashboard / simulators: `Ctrl+C` in each terminal
- Docker stack: `docker compose down`

## Configuration

### PMU reference configs

- `config/pmus.yaml` — single simulator
- `config/pmus_3.yaml` — three simulators

These YAML files are reference definitions. Runtime PMU config is persisted in InfluxDB and managed through the dashboard or REST API.

### Environment variables (pipeline)

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | `127.0.0.1:9093` | Kafka broker list |
| `KAFKA_TOPIC` | `pmu.readings` | Publish topic |
| `KAFKA_ASYNC` | `true` | Non-blocking Kafka writes |
| `KAFKA_BATCH_SIZE` | `500` | Messages per Kafka batch |
| `REDIS_ADDR` | `127.0.0.1:6380` | Redis address |
| `INFLUX_URL` | `http://127.0.0.1:8087` | InfluxDB URL |
| `INFLUX_BATCH_SIZE` | `500` | Points per Influx batch |
| `SINK_CHANNEL_SIZE` | `8192` | Buffered sink queue depth |
| `SINK_WORKERS` | `4` | Parallel Redis/Influx writers |
| `FRAME_HANDLER_MAX_INFLIGHT` | `128` | Max concurrent frame handlers |

### Kafka topic quick fix

If you see `Unknown Topic Or Partition`:

```bash
docker compose exec kafka kafka-topics --create --if-not-exists --topic pmu.readings --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1
```

## No-data-loss design (heavy load)

The pipeline is built for high frame rates (50 fps × many PMUs):

1. **Per-PMU receivers** with auto-reconnect
2. **Non-blocking frame dispatch** — if the handler pool is full, frames are dropped with a metric instead of stalling the TCP read loop
3. **Decoupled sink** — Kafka publish returns immediately; Redis/Influx writes run in a buffered channel with worker pool
4. **Batched InfluxDB writes** — non-blocking WriteAPI with internal batching and retry
5. **Pipelined Redis** — ZAdd + Expire + Set in one round-trip
6. **Disk spool** — failed Kafka/sink writes are buffered to `data/spool/` and replayed automatically
7. **Prometheus metrics** — frame drops, spool backlog, processing latency

Recommended settings for heavy load:

```powershell
$env:KAFKA_ASYNC="true"
$env:KAFKA_BATCH_SIZE="500"
$env:KAFKA_BATCH_TIMEOUT_MS="50"
$env:INFLUX_BATCH_SIZE="500"
$env:INFLUX_FLUSH_INTERVAL_MS="1000"
$env:SINK_CHANNEL_SIZE="8192"
$env:SINK_WORKERS="4"
$env:FRAME_HANDLER_MAX_INFLIGHT="256"
$env:SPOOL_FSYNC="true"
go run . -metrics-addr :2112 -api-addr :8080
```

## Production dashboard build

```bash
cd dashboard
npm run build
npm run preview
```

For production, serve the `dashboard/dist` output behind a reverse proxy that forwards `/conversation`, `/api`, and `/metrics` to the Go backend.

## Notes

- The application uses `127.0.0.1` defaults to avoid Windows localhost resolution issues.
- If you run multiple PDC instances, use separate spool file paths (`KAFKA_SPOOL_FILE`, `SINK_SPOOL_FILE`).
- A single Kafka broker is a single point of failure; use replication (3 brokers, RF ≥ 3, min ISR ≥ 2) for production.
