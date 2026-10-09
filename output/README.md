# output/

| File | Role |
|------|------|
| `frame_capture.go` | **Active** — short in-memory tape of recent frames (CSV dump). |
| `sink.go` | History sink adapter (`ENABLE_HISTORY=true`). Wired for CFG/events/frames when enabled. |
| `postgres/writer.go` | **Active** — bounded queue + `CopyFrom` Timescale writer (dedicated pool, reconnect). |
| `spool.go` | **Parked** — local durable queue if the DB is briefly down (phase 2+ later). |

Env (history writer) — prefer `.env` (see `.env.example`); OS env still wins:

- `ENABLE_HISTORY` (or legacy `ENABLE_SINK`) — default **off**
- `HISTORY_QUEUE_SIZE` (8192), `HISTORY_BATCH_SIZE` (500), `HISTORY_FLUSH_INTERVAL_MS` (1000)
- `HISTORY_INSERT_TIMEOUT_MS` (15000), `HISTORY_RECONNECT_MS` (5000)
- `POSTGRES_DSN` — same as address book

`main` loads `.env` at startup via `internal/envfile` (missing file is OK).
