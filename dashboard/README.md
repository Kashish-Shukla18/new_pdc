# PDC React Dashboard

Operator dashboard for the PDC synchrophasor platform. Replaces Grafana with a purpose-built React UI for live PMU monitoring, device management, and analytics.

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
| `/api/*` | `http://127.0.0.1:8080` |

Start PDC before the dashboard:

```bash
# from repo root
go run . -metrics-addr :2112 -api-addr :8080
```

## Pages

- **Overview** — KPIs, frequency chart, simulator map, alerts
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
