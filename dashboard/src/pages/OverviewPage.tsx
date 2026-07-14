import { useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  Gauge,
  Radio,
  Signal,
  X,
} from 'lucide-react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import 'leaflet/dist/leaflet.css'
import { MapContainer, TileLayer, Marker, Popup, Tooltip as LeafletTooltip, useMap } from 'react-leaflet'
import { CHART_COLORS, INDIA_CENTER } from '../constants'
import { useDashboardContext } from '../context/DashboardContext'
import { formatTS, round } from '../utils/format'
import { packetLossOf, pmuKey } from '../utils/pmu'

type FreqMode = 'absolute' | 'deviation'

function MapInvalidateSize() {
  const map = useMap()
  useEffect(() => {
    const refresh = () => map.invalidateSize()
    refresh()
    const t1 = window.setTimeout(refresh, 100)
    const t2 = window.setTimeout(refresh, 400)
    window.addEventListener('resize', refresh)
    return () => {
      window.clearTimeout(t1)
      window.clearTimeout(t2)
      window.removeEventListener('resize', refresh)
    }
  }, [map])
  return null
}

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

  const [freqMode, setFreqMode] = useState<FreqMode>('absolute')
  const [selectedStreams, setSelectedStreams] = useState<string[]>([])

  useEffect(() => {
    setSelectedStreams((prev) => {
      const names = pmus.map((p) => p.name)
      if (!names.length) return []
      const kept = prev.filter((n) => names.includes(n))
      return kept.length ? kept : names
    })
  }, [pmus])

  const plotPMUs = useMemo(
    () => pmus.filter((p) => selectedStreams.includes(p.name)),
    [pmus, selectedStreams],
  )

  const fleetFnom = useMemo(() => {
    const fromSelected = plotPMUs.map((p) => p.fnomHz).filter((v): v is number => !!v && v > 0)
    if (fromSelected.length) {
      const counts = new Map<number, number>()
      for (const f of fromSelected) counts.set(f, (counts.get(f) ?? 0) + 1)
      return [...counts.entries()].sort((a, b) => b[1] - a[1])[0][0]
    }
    return 60
  }, [plotPMUs])

  const offlineCount = pmus.filter((p) => !p.connected).length
  const issuesCount = pmus.filter((p) => !p.connected || packetLossOf(p) > 1).length
  const totalFps = Math.round(pmus.reduce((sum, p) => sum + p.approxFps, 0))
  const uniqueSubstations = new Set(pmus.map((p) => p.meta.substation)).size

  const fleetSnapshot = useMemo(() => {
    const online = pmus.filter((p) => p.connected)
    let worstDf = 0
    let worstDfPMU = '—'
    let maxRocof = 0
    let maxRocofPMU = '—'
    let statErrors = 0

    for (const pmu of online) {
      const fnom = pmu.fnomHz && pmu.fnomHz > 0 ? pmu.fnomHz : fleetFnom
      const df =
        typeof pmu.lastReading?.frequencyDev === 'number'
          ? pmu.lastReading.frequencyDev
          : (pmu.lastReading?.frequency ?? fnom) - fnom
      if (Math.abs(df) >= Math.abs(worstDf)) {
        worstDf = df
        worstDfPMU = pmu.name
      }
      const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)
      if (rocof >= maxRocof) {
        maxRocof = rocof
        maxRocofPMU = pmu.name
      }
      if (pmu.statDataError || pmu.lastReading?.statDataError) {
        statErrors += 1
      }
    }

    return { worstDf, worstDfPMU, maxRocof, maxRocofPMU, statErrors, online: online.length }
  }, [pmus, fleetFnom])

  const kpiCards = useMemo(
    () => [
      {
        label: 'Total PMUs',
        value: `${pmus.length}`,
        subtext: `${uniqueSubstations} substations`,
        tone: 'neutral',
        icon: <Radio size={18} />,
      },
      {
        label: 'Offline',
        value: `${offlineCount}`,
        subtext: issuesCount > offlineCount ? `${issuesCount} with issues (loss/offline)` : 'no frames recently',
        tone: offlineCount > 0 ? 'bad' : 'neutral',
        icon: <X size={18} />,
      },
      {
        label: 'Frames / sec',
        value: `${totalFps.toLocaleString()}`,
        subtext: 'aggregate across all streams',
        tone: 'neutral',
        icon: <Signal size={18} />,
      },
      {
        label: 'Worst Δf',
        value: `${fleetSnapshot.worstDf >= 0 ? '+' : ''}${round(fleetSnapshot.worstDf, 4)} Hz`,
        subtext: fleetSnapshot.online
          ? `${fleetSnapshot.worstDfPMU} · among online · now`
          : 'no online PMUs',
        tone: Math.abs(fleetSnapshot.worstDf) > 0.05 ? 'warn' : 'ok',
        icon: <Gauge size={18} />,
      },
      {
        label: 'Max |ROCOF|',
        value: `${round(fleetSnapshot.maxRocof, 4)} Hz/s`,
        subtext: fleetSnapshot.online
          ? `${fleetSnapshot.maxRocofPMU} · among online · now`
          : 'no online PMUs',
        tone: fleetSnapshot.maxRocof > 0.1 ? 'warn' : 'neutral',
        icon: <Activity size={18} />,
      },
      {
        label: 'STAT data errors',
        value: `${fleetSnapshot.statErrors}`,
        subtext: `${fleetSnapshot.online} online · latest STAT · FNOM ${fleetFnom} Hz`,
        tone: fleetSnapshot.statErrors > 0 ? 'bad' : 'ok',
        icon: <AlertTriangle size={18} />,
      },
    ],
    [pmus.length, uniqueSubstations, offlineCount, issuesCount, totalFps, fleetSnapshot, fleetFnom],
  )

  const toggleStream = (name: string) => {
    setSelectedStreams((prev) => {
      if (prev.includes(name)) {
        if (prev.length === 1) return prev
        return prev.filter((n) => n !== name)
      }
      return [...prev, name]
    })
  }

  const freqDataKey = (name: string) =>
    freqMode === 'absolute' ? `${pmuKey(name)}__frequency` : `${pmuKey(name)}__frequencyDev`

  return (
    <div className="overview-page">
      <section className="kpi-grid">
        {kpiCards.map((card) => (
          <article key={card.label} className={`kpi-card ${card.tone}`}>
            <div className="icon-wrap">{card.icon}</div>
            <p className="kpi-meta">{card.label}</p>
            <h2 className="kpi-value">{card.value}</h2>
            {card.subtext && (
              <p className="kpi-subtext" style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>
                {card.subtext}
              </p>
            )}
          </article>
        ))}
      </section>

      <section className="panel overview-toolbar">
        <div className="panel-head">
          <div>
            <h3>Live streams</h3>
            <p>Selection applies to both frequency and ROCOF charts</p>
          </div>
        </div>
        <div className="plot-chip-grid">
          {pmus.map((pmu, idx) => {
            const active = selectedStreams.includes(pmu.name)
            const color = CHART_COLORS[idx % CHART_COLORS.length]
            return (
              <button
                key={pmu.name}
                type="button"
                className={`plot-chip ${active ? 'active' : ''}`}
                onClick={() => toggleStream(pmu.name)}
                style={active ? { borderColor: color } : undefined}
              >
                <span className={`dot ${pmu.connected ? 'live' : 'off'}`} />
                {pmu.name}
              </button>
            )
          })}
        </div>
      </section>

      <section className="overview-charts-row">
        <div className="panel overview-chart-panel">
          <div className="panel-head">
            <div>
              <h3>System Frequency</h3>
              <p>
                {freqMode === 'absolute'
                  ? `Absolute Hz · FNOM ${fleetFnom} Hz band`
                  : `Δf from CFG FNOM (${fleetFnom} Hz)`}
              </p>
            </div>
            <div className="panel-tools-inline">
              <button
                type="button"
                className={`plot-action ${freqMode === 'absolute' ? 'active' : ''}`}
                onClick={() => setFreqMode('absolute')}
              >
                Absolute
              </button>
              <button
                type="button"
                className={`plot-action ${freqMode === 'deviation' ? 'active' : ''}`}
                onClick={() => setFreqMode('deviation')}
              >
                Δf
              </button>
            </div>
          </div>
          <div className="chart-wrap overview-chart">
            <ResponsiveContainer width="99%" height="100%" minWidth={1} minHeight={1}>
              <AreaChart data={mergedTrend}>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <YAxis
                  tick={{ fill: '#9eb0c5', fontSize: 11 }}
                  domain={['auto', 'auto']}
                  scale="linear"
                  tickFormatter={(v) => round(Number(v), freqMode === 'absolute' ? 3 : 4).toString()}
                  width={56}
                />
                <Tooltip
                  labelFormatter={(value) => formatTS(Number(value))}
                  formatter={(value) => [
                    `${round(Number(value ?? 0), 4)} ${freqMode === 'absolute' ? 'Hz' : 'Hz Δ'}`,
                    '',
                  ]}
                />
                <Legend />
                {freqMode === 'absolute' ? (
                  <>
                    <ReferenceLine y={fleetFnom} stroke="#7dd3a7" strokeDasharray="4 4" label={`FNOM ${fleetFnom}`} />
                    <ReferenceLine y={fleetFnom + 0.05} stroke="rgba(125,211,167,0.35)" strokeDasharray="2 4" />
                    <ReferenceLine y={fleetFnom - 0.05} stroke="rgba(125,211,167,0.35)" strokeDasharray="2 4" />
                  </>
                ) : (
                  <ReferenceLine y={0} stroke="#7dd3a7" strokeDasharray="4 4" label="0 Δf" />
                )}
                {plotPMUs.map((pmu) => {
                  const idx = pmus.findIndex((p) => p.name === pmu.name)
                  const color = CHART_COLORS[(idx >= 0 ? idx : 0) % CHART_COLORS.length]
                  return (
                    <Area
                      key={`${pmu.name}-${freqMode}`}
                      type="natural"
                      dataKey={freqDataKey(pmu.name)}
                      name={pmu.name}
                      stroke={color}
                      fill={color}
                      fillOpacity={0.12}
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

        <div className="panel overview-chart-panel">
          <div className="panel-head">
            <div>
              <h3>ROCOF</h3>
              <p>Rate of change of frequency (Hz/s)</p>
            </div>
          </div>
          <div className="chart-wrap overview-chart">
            <ResponsiveContainer width="99%" height="100%" minWidth={1} minHeight={1}>
              <LineChart data={mergedTrend}>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} domain={['auto', 'auto']} scale="linear" width={56} />
                <Tooltip
                  labelFormatter={(value) => formatTS(Number(value))}
                  formatter={(value) => [`${round(Number(value ?? 0), 5)} Hz/s`, 'ROCOF']}
                />
                <Legend />
                <ReferenceLine y={0} stroke="rgba(255,255,255,0.25)" strokeDasharray="4 4" />
                {plotPMUs.map((pmu) => {
                  const idx = pmus.findIndex((p) => p.name === pmu.name)
                  const color = CHART_COLORS[(idx >= 0 ? idx : 0) % CHART_COLORS.length]
                  return (
                    <Line
                      key={pmu.name}
                      type="natural"
                      dataKey={`${pmuKey(pmu.name)}__rocof`}
                      name={pmu.name}
                      stroke={color}
                      dot={false}
                      strokeWidth={2}
                      connectNulls
                      isAnimationActive={false}
                    />
                  )
                })}
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>
      </section>

      <section className="overview-lower-row">
        <div className="panel overview-map-panel">
          <div className="panel-head">
            <div>
              <h3>PMU map</h3>
              <p>Registered latitude / longitude · India view</p>
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
          <div className="map-wrap-react overview-map">
            <MapContainer center={INDIA_CENTER} zoom={5} style={{ height: '100%', width: '100%' }}>
              <MapInvalidateSize />
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

        <div className="overview-side-stack">
          <div className="panel events-panel overview-alerts-panel">
            <div className="panel-head">
              <div>
                <h3>Active Alerts & Events</h3>
                <p>{liveAlerts.length} live alerts</p>
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
                <h3>Regional Health</h3>
                <p>By registered region</p>
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
    </div>
  )
}
