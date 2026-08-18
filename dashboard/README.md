# PDC React Dashboard

Operator dashboard for the PDC synchrophasor platform. Purpose-built React UI for live PMU monitoring, device management, and analytics.

Live data is fed by the Go PDC **`pdc-dashboard`** Kafka consumer group (SSE / conversation API on `:2112`). PMU CRUD goes through the REST API on `:8081`.

## Stack

- React 19 + TypeScript
- Vite 8
- Recharts (charts)
- React Leaflet (map)
- Lucide React (icons)

## Development

From this directory:

```bash
npm install
npm run dev
```

Open **http://localhost:5173**.

The Vite dev server proxies backend routes to the Go PDC service:

| Proxy path | Backend |
|------------|---------|
| `/conversation/*` | `http://127.0.0.1:2112` |
| `/metrics` | `http://127.0.0.1:2112` |
| `/api/*` | `http://127.0.0.1:8081` |

Start the supporting stack and PDC first (from repo root):

```powershell
docker compose up -d
go build -o pdc.exe .
.\pdc.exe -mode=all -metrics-addr :2112 -api-addr :8081
```

## Pages

- **Overview** — KPIs, frequency chart, map, alerts
- **Devices** — inventory table, filters, distribution charts
- **Data Frames** — live frame data, frequency & ROCOF chart
- **Connectivity** — RTT chart, connectivity matrix, recommendations
- **Analytics** — angle differences, oscillation, islanding risk
- **Help / Docs** — in-app documentation

## Production build

```bash
npm run build
npm run preview
```

Serve `dist/` behind a reverse proxy that forwards `/conversation`, `/api`, and `/metrics` to the Go backend.

## Project structure

```
src/
  components/   # UI components by feature (layout, connectivity, devices, …)
  context/      # DashboardContext provider
  hooks/        # useDashboard, useRttHistory, useFrameTrendHistory
  pages/        # Route-level page components
  types/        # TypeScript definitions
  utils/        # Data transforms and chart helpers
```
