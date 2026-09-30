# PDC — Phasor Data Concentrator

A program that listens to power-grid sensors (PMUs), checks their data, lines
the timestamps up, and shows it on a live dashboard.

## Architecture (wire diagram)

Everything below is what runs **today**. Layout (CFG-2) always comes from the
live PMU handshake — never from static config files on disk.

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│  SOURCES                                                                      │
│  Lab / field PMUs or simulators · IEEE C37.118.2 over TCP (or UDP)            │
└────────────────────────────────┬─────────────────────────────────────────────┘
                                 │  DATA frames (0xAA…)
                                 │  CFG-2 on handshake
                                 ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  RECEIVER  (receiver/)                                                         │
│  • Dial IP:port · exclusive instance lock (port 21119)                        │
│  • Handshake: CMD_SEND_CFG2 (retry until reply) → CMD_DATA_ON                 │
│  • Stream: read each DATA frame + wall-clock receivedAt                       │
│  • CRC via parser.CRC16                                                       │
└────────────────────────────────┬─────────────────────────────────────────────┘
                                 │  raw bytes + receivedAt
                                 ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  PIPELINE  (main.go HandleFrame)                                               │
│                                                                                │
│   1. PARSE     parser.ParseDataFrame                                           │
│                uses Profile from CFG-2 (phasors, Analog1…N, freq, STAT)        │
│                                                                                │
│   2. QUALITY   aligner.Checker (clock skew / STAT sanity)                      │
│                                                                                │
│   3. SPLIT ───► A) live path          B) align path          C) dump tape      │
└───────────────┬───────────────────────┬───────────────────────┬───────────────┘
                │                       │                       │
                ▼                       ▼                       ▼
┌───────────────────────┐  ┌────────────────────────┐  ┌────────────────────────┐
│ A) LIVE INVENTORY     │  │ B) TIME ALIGNER        │  │ C) FRAME CAPTURE       │
│ monitoring/           │  │ aligner/               │  │ output/frame_capture   │
│                       │  │                        │  │                        │
│ RecordReading         │  │ Bank: per-PMU map      │  │ Last N frames in RAM   │
│ • lastChannels        │  │   key = SOC+FRACSEC ms │  │ dump: cmd/dump-frames  │
│ • trends / FPS        │  │   value = Reading      │  │ CSV under data/        │
│ • lastHops (5 stages) │  │   cap ~50–200 slots    │  └────────────────────────┘
│                       │  │                        │
│ /conversation/state   │  │ Publisher (no wait):   │
│ /conversation/latency │  │   tick grid @ CFG rate │
│ Prometheus :2112      │  │   head = MinNewest     │
└───────────┬───────────┘  │   emit present/missing │
            │              │                        │
            │              │ RecordAlignedFrame ──► alignedBatches[]            │
            │              │                        │
            │              │ Inspect:               │
            │              │  /conversation/        │
            │              │   aligner-buffers/     │
            │              │   sheets (.xls/HTML)   │
            │              └────────────┬───────────┘
            │                           │
            └─────────────┬─────────────┘
                          ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│  DASHBOARD  (dashboard/ — React + Vite)                                        │
│  http://localhost:5173  →  proxies /conversation → :2112                       │
│                                                                                │
│  Overview     frequency / trends from alignedBatches (gaps = holes)            │
│  Analytics    VA/VB/VC + CFG analogs (Analog1…) from alignedBatches            │
│               phasor diagrams from lastChannels                                │
│  Connectivity five hop stages (see Latency below)                              │
│  Devices     add/edit PMUs via config API                                     │
└──────────────────────────────────────────────────────────────────────────────┘
                          ▲
                          │ REST
┌─────────────────────────┴────────────────────────────────────────────────────┐
│  PMU ADDRESS BOOK                                                              │
│  store/ + api/  ←→  Postgres/Timescale  (docker: timescaledb :5433)            │
│  Config API: http://127.0.0.1:8081                                             │
│  manager/ starts/stops receivers when you add/remove a PMU                     │
└──────────────────────────────────────────────────────────────────────────────┘

PARKED (not wired into the live path)
  output/sink.go + output/postgres/  →  Timescale history writer (re-enable later)
```

### Latency hops (what we measure)

Only five stages — enough to tell “PMU period” from “PDC work”:


| Stage            | ID                      | Meaning                                                             |
| ---------------- | ----------------------- | ------------------------------------------------------------------- |
| Frame gap        | `frame_gap`             | Idle time between DATA frames (~40 ms @ 25 FPS). **Not** PDC delay. |
| Parse            | `parse`                 | Decode DATA with CFG-2 profile.                                     |
| Align wait       | `align_wait`            | Sample sat in buffer until tick emit (head lag).                    |
| Dashboard record | `dashboard_record`      | Write into live inventory.                                          |
| End-to-end       | `e2e_recv_to_dashboard` | Frame complete → dashboard recorded.                                |


### Align publish rule (simple)

```text
  For each tick T on a ruler (period = 1000 / CFG DATA_RATE ms):

      do not advance past MinNewest (slowest live PMU)
      collect every PMU sample near T (± half period)
      if nobody has data near T → skip tick (no fake hole)
      else emit { present, missing, complete } → charts
```

No wait timer: as soon as the slowest stream reaches T, we emit what we have.

### What we do / do not

- **Does:** connect, CFG-2 handshake, parse, quality-check, live dashboard, time-aligned analytics charts, in-RAM frame dumps, PMU CRUD in Postgres.
- **Does not (yet):** persist every reading to Timescale via the parked sink.

## Folders (simple map)


| Folder             | Plain-English job                                         |
| ------------------ | --------------------------------------------------------- |
| `receiver/`        | Call the PMU, handshake, keep the stream alive            |
| `parser/`          | CFG-2 profile + DATA decode (CRC, phasors, analogs)       |
| `aligner/`         | Quality gate + per-PMU timestamp buffers + tick publisher |
| `manager/`         | Start/stop receivers when PMUs are added/removed          |
| `store/`           | PMU address book in Postgres                              |
| `api/`             | REST for that address book (`:8081`)                      |
| `monitoring/`      | Live state, alignedBatches, latency hops, SSE (`:2112`)   |
| `output/`          | Frame dump tape (active) + Postgres history sink (parked) |
| `dashboard/`       | React UI                                                  |
| `cmd/dump-frames/` | Save the last N frames to CSV                             |
| `sql/`             | Creates the PMU address-book table on first boot          |


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


| What                                 | URL                                                                                                                    |
| ------------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| Dashboard                            | [http://localhost:5173](http://localhost:5173)                                                                         |
| Live data / metrics / aligner sheets | [http://127.0.0.1:2112](http://127.0.0.1:2112)                                                                         |
| PMU config API                       | [http://127.0.0.1:8081](http://127.0.0.1:8081)                                                                         |
| Aligner buffer sheets                | [http://127.0.0.1:2112/conversation/aligner-buffers/sheets](http://127.0.0.1:2112/conversation/aligner-buffers/sheets) |


Stop the PDC with Ctrl+C, or `.\stop.ps1` if an orphan is stuck.

## Frame dumps

```powershell
go run ./cmd/dump-frames -count 1500
```

## Optional

```powershell
docker compose up -d prometheus pgadmin
```

