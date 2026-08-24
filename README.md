# PDC

Go-based synchrophasor **Phasor Data Concentrator**. It speaks IEEE C37.118 to PMUs, buffers and fans out data through Kafka, stores live state in Redis and history in **Postgres/TimescaleDB**, and exposes a real-time React operator dashboard.

Designed for high rate ingest (e.g. **50+ PMUs × ~100 samples/s**): Kafka absorbs burst/backlog; independent consumer groups feed the dashboard and storage without sharing one in-process queue.

---

## Architecture

```
PMU ──TCP/UDP C37.118──► Ingress
                           │  handshake (HDR → CFG2 → DATA_ON)
                           │  publish raw frames (keyed by PMU name)
                           ▼
                    Kafka: pmu.raw.frames
                           │  consumer group: pdc-processor
                           ▼
                    Processor
                           │  parse DATA, register CFG2/HDR profiles
                           │  quality gate → time-align buffer → publish JSON readings
                           ▼
                    Kafka: pmu.readings
                           │
           ┌───────────────┴───────────────┐
           ▼                               ▼
   group: pdc-dashboard            group: pdc-sink
   → in-memory SSE bus             → buffered sinkCh
   → React live UI                 → Redis (live) + Postgres/Timescale (history)
```

| Layer | Role |
|-------|------|
| **Ingress** | Owns the C37.118 TCP/UDP session; publishes **raw** frames to Kafka |
| **`pmu.raw.frames`** | Durable shock absorber between ingest and parse (hash-partitioned by PMU) |
| **Processor** | Consumes raw frames; parses; quality-checks; publishes **parsed** readings |
| **`pmu.readings`** | Fan-out bus for any number of consumer groups |
| **`pdc-dashboard`** | Feeds the live SSE / conversation state used by the React UI |
| **`pdc-sink`** | Writes **Redis** (shared live latest) + **Postgres/Timescale** (history) |
| **Redis** | Shared live-state store; hydrates dashboard after restart; enables multi-instance scale-out |
| **Postgres / TimescaleDB** | Time-series history (`pmu_readings`) + PMU config (`pmu_config`) |
| **Disk spools** | Local JSONL retry if Kafka publish or sink store fails |
| **CFG2 profile store** | Persisted channel layouts under `data/profiles/` so parse works after restart |

Kafka cannot sit *on the wire* between a PMU and the PDC: C37.118 needs a live session. Ingress is that thin TCP owner; Kafka sits immediately after it.

---

## Run modes

Set with `-mode` or `PDC_MODE` (default: `all`).

| Mode | Starts | Requires Kafka |
|------|--------|----------------|
| **`all`** | Ingress + raw consumer + readings publisher + dashboard/sink consumers | Yes |
| **`ingress`** | TCP/UDP to PMUs → `pmu.raw.frames` only | Yes |
| **`processor`** | `pmu.raw.frames` → parse → `pmu.readings` → dashboard + sink consumers | Yes |
| **`direct`** | TCP → parse in-process (skips raw Kafka). Readings fan-out still used if Kafka is up | Optional |

Split example (two processes):

```powershell
# A — own all PMU sockets
.\pdc.exe -mode=ingress -metrics-addr :2112 -api-addr :8081

# B — parse + fan-out (different ports)
.\pdc.exe -mode=processor -metrics-addr :2113 -api-addr :8082
```

---

## Quick start

### 1. Supporting stack

```bash
docker compose up -d
```

Wait ~15s for Kafka. Host ports:

| Service | Port |
|---------|------|
| Kafka (external) | `localhost:9093` |
| Redis | `localhost:6380` |
| Postgres / TimescaleDB | `localhost:5433` (user/pass/db: `pdc` / `pdc` / `pdc`) |
| pgAdmin | `http://localhost:5050` (login `admin@example.com` / `admin`) |
| Prometheus | `http://localhost:9090` |

Operator UI is the **React dashboard** (`dashboard/`).

### View history in local Postgres / pgAdmin

**pgAdmin (browser):** open `http://localhost:5050` → login `admin@example.com` / `admin` → Register server:

| Field | Value |
|-------|--------|
| Name | `pdc` |
| Host | `timescaledb` (from inside Docker) or `host.docker.internal` |
| Port | `5432` (internal) — from host use `localhost` + **5433** |
| Username | `pdc` |
| Password | `pdc` |
| Database | `pdc` |

Then open **Servers → pdc → Databases → pdc → Schemas → public → Tables → `pmu_readings` / `pmu_config`** → right-click → **View/Edit Data → First 100 Rows**, or **Tools → Query Tool**.

**CLI:**

```powershell
docker compose exec timescaledb psql -U pdc -d pdc
# SELECT time, pmu, frequency, rocof FROM pmu_readings ORDER BY time DESC LIMIT 20;
```

Sample queries: `sql/queries.sql`. DSN: `postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable`

**Note:** Re-add PMUs via the dashboard / `POST /api/pmus` after a fresh DB (or seed `pmu_config`).

### 2. PDC

```powershell
go build -o pdc.exe .
.\pdc.exe -mode=all -metrics-addr :2112 -api-addr :8081
```

| Endpoint | Address |
|----------|---------|
| Metrics + SSE / conversation API | `http://127.0.0.1:2112` |
| REST API (PMU CRUD) | `http://127.0.0.1:8081` |

PMU connections load from Postgres (`pmu_config`) on startup and can be added at runtime via the dashboard or `POST /api/pmus`.

### 3. React dashboard

```bash
cd dashboard
npm install
npm run dev
```

Open **http://localhost:5173**. Vite proxies:

| Path | Backend |
|------|---------|
| `/conversation/*`, `/metrics` | `:2112` |
| `/api/*` | `:8081` |

### 4. Optional PMU simulators

```bash
python stimulator.py --tcp-port 4712 --udp-port 4713
# or
python run_3_simulators.py
```

Register each device in **Devices → Register New PMU**, or via the API. Reference YAML: `config/pmus.yaml`, `config/pmus_3.yaml`.

---

## Package map

| Path | Responsibility |
|------|----------------|
| `main.go` | Modes, pipeline, readings fan-out wiring |
| `receiver/` | C37.118 connect, handshake, frame read; ingress publish |
| `manager/` | Start/stop PMU receivers dynamically |
| `parser/` | CFG2 profiles, DATA frame → `Reading`; disk hydrate of profiles |
| `aligner/` | Timestamp / quality gate |
| `output/raw_kafka.go` | Raw frame producer + consumer |
| `output/kafka.go` | Parsed readings publisher |
| `output/readings_consumer.go` | Readings consumers (dashboard / sink groups) |
| `output/sink.go` | Redis + Postgres sink |
| `output/postgres.go` | Batched Timescale/Postgres history writer |
| `output/spool.go` | Disk JSONL retry queues |
| `data/profiles/` | Persisted CFG2 layouts (runtime; gitignored) |
| `monitoring/` | Prometheus metrics + SSE conversation bus |
| `api/` | REST PMU management |
| `store/` | Postgres-backed PMU config store |
| `dashboard/` | React operator UI |
| `cmd/` | Utilities (`probe-pmu`, `capture-pmu`, …) |

---

## Dashboard pages

| Page | Description |
|------|-------------|
| **Overview** | KPIs, frequency trend, map, alerts |
| **Devices** | Inventory, filters, region/vendor charts |
| **Data Frames** | Live frame details, phasors, frequency & ROCOF |
| **Connectivity** | RTT, per-PMU matrix, recommendations |
| **Analytics** | Angle differences, oscillation hints |
| **Help / Docs** | In-app documentation |

---

## Configuration

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-mode` | `all` (`PDC_MODE`) | `all` / `ingress` / `processor` / `direct` |
| `-metrics-addr` | `:2112` | Prometheus + `/conversation/*` |
| `-api-addr` | `:8081` | REST API |

### Environment — Kafka (raw buffer)

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | `127.0.0.1:9093` | Broker list |
| `KAFKA_RAW_TOPIC` | `pmu.raw.frames` | Raw C37.118 frames |
| `KAFKA_RAW_GROUP` | `pdc-processor` | Processor consumer group |
| `KAFKA_RAW_ASYNC` | `false` | Sync publish → backpressure on TCP instead of drops |
| `KAFKA_RAW_TOPIC_PARTITIONS` | `6` | Partitions when auto-creating raw topic |
| `KAFKA_RAW_BATCH_SIZE` | (inherits `KAFKA_BATCH_SIZE`) | Producer batch size |
| `KAFKA_RAW_BATCH_TIMEOUT_MS` | `20` | Producer batch linger |

### Environment — Kafka (parsed fan-out)

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_TOPIC` | `pmu.readings` | Parsed JSON readings |
| `KAFKA_TOPIC_PARTITIONS` | `3` | Partitions when auto-creating readings topic |
| `KAFKA_READINGS_FANOUT` | `true` | Dashboard + sink consume from Kafka |
| `KAFKA_DASHBOARD_GROUP` | `pdc-dashboard` | Live UI consumer group |
| `KAFKA_SINK_GROUP` | `pdc-sink` | Redis/Postgres consumer group |
| `KAFKA_ASYNC` | `true` | Async readings producer |
| `KAFKA_BATCH_SIZE` | `500` | Readings producer batch |
| `KAFKA_BATCH_TIMEOUT_MS` | `50` | Readings producer linger |
| `KAFKA_REQUIRED_ACKS` | all | `none` / `one` / `all` |
| `KAFKA_ENSURE_TOPIC` | `true` | Auto-create topics on startup |
| `KAFKA_TOPIC_REPLICATION_FACTOR` | `1` | RF for auto-created topics (compose is single-broker) |

Both producers use a **hash balancer on PMU name** so CFG2 and DATA for one device stay ordered on one partition.

### Environment — sink & quality

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_SINK` | `true` | Redis + Postgres writers |
| `REDIS_ADDR` | `127.0.0.1:6380` | Redis (shared live latest + timeline) |
| `REDIS_TTL_SECONDS` | `120` | TTL for `:latest` / `:timeline` keys |
| `POSTGRES_DSN` | `postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable` | History + config DB |
| `POSTGRES_BATCH_SIZE` | `500` | COPY batch size |
| `POSTGRES_FLUSH_INTERVAL_MS` | `1000` | Max time before a partial batch flushes |
| `SINK_CHANNEL_SIZE` | `8192` | In-process queue before Redis/Postgres |
| `SINK_WORKERS` | `4` | Parallel store workers |
| `MAX_CLOCK_SKEW` | `24h` | Quality gate clock skew |
| `DROP_QUALITY_REJECTED` | `false` | If `false`, warn and continue (no data loss) |
| `TIME_ALIGN_ENABLED` | `true` | Multi-PMU in-memory time alignment before fan-out |
| `TIME_ALIGN_MODE` | `relative` | `relative` (wait from first arrival) or `absolute` (wait from SOC) |
| `TIME_ALIGN_WAIT` | `200ms` | Wait cutoff before emitting a partial set (missing = absent) |
| `TIME_ALIGN_KEY` | `corrected` | Bucket axis: `corrected` (SOC+clock offset), `soc` (raw GPS), or `receive` |
| `TIME_ALIGN_QUANTIZE` | `20ms` | Snap bucket keys to this grid (use ~33ms for 30 FPS) |
| `TIME_ALIGN_BUFFER_DEPTH` | `50` | Per-PMU history length + max open timestamp buckets |
| `TIME_ALIGN_ACTIVE_TTL` | `5s` | PMUs idle longer than this leave the expected set |
| `FRAME_HANDLER_MAX_INFLIGHT` | `128` | Direct-mode only; drop if pool full |
| `CFG2_PROFILE_PERSIST` | `true` | Persist CFG2 layouts to disk for restart recovery |
| `CFG2_PROFILE_DIR` | `data/profiles` | Directory for per-PMU profile JSON files |

### Environment — spools

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_SPOOL_FILE` | `data/spool/kafka_failed.jsonl` | Failed readings publishes |
| `SINK_SPOOL_FILE` | `data/spool/sink_failed.jsonl` | Failed Redis/Postgres stores |
| `KAFKA_REPLAY_ENABLED` | `true` | Replay readings spool → Kafka |
| `SINK_REPLAY_ENABLED` | `true` | Replay sink spool → store |
| `SPOOL_FSYNC` | `true` | fsync on append |

### Topic bootstrap (if auto-create fails)

```bash
docker compose exec kafka kafka-topics --create --if-not-exists --topic pmu.raw.frames --bootstrap-server localhost:9092 --partitions 6 --replication-factor 1
docker compose exec kafka kafka-topics --create --if-not-exists --topic pmu.readings --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1
```

---

## Observability

### HTTP (metrics process, default `:2112`)

| Path | Purpose |
|------|---------|
| `/metrics` | Prometheus scrape |
| `/conversation/state` | Snapshot for dashboard |
| `/conversation/events` | SSE live stream |
| `/conversation/recent` | Recent conversation events |
| `/conversation` | Small built-in HTML debug page |

Prometheus scrapes `host.docker.internal:2112` (see `prometheus/prometheus.yml`).

### Important metrics

| Metric | Meaning |
|--------|---------|
| `pdc_raw_frames_published_total` | Ingress → raw Kafka |
| `pdc_raw_frames_consumed_total` | Processor ← raw Kafka |
| `pdc_frames_parsed_total` | Successful DATA parses |
| `pdc_readings_consumed_total` | Fan-out consumes (**≈ 2× parsed** with both groups) |
| `pdc_frames_dropped_total` | Direct-mode handler-pool drops |
| `pdc_queue_publish_errors_total` | Kafka publish failures |
| `pdc_store_errors_total` | Redis/Postgres / sink-channel pressure |
| `pdc_spool_queued_total` / `pdc_spool_replayed_total` | Disk spool activity |
| `pdc_frame_processing_seconds` | Latency (PMU timestamp → sink path) |
| `pdc_sink_inflight` | Readings waiting in `sinkCh` |

---

### Redis role (keep + wire)

Redis is **kept and wired** as the shared live-state layer (not optional decoration):

| Path | Role |
|------|------|
| Kafka `pdc-sink` → Redis `:latest` | Full JSON `Reading` per PMU (TTL, default 120s) |
| Redis `:timeline` | Compact freq/power points for a short window |
| Startup hydrate | PDC loads all `:latest` keys into the dashboard bus so UI recovers after restart |
| Kafka `pdc-dashboard` | Still pushes live SSE updates in-process (low latency) |

**Why keep Redis (scaling):** multiple API/UI processes cannot each hold a full fleet view from one Kafka consumer group (partitions split the fleet). A shared Redis latest-store lets any instance read “current state for all PMUs.” Postgres/Timescale remains history; Kafka remains the buffer/fan-out bus.

**Why not drop:** you already pay the write cost; without Redis (or an equivalent), scale-out of the live UI needs either N full Kafka consumer groups or sticky single-process memory.  
**Why not leave write-only:** zero benefit for operators or multi-instance deploy.

## Reliability notes

1. **CFG2 profiles are persisted to disk** (`data/profiles/` by default). After a processor restart, DATA frames can still be parsed without waiting for a new PMU handshake. A fresh CFG2 from Kafka/ingress still overwrites the stored profile when config changes.
2. **Ingress does not drop under Kafka lag by default** — `KAFKA_RAW_ASYNC=false` blocks the TCP read briefly (backpressure) instead of discarding frames.
3. **CFG2/HDR travel on `pmu.raw.frames`** ahead of DATA (same PMU key/partition) so the processor can rebuild profiles; disk hydrate covers the gap until that republish happens.
4. **Two readings consumer groups** each get a full copy of every message — that is intentional Kafka fan-out.
5. **Redis holds shared live latest** and is loaded into the dashboard bus on startup (`redis live-state hydrate`).
6. **If readings publish fails**, the pipeline falls back to local dashboard + sink delivery and spools for later Kafka replay.
7. **Compose Kafka is RF=1** — fine for local/dev; production needs multiple brokers and RF ≥ 2.
8. Use `127.0.0.1` defaults on Windows to avoid `localhost` resolution quirks.
9. Multiple PDC instances need distinct spool paths (`KAFKA_SPOOL_FILE`, `SINK_SPOOL_FILE`), and for split hosts each processor needs its own `CFG2_PROFILE_DIR` (populated from CFG2 it has consumed).

### Heavy-load starter settings (PowerShell)

```powershell
$env:PDC_MODE="all"
$env:KAFKA_RAW_ASYNC="false"
$env:KAFKA_ASYNC="true"
$env:KAFKA_BATCH_SIZE="500"
$env:KAFKA_BATCH_TIMEOUT_MS="50"
$env:POSTGRES_BATCH_SIZE="500"
$env:POSTGRES_FLUSH_INTERVAL_MS="1000"
$env:SINK_CHANNEL_SIZE="8192"
$env:SINK_WORKERS="4"
.\pdc.exe -metrics-addr :2112 -api-addr :8081
```

---

## Production dashboard build

```bash
cd dashboard
npm run build
npm run preview
```

Serve `dashboard/dist` behind a reverse proxy that forwards `/conversation`, `/api`, and `/metrics` to the Go backend (`:2112` / `:8081`).

---

## Stop

- PDC / dashboard / simulators: `Ctrl+C`
- Docker stack: `docker compose down`
