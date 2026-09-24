# output/

Two different jobs live here:

| File | Role |
|------|------|
| `frame_capture.go` | **Active** — short in-memory tape of recent frames (CSV dump). |
| `sink.go` | **Parked** — Redis latest + Postgres history writer. |
| `spool.go` | **Parked** — local durable queue if the DB is briefly down. |
| `postgres/history.go` | **Parked** — TimescaleDB insert batcher. |

The live pipeline does **not** call the sink. When you re-enable storage later, wire `NewSinkFromEnv` from `main.go` again; Redis + Timescale must be up (`docker compose up -d redis timescaledb`).
