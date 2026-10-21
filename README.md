# PDC — Phasor Data Concentrator

## Objective

This project is a **Phasor Data Concentrator (PDC)** for IEEE C37.118 synchrophasor streams.

It connects to one or more PMUs (or lab simulators), learns each device’s channel layout from the live CFG-2 handshake, validates incoming DATA frames, time-aligns measurements across devices, and presents a live operator dashboard. Optionally it can persist CFG versions, stream events, and DATA frames to TimescaleDB for history.

**In short:** turn raw PMU streams into a reliable live view (and optional history) for monitoring, comparison, and operations.

---

## What it does

- Connects to PMUs over **IEEE C37.118.2** (TCP or UDP)
- Performs CFG-2 handshake and parses DATA frames
- Applies quality checks (clock skew, STAT, frequency sanity)
- Time-aligns multi-PMU samples for charts (no-wait tick grid)
- Serves a React dashboard (overview, devices, data frames, connectivity, analytics)
- Stores the PMU address book in Postgres/Timescale
- Optionally writes history when `ENABLE_HISTORY=true`

**Not included yet:** authentication on the APIs, TLS termination in-app, multi-instance HA. Treat local ports as lab/dev surfaces unless you put them behind your own network controls.

---

## Architecture (high level)

```text
PMUs / simulators (C37.118)
        │
        ▼
   Receiver ──► Parse ──► Quality
        │                    │
        │                    ├─► Live dashboard state (:2112)
        │                    ├─► Time aligner ──► aligned charts
        │                    ├─► Frame capture (in-RAM dump tape)
        │                    └─► History writer (optional, Timescale)
        │
   Manager ◄── Config API (:8081) ◄── Device Inventory (dashboard)
        │
        ▼
   Postgres / Timescale  (address book + optional history)
```

| Layer | Role |
| ----- | ---- |
| Receiver | Dial PMU, handshake, keep the stream alive |
| Parser | CFG-2 profile + DATA decode |
| Quality + aligner | Gate bad samples; align timestamps for multi-PMU charts |
| Manager + API | Start/stop streams when devices are added or removed |
| Store | Address book in Postgres |
| Monitoring | Live state, latency hops, metrics (`:2112`) |
| Output | In-RAM frame dumps; optional Timescale history |
| Dashboard | Operator UI (`:5173` in dev) |

---

## Repository layout

| Path | Purpose |
| ---- | ------- |
| `main.go` | Wires the pipeline and starts services |
| `receiver/` | PMU connection and stream I/O |
| `parser/` | C37.118 CFG-2 / DATA parsing |
| `aligner/` | Quality checks, buffers, tick publisher |
| `manager/` | Lifecycle of configured vs live PMUs |
| `store/` | Address-book persistence |
| `api/` | REST API for device inventory (`:8081`) |
| `monitoring/` | Dashboard feed, metrics, latency |
| `output/` | Frame capture + optional history writer |
| `config/` | PMU config helpers (including timestamp TZ) |
| `dashboard/` | React + Vite UI |
| `sql/` | First-boot DB schema for Docker |
| `cmd/dump-frames/` | Helper to export recent frames to CSV |
| `docker-compose.local.yaml` | Local Timescale (and optional tools) |
| `docker-compose.yaml` | Server/EC2 Timescale (localhost-bound) |
| `start.ps1` / `stop.ps1` | Start / stop the PDC on Windows |

---

## Prerequisites

- **Go** 1.22+ (module targets a recent Go toolchain)
- **Node.js** 20+ (for the dashboard)
- **Docker** (for TimescaleDB)
- **Git**
- Windows PowerShell recommended for `start.ps1` / `stop.ps1` (Linux/macOS: run `go run .` directly)

---

## Clone and run

### 1. Clone

```bash
git clone https://github.com/Kashish-Shukla18/new_pdc.git
cd new_pdc
```

### 2. Environment

```powershell
copy .env.example .env
```

Edit `.env`. Minimum for local laptop:

```env
POSTGRES_DSN=postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable
POSTGRES_RECONNECT_SEC=5s

# Optional history (CFG / events / DATA frames → Timescale)
ENABLE_HISTORY=false

# How measurement SOC is interpreted → UTC (fleet default).
# Prefer UTC for GPS/true-UTC devices; use Asia/Kolkata or IST when devices
# paint local wall time into SOC. Per-device override can be set later in inventory.
PMU_TIMESTAMP_TZ=UTC
```

Notes:

- `.env` is gitignored. Real OS environment variables override `.env`.
- Local Docker publishes Timescale on **host port 5433**.
- Server compose (`docker-compose.yaml`) uses **127.0.0.1:5432** — set `POSTGRES_DSN` accordingly and provide `POSTGRES_PASSWORD`.

Common optional knobs (see `.env.example`):

| Variable | Meaning |
| -------- | ------- |
| `ENABLE_HISTORY` | Persist frames/events to Timescale (`true`/`false`) |
| `PMU_TIMESTAMP_TZ` | Fleet default for SOC reinterpretation |
| `PMU_STARTUP_STAGGER` | Delay between starting PMUs at boot (`0s` = all at once) |
| `MAX_CLOCK_SKEW` | Quality gate for measurement vs receive time |
| `DROP_QUALITY_REJECTED` | Drop bad frames from the live/align path |

### 3. Docker (database)

**Local development** (required service only):

```powershell
docker compose -f docker-compose.local.yaml up -d timescaledb
```

Wait until healthy, then confirm:

```powershell
docker compose -f docker-compose.local.yaml ps
```

Optional tools (pgAdmin on `:5050`, Prometheus on `:9090`):

```powershell
docker compose -f docker-compose.local.yaml up -d pgadmin prometheus
```

**Server / EC2:**

```bash
export POSTGRES_PASSWORD='choose-a-strong-password'
docker compose up -d
```

Then point `POSTGRES_DSN` at `127.0.0.1:5432` with that password.

### 4. Start the PDC

```powershell
.\start.ps1
```

This runs `go run .` in the foreground (Ctrl+C to stop). If a previous instance is stuck:

```powershell
.\stop.ps1
```

On Linux/macOS:

```bash
go run .
```

### 5. Start the dashboard

```powershell
cd dashboard
npm install
npm run dev
```

Open **http://localhost:5173**

---

## URLs

| What | URL |
| ---- | --- |
| Dashboard | http://localhost:5173 |
| Live state / metrics | http://127.0.0.1:2112 |
| PMU config API | http://127.0.0.1:8081 |

Flags (optional):

```powershell
.\start.ps1 -metrics-addr=:2112 -api-addr=:8081
```

---

## Using the app

1. Open **Device Inventory** and add a PMU (IP, port, protocol).
2. Connect — the PDC handshakes, learns CFG-2, and starts DATA.
3. Use **Overview** / **Analytics** for live and time-aligned views.
4. Use **Connectivity** when latency or loss looks wrong.
5. Use **Data Frames** to inspect CFG/DATA decode for a stream.

Measurement timezone defaults to `PMU_TIMESTAMP_TZ`. Per-device `timestamp_tz` is stored on the address-book row when set via the API.

---

## Frame dumps (optional)

Export recent in-memory frames to CSV under `data/`:

```powershell
go run ./cmd/dump-frames -count 1500
```

---

## Stop

| Component | How |
| --------- | --- |
| PDC | Ctrl+C in the `start.ps1` terminal, or `.\stop.ps1` |
| Dashboard | Ctrl+C in the `npm run dev` terminal |
| Docker DB | `docker compose -f docker-compose.local.yaml down` (add `-v` only if you want to wipe data) |

---

## License / status

Lab / research PDC. Harden network exposure (bind, auth, TLS) before production use on shared networks.
