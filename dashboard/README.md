# PDC React Dashboard

The website operators use to watch PMUs.

Live numbers come from the Go PDC on `:2112`.
Adding/editing devices goes to the REST API on `:8081`.

## Run

```powershell
# from repo root first:
docker compose up -d timescaledb
.\start.ps1

# then here:
npm install
npm run dev
```

Open http://localhost:5173
