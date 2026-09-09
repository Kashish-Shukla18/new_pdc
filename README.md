# PDC

IEEE C37.118 phasor data concentrator. It receives PMU streams, parses frames in order, stores live/history data, and serves a React dashboard.

## Requirements

- Docker Desktop
- Go 1.26+
- Node.js

## Start

From PowerShell in the project directory:

```powershell
.\start.ps1
```

The script is safe to run again. It starts Redis and TimescaleDB, replaces an existing PDC process with a fresh build, and starts the dashboard if needed.

| Service | URL |
|---------|-----|
| Dashboard | http://localhost:5173 |
| Metrics and live API | http://127.0.0.1:2112 |
| PMU configuration API | http://127.0.0.1:8081 |

If PowerShell blocks local scripts:

```powershell
powershell -ExecutionPolicy Bypass -File .\start.ps1
```

## Stop

```powershell
.\stop.ps1
```

This stops the PDC and dashboard but keeps the Docker data services running.

To stop Docker too:

```powershell
docker compose down
```

## Manual start

Use this only when you need separate terminals:

```powershell
docker compose up -d redis timescaledb
go build -o pdc.exe .
.\pdc.exe
```

In another terminal:

```powershell
cd dashboard
npm install
npm run dev
```

Only one PDC may run at a time. Also close other PMU DATA clients, such as Connection Tester, before connecting the PDC.

## Architecture

```text
PMUs ──TCP/UDP──► Receiver ──► Parser + quality check
                                      ├──► Dashboard state + SSE
                                      ├──► Redis (latest)
                                      └──► TimescaleDB (history)
```

The default `direct` mode does not use Kafka. Kafka support remains optional for split/scaled deployments and can be enabled with `KAFKA_ENABLED=true`.

## Frame dumps

```powershell
go run ./cmd/dump-frames -count 1500
```

This overwrites:

- `data/last_1500_combined_raw.csv`
- `data/last_1500_combined_parsed.csv`

Use custom names to preserve an existing dump:

```powershell
go run ./cmd/dump-frames -count 1500 `
  -raw-out data/my_raw.csv `
  -parsed-out data/my_parsed.csv
```

## Verify

```powershell
Invoke-RestMethod http://127.0.0.1:8081/api/pmus
Invoke-RestMethod http://127.0.0.1:2112/conversation/state
go test ./...
```

## Optional services

Start these only when needed:

```powershell
docker compose up -d kafka zookeeper prometheus pgadmin
```

Kafka modes are still available through `-mode=all`, `-mode=ingress`, and `-mode=processor`.
