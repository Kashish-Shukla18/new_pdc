# PDC — Phasor Data Concentrator

A program that listens to power-grid sensors (PMUs), checks their data, lines
the timestamps up, and shows it on a live dashboard.

## What it does (and what it does not)

```text
PMU ──network──► connect ──► parse ──► quality check ──┬──► live inventory ──► dashboard
                                                       │
                                                       └──► per-PMU timestamp buffers
                                                            └── publish tick grid ──► alignedBatches ──► analytics charts
```

- **Does:** connect, parse (CFG-2 from the live handshake), quality-check, live dashboard, time-aligned analytics (no wait; head = slowest live PMU)
- **Does not (yet):** save every reading to Redis/Timescale — that code stays in `output/` as parked storage

## Folders (simple map)

| Folder | Plain-English job |
|--------|-------------------|
| `receiver/` | Call the PMU and keep the connection alive |
| `parser/` | Turn binary frames into frequency / phasors (layout from CFG-2 handshake) |
| `aligner/` | Quality check + per-PMU timestamp buffers + tick publish for charts |
| `manager/` | Start/stop receivers when you add/remove a PMU |
| `store/` | Address book of PMUs in Postgres |
| `api/` | REST endpoints the React UI uses for that address book |
| `monitoring/` | Live dashboard data (SSE / JSON on `:2112`) |
| `output/` | Frame dump tape (active) + Redis/Postgres sink (parked, kept) |
| `dashboard/` | React website |
| `cmd/dump-frames/` | Save the last N frames to CSV |
| `sql/` | Creates the PMU address-book table on first boot |

## Start

**1. Postgres** (address book):

```powershell
docker compose up -d timescaledb
```

**2. PDC:**

```powershell
.\start.ps1
```

**3. Dashboard:**

```powershell
cd dashboard
npm install   # first time
npm run dev
```

| What | URL |
|------|-----|
| Dashboard | http://localhost:5173 |
| Live data / metrics | http://127.0.0.1:2112 |
| PMU config API | http://127.0.0.1:8081 |

Stop the PDC with Ctrl+C, or `.\stop.ps1` if an orphan is stuck.

## Frame dumps

```powershell
go run ./cmd/dump-frames -count 1500
```

## Optional

```powershell
docker compose up -d redis prometheus pgadmin
```

Redis is only needed when you re-enable the parked storage sink in `output/`.
