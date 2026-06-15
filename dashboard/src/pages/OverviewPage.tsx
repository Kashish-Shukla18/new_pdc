import { useMemo } from 'react'
import {
  Activity,
  AlertTriangle,
  Radio,
  Signal,
  Wifi,
  X,
} from 'lucide-react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import 'leaflet/dist/leaflet.css'
import { MapContainer, TileLayer, Marker, Popup, Tooltip as LeafletTooltip } from 'react-leaflet'
import { CHART_COLORS, INDIA_CENTER } from '../constants'
import { useDashboardContext } from '../context/DashboardContext'
import { formatTS, round } from '../utils/format'
import { availabilityOf, packetLossOf, pmuKey } from '../utils/pmu'

export function OverviewPage() {
  const {
    pmus,
    mergedTrend,
    mapFilter,
    setMapFilter,
    liveAlerts,
    regionSummary,
    setDrawerPMUName,
  } = useDashboardContext()

  const healthyCount = pmus.filter((p) => p.connected && packetLossOf(p) <= 1).length
  const degradedCount = pmus.filter((p) => p.connected && packetLossOf(p) > 1).length
  const offlineCount = pmus.filter((p) => !p.connected).length
  const avgAvailability = pmus.length
    ? round(pmus.reduce((sum, p) => sum + availabilityOf(p, p.meta.targetFps), 0) / pmus.length, 1)
    : 0
  const totalFps = Math.round(pmus.reduce((sum, p) => sum + p.approxFps, 0))
  const uniqueSubstations = new Set(pmus.map((p) => p.meta.substation)).size

  const statusCards = useMemo(
    () => [
      {
        label: 'Total PMUs',
        value: `${pmus.length}`,
        subtext: `${uniqueSubstations} substations`,
        trend: '▲ stable',
        tone: 'neutral',
        icon: <Radio size={18} />,
      },
      {
        label: 'Healthy',
        value: `${healthyCount}`,
        subtext: `${Math.round((healthyCount / (pmus.length || 1)) * 100)}% reporting`,
        tone: 'ok',
        icon: <Wifi size={18} />,
      },
      {
        label: 'Degraded',
        value: `${degradedCount}`,
        subtext: 'quality flag set / partial loss',
        tone: degradedCount > 0 ? 'warn' : 'neutral',
        icon: <AlertTriangle size={18} />,
      },
      {
        label: 'Offline',
        value: `${offlineCount}`,
        subtext: 'no frames in 30 s',
        tone: offlineCount > 0 ? 'bad' : 'neutral',
        icon: <X size={18} />,
      },
      {
        label: 'Avg Data Availability',
        value: `${avgAvailability}%`,
        subtext: 'last 60 minutes',
        tone: avgAvailability < 95 ? 'warn' : 'ok',
        icon: <Activity size={18} />,
      },
      {
        label: 'Frames / sec ingested',
        value: `${totalFps.toLocaleString()}`,
        subtext: 'aggregate across all streams',
        tone: 'neutral',
        icon: <Signal size={18} />,
      },
    ],
    [pmus.length, uniqueSubstations, healthyCount, degradedCount, offlineCount, avgAvailability, totalFps],
  )

  return (
    <>
      <section className="kpi-grid">
        {statusCards.map((card) => (
          <article key={card.label} className={`kpi-card ${card.tone}`}>
            <div className="icon-wrap">{card.icon}</div>
            <p className="kpi-meta">{card.label}</p>
            <h2 className="kpi-value">{card.value}</h2>
            {card.subtext && (
              <p className="kpi-subtext" style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>
                {card.subtext}
              </p>
            )}
            {card.trend && (
              <p className="kpi-trend" style={{ fontSize: '0.8rem', color: 'var(--ok-color)', marginTop: '4px' }}>
                {card.trend}
              </p>
            )}
          </article>
        ))}
      </section>

      <section className="main-grid dashboard-grid">
        <div className="stacked-panels">
          <div className="panel">
            <div className="panel-head">
              <div>
                <h3>System Frequency</h3>
                <p>Live traces from all connected PMUs.</p>
              </div>
            </div>
            <div className="chart-wrap small">
              <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
                <AreaChart data={mergedTrend}>
                  <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                  <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                  <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} domain={['auto', 'auto']} scale="linear" />
                  <Tooltip labelFormatter={(value) => formatTS(Number(value))} />
                  <Legend />
                  {pmus.map((pmu, idx) => {
                    const color = CHART_COLORS[idx % CHART_COLORS.length]
                    return (
                      <Area
                        key={pmu.name}
                        type="natural"
                        dataKey={`${pmuKey(pmu.name)}__frequency`}
                        name={pmu.name}
                        stroke={color}
                        fill={color}
                        fillOpacity={0.15}
                        dot={false}
                        strokeWidth={2}
                        connectNulls
                        isAnimationActive={false}
                      />
                    )
                  })}
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="panel">
            <div className="panel-head">
              <div>
                <h3>Simulator Map</h3>
                <p>Mapped using simulator metadata and live status.</p>
              </div>
              <div className="panel-tools-inline">
                <button type="button" className="plot-action" onClick={() => setMapFilter('all')}>
                  All
                </button>
                <button type="button" className="plot-action" onClick={() => setMapFilter('issues')}>
                  Issues
                </button>
              </div>
            </div>
            <div className="map-wrap-react">
              <MapContainer center={INDIA_CENTER} zoom={5} style={{ height: '450px', width: '100%' }}>
                <TileLayer url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
                {pmus
                  .filter((pmu) =>
                    mapFilter === 'issues' ? !pmu.connected || packetLossOf(pmu) > 1 : true,
                  )
                  .map((pmu) => (
                    <Marker
                      key={pmu.name}
                      position={[pmu.meta.lat, pmu.meta.lon]}
                      eventHandlers={{ click: () => setDrawerPMUName(pmu.name) }}
                    >
                      <LeafletTooltip direction="top">{pmu.name}</LeafletTooltip>
                      <Popup>
                        <strong>{pmu.name}</strong>
                        <br />
                        Region: {pmu.meta.region}
                        <br />
                        Status: {pmu.connected ? 'Connected' : 'Offline'}
                      </Popup>
                    </Marker>
                  ))}
              </MapContainer>
            </div>
          </div>
        </div>

        <div className="stacked-panels">
          <div className="panel events-panel">
            <div className="panel-head">
              <div>
                <h3>Active Alerts & Events</h3>
                <p>{liveAlerts.length} live alerts from simulator telemetry and stream events.</p>
              </div>
            </div>
            <div className="events-list">
              {liveAlerts.map((alert, idx) => (
                <div key={`${alert.title}-${idx}`} className="event-row">
                  <div className="event-time">{alert.time}</div>
                  <div className="event-main">
                    <strong>{alert.title}</strong>
                    <span
                      className={`status-chip ${alert.sev === 'bad' ? 'bad' : alert.sev === 'warn' ? 'warn' : 'ok'}`}
                    >
                      {alert.sev}
                    </span>
                  </div>
                  <p className="event-msg">{alert.msg}</p>
                </div>
              ))}
            </div>
          </div>

          <div className="panel">
            <div className="panel-head">
              <div>
                <h3>Regional Health Summary</h3>
                <p>Roll-up from simulator PMUs.</p>
              </div>
            </div>
            <div className="plot-chip-grid">
              {regionSummary.map((region) => (
                <article key={region.region} className="plot-chip active">
                  <span className={`dot ${region.connected === region.total ? 'live' : 'off'}`} />
                  {region.region}: {region.connected}/{region.total} ({round(region.availability, 1)}%)
                </article>
              ))}
            </div>
          </div>
        </div>
      </section>
    </>
  )
}
