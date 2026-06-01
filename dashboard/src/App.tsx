import { useEffect, useMemo, useState } from 'react'
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import {
  Activity,
  AlertTriangle,
  Gauge,
  MapPin,
  Plus,
  Radio,
  Signal,
  SlidersHorizontal,
  TimerReset,
  Wifi,
} from 'lucide-react'

type TrendPoint = {
  ts: number
  frequency: number
  mw: number
  mvar: number
  rocof: number
}

type PhasorVector = {
  magnitude: number
  angleDeg: number
}

type PhasorSnapshot = {
  va: PhasorVector
  vb: PhasorVector
  vc: PhasorVector
  ia: PhasorVector
  ts: number
}

type LivePMUState = {
  name: string
  connected: boolean
  connectionText: string
  lastEventTime: string
  lastFrameTime: string
  lastHandshake: string
  lastError: string
  totalFrames: number
  approxFps: number
  qualityRejects: number
  kafkaErrors: number
  sinkErrors: number
  spoolQueued: number
  lastReading: TrendPoint
  lastPhasor: PhasorSnapshot
  trends: TrendPoint[]
}

type DashboardState = {
  nowUtc: string
  pmus: LivePMUState[]
  eventCount: number
}

type ConversationEvent = {
  time: string
  pmu: string
  stage: string
  status: string
  message: string
}

type PMURecord = {
  id: string
  displayName: string
  substation: string
  region: string
  voltageClass: string
  vendorModel: string
  primaryIp: string
  redundantIp: string
  reportingRate: string
  commissioned: string
  status: string
  dataAvailability: string
  latency: string
  jitter: string
  packetLoss: string
  signalChannels: string
  notes: string
}

type PMUFormState = PMURecord

type PMUPreset = {
  id: string
  label: string
  values: Partial<PMUFormState>
}

const registryStorageKey = 'pdc.dashboard.registry.v1'

const sampleRecord: PMURecord = {
  id: '7004',
  displayName: 'Bhiwadi-PMU1 - Bhiwadi',
  substation: 'Bhiwadi, Rajasthan',
  region: 'NRLDC',
  voltageClass: '765 kV',
  vendorModel: 'ABB RES670 v2.0',
  primaryIp: '10.45.15.12',
  redundantIp: '10.45.115.12',
  reportingRate: '50 fps',
  commissioned: '2018-02-08',
  status: 'Degraded',
  dataAvailability: '89.2%',
  latency: '81 ms',
  jitter: '20.6 ms',
  packetLoss: '6.57%',
  signalChannels: 'Voltage phasors: V_R, V_Y, V_B, V_pos | Current phasors: I_R, I_Y, I_B, I_neg | Analog: MW, MVAR, MVA | Digital: 52A_brk, 79_reclose',
  notes: 'Inspect STAT word, verify time sync, and review fiber path.',
}

const emptyState: DashboardState = {
  nowUtc: new Date().toISOString(),
  pmus: [],
  eventCount: 0,
}

const defaultForm: PMUFormState = {
  ...sampleRecord,
  id: '',
  displayName: '',
  notes: '',
}

const regionOrder = ['NRLDC', 'WRLDC', 'NR', 'SR', 'ER', 'NER', 'ALL']

const pmuPresets: PMUPreset[] = [
  {
    id: 'pmu-1',
    label: 'Simulator 1',
    values: {
      id: '7004',
      displayName: 'Bhiwadi-PMU1 - Bhiwadi',
      substation: 'Bhiwadi, Rajasthan',
      region: 'NRLDC',
      voltageClass: '765 kV',
      vendorModel: 'ABB RES670 v2.0',
      primaryIp: '10.45.15.12',
      redundantIp: '10.45.115.12',
      reportingRate: '50 fps',
      commissioned: '2018-02-08',
      status: 'Degraded',
      dataAvailability: '89.2%',
      latency: '81 ms',
      jitter: '20.6 ms',
      packetLoss: '6.57%',
      signalChannels:
        'Voltage phasors: V_R, V_Y, V_B, V_pos | Current phasors: I_R, I_Y, I_B, I_neg | Analog: MW, MVAR, MVA | Digital: 52A_brk, 79_reclose',
      notes: 'Inspect STAT word, verify time sync, and review fiber path.',
    },
  },
  {
    id: 'pmu-2',
    label: 'Simulator 2',
    values: {
      id: '7005',
      displayName: 'Bhiwadi-PMU2 - Alwar',
      substation: 'Alwar, Rajasthan',
      region: 'NRLDC',
      voltageClass: '220 kV',
      vendorModel: 'SEL-421 PMU',
      primaryIp: '10.45.16.12',
      redundantIp: '10.45.116.12',
      reportingRate: '50 fps',
      commissioned: '2019-05-16',
      status: 'Healthy',
      dataAvailability: '98.7%',
      latency: '34 ms',
      jitter: '9.4 ms',
      packetLoss: '0.45%',
      signalChannels:
        'Voltage phasors: V_R, V_Y, V_B, V_pos | Current phasors: I_R, I_Y, I_B, I_neg | Analog: MW, MVAR, MVA | Digital: 52A_brk, 79_reclose',
      notes: 'Use this as a stable simulator lane for baseline comparison.',
    },
  },
  {
    id: 'pmu-3',
    label: 'Simulator 3',
    values: {
      id: '7006',
      displayName: 'Bhiwadi-PMU3 - Neemrana',
      substation: 'Neemrana, Rajasthan',
      region: 'NRLDC',
      voltageClass: '132 kV',
      vendorModel: 'GE D20 PMU',
      primaryIp: '10.45.17.12',
      redundantIp: '10.45.117.12',
      reportingRate: '50 fps',
      commissioned: '2020-11-03',
      status: 'Warning',
      dataAvailability: '94.1%',
      latency: '57 ms',
      jitter: '14.8 ms',
      packetLoss: '1.62%',
      signalChannels:
        'Voltage phasors: V_R, V_Y, V_B, V_pos | Current phasors: I_R, I_Y, I_B, I_neg | Analog: MW, MVAR, MVA | Digital: 52A_brk, 79_reclose',
      notes: 'Useful for a slightly noisy stream in the comparison view.',
    },
  },
]

function safeParseRegistry(raw: string | null): PMURecord[] {
  if (!raw) return [sampleRecord]
  try {
    const parsed = JSON.parse(raw) as PMURecord[]
    if (!Array.isArray(parsed) || parsed.length === 0) return [sampleRecord]
    return parsed
  } catch {
    return [sampleRecord]
  }
}

function formatTS(ts: number) {
  return new Date(ts).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function round(v: number, digits = 2) {
  return Number(v.toFixed(digits))
}

function statusTone(status: string) {
  const lowered = status.toLowerCase()
  if (lowered.includes('healthy') || lowered.includes('online') || lowered.includes('ok')) return 'ok'
  if (lowered.includes('degraded') || lowered.includes('warning') || lowered.includes('warn')) return 'warn'
  return 'bad'
}

function pmuKey(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '')
}

function phasorToXY(vector: PhasorVector, radius: number) {
  const radians = (vector.angleDeg * Math.PI) / 180
  const magnitude = Math.max(0, Math.min(1, vector.magnitude / 100)) * radius
  return {
    x: Math.cos(radians) * magnitude,
    y: -Math.sin(radians) * magnitude,
  }
}

function PhasorPlot({ snapshot }: { snapshot?: PhasorSnapshot }) {
  const center = 100
  const radius = 72

  const vectors = [
    { label: 'VA', color: '#4de0ff', value: snapshot?.va },
    { label: 'VB', color: '#7dff93', value: snapshot?.vb },
    { label: 'VC', color: '#ffd166', value: snapshot?.vc },
    { label: 'IA', color: '#ff7b7b', value: snapshot?.ia },
  ]

  return (
    <div className="phasor-frame">
      <svg viewBox="0 0 200 200" className="phasor-svg" aria-label="Phasor plot">
        <defs>
          <marker id="arrow-cyan" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" fill="#4de0ff" />
          </marker>
          <marker id="arrow-mint" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" fill="#7dff93" />
          </marker>
          <marker id="arrow-amber" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" fill="#ffd166" />
          </marker>
          <marker id="arrow-coral" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" fill="#ff7b7b" />
          </marker>
        </defs>

        <circle cx={center} cy={center} r={radius} className="phasor-ring" />
        <circle cx={center} cy={center} r={radius * 0.66} className="phasor-ring faint" />
        <line x1={center - radius} y1={center} x2={center + radius} y2={center} className="phasor-axis" />
        <line x1={center} y1={center - radius} x2={center} y2={center + radius} className="phasor-axis" />

        {vectors.map((entry) => {
          const xy = entry.value ? phasorToXY(entry.value, radius) : { x: 0, y: 0 }
          const markerMap: Record<string, string> = {
            '#4de0ff': 'url(#arrow-cyan)',
            '#7dff93': 'url(#arrow-mint)',
            '#ffd166': 'url(#arrow-amber)',
            '#ff7b7b': 'url(#arrow-coral)',
          }

          return (
            <g key={entry.label}>
              <line
                x1={center}
                y1={center}
                x2={center + xy.x}
                y2={center + xy.y}
                stroke={entry.color}
                strokeWidth="3"
                markerEnd={markerMap[entry.color]}
              />
              <circle cx={center + xy.x} cy={center + xy.y} r="3.5" fill={entry.color} />
            </g>
          )
        })}
      </svg>

      <div className="phasor-legend">
        {vectors.map((entry) => (
          <div key={entry.label} className="phasor-item">
            <span style={{ background: entry.color }} />
            <div>
              <strong>{entry.label}</strong>
              <p>
                {entry.value
                  ? `${round(entry.value.magnitude, 1)} pu · ${round(entry.value.angleDeg, 1)}°`
                  : 'No live value'}
              </p>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function App() {
  const [dashboard, setDashboard] = useState<DashboardState>(emptyState)
  const [eventFeed, setEventFeed] = useState<ConversationEvent[]>([])
  const [streamOnline, setStreamOnline] = useState(false)
  const [registry, setRegistry] = useState<PMURecord[]>(() => safeParseRegistry(localStorage.getItem(registryStorageKey)))
  const [selectedRegion, setSelectedRegion] = useState<string>('ALL')
  const [selectedPMU, setSelectedPMU] = useState<string>(sampleRecord.displayName)
  const [form, setForm] = useState<PMUFormState>(defaultForm)
  const [activeTab, setActiveTab] = useState<'dashboard' | 'register'>('dashboard')

  useEffect(() => {
    localStorage.setItem(registryStorageKey, JSON.stringify(registry))
  }, [registry])

  useEffect(() => {
    const refresh = async () => {
      try {
        const res = await fetch('/conversation/state')
        if (!res.ok) return
        const data = (await res.json()) as DashboardState
        setDashboard(data)
      } catch {
        // Keep the last good snapshot visible.
      }
    }

    void refresh()
    const timer = window.setInterval(refresh, 1000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    const es = new EventSource('/conversation/events')

    es.onopen = () => setStreamOnline(true)
    es.onerror = () => setStreamOnline(false)
    es.onmessage = (evt) => {
      try {
        const parsed = JSON.parse(evt.data) as ConversationEvent
        setEventFeed((prev) => [parsed, ...prev].slice(0, 30))
      } catch {
        // Ignore malformed events.
      }
    }

    return () => es.close()
  }, [])

  const liveByName = useMemo(() => {
    return new Map(dashboard.pmus.map((item) => [pmuKey(item.name), item]))
  }, [dashboard.pmus])

  const mergedRegistry = useMemo(() => {
    const matchedLiveKeys = new Set<string>()

    const registeredEntries = registry.map((record) => {
      const live = liveByName.get(pmuKey(record.displayName)) ?? liveByName.get(pmuKey(record.id))
      if (live) {
        matchedLiveKeys.add(pmuKey(live.name))
      }
      return {
        record,
        live,
        connected: live?.connected ?? false,
        connectionText: live?.connectionText ?? 'awaiting telemetry',
        fps: live?.approxFps ?? 0,
        frames: live?.totalFrames ?? 0,
        rejects: live?.qualityRejects ?? 0,
        kafkaErrors: live?.kafkaErrors ?? 0,
        sinkErrors: live?.sinkErrors ?? 0,
        spoolQueued: live?.spoolQueued ?? 0,
        lastReading: live?.lastReading,
        lastPhasor: live?.lastPhasor,
        trends: live?.trends ?? [],
      }
    })

    const autoDiscoveredEntries = dashboard.pmus
      .filter((live) => !matchedLiveKeys.has(pmuKey(live.name)))
      .map((live) => ({
        record: {
          id: live.name,
          displayName: live.name,
          substation: 'Auto-discovered stream',
          region: 'LIVE',
          voltageClass: '--',
          vendorModel: 'stream source',
          primaryIp: '127.0.0.1',
          redundantIp: '127.0.0.1',
          reportingRate: `${round(live.approxFps)} fps`,
          commissioned: '--',
          status: live.connected ? 'Healthy' : 'Offline',
          dataAvailability: '--',
          latency: '--',
          jitter: '--',
          packetLoss: '--',
          signalChannels: 'Live telemetry from PDC /conversation/state',
          notes: 'Auto-created from live stream because no matching registry entry was found.',
        },
        live,
        connected: live.connected,
        connectionText: live.connectionText,
        fps: live.approxFps,
        frames: live.totalFrames,
        rejects: live.qualityRejects,
        kafkaErrors: live.kafkaErrors,
        sinkErrors: live.sinkErrors,
        spoolQueued: live.spoolQueued,
        lastReading: live.lastReading,
        lastPhasor: live.lastPhasor,
        trends: live.trends ?? [],
      }))

    return [...registeredEntries, ...autoDiscoveredEntries]
  }, [liveByName, registry])

  const filteredRegistry = useMemo(() => {
    if (selectedRegion === 'ALL') return mergedRegistry
    return mergedRegistry.filter((entry) => entry.record.region === selectedRegion)
  }, [mergedRegistry, selectedRegion])

  useEffect(() => {
    if (!filteredRegistry.length) return
    if (!filteredRegistry.some((entry) => entry.record.displayName === selectedPMU)) {
      setSelectedPMU(filteredRegistry[0].record.displayName)
    }
  }, [filteredRegistry, selectedPMU])

  const selected = useMemo(() => {
    return (
      filteredRegistry.find((entry) => entry.record.displayName === selectedPMU) ?? filteredRegistry[0] ?? null
    )
  }, [filteredRegistry, selectedPMU])

  const selectedTrend = selected?.trends ?? []
  const latest = selected?.lastReading ?? selectedTrend.at(-1)
  const totalErrors = filteredRegistry.reduce((sum, entry) => sum + entry.kafkaErrors + entry.sinkErrors, 0)
  const connectedCount = filteredRegistry.filter((entry) => entry.connected).length
  const totalSpool = filteredRegistry.reduce((sum, entry) => sum + entry.spoolQueued, 0)
  const totalFrames = filteredRegistry.reduce((sum, entry) => sum + entry.frames, 0)
  const regions = [
    'ALL',
    ...Array.from(new Set(mergedRegistry.map((entry) => entry.record.region))).sort((a, b) => a.localeCompare(b)),
  ]

  function updateForm<K extends keyof PMUFormState>(key: K, value: PMUFormState[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function registerPMU() {
    if (!form.id.trim() || !form.displayName.trim()) return

    const nextRecord: PMURecord = {
      ...form,
      id: form.id.trim(),
      displayName: form.displayName.trim(),
      substation: form.substation.trim(),
      region: form.region.trim(),
      voltageClass: form.voltageClass.trim(),
      vendorModel: form.vendorModel.trim(),
      primaryIp: form.primaryIp.trim(),
      redundantIp: form.redundantIp.trim(),
      reportingRate: form.reportingRate.trim(),
      commissioned: form.commissioned.trim(),
      status: form.status.trim(),
      dataAvailability: form.dataAvailability.trim(),
      latency: form.latency.trim(),
      jitter: form.jitter.trim(),
      packetLoss: form.packetLoss.trim(),
      signalChannels: form.signalChannels.trim(),
      notes: form.notes.trim(),
    }

    setRegistry((current) => {
      const filtered = current.filter((item) => item.id !== nextRecord.id && item.displayName !== nextRecord.displayName)
      return [nextRecord, ...filtered]
    })
    setSelectedRegion(nextRecord.region)
    setSelectedPMU(nextRecord.displayName)
  }

  function applyPreset(preset: PMUPreset) {
    setForm((current) => ({
      ...current,
      ...preset.values,
      notes: preset.values.notes ?? current.notes,
      signalChannels: preset.values.signalChannels ?? current.signalChannels,
    }))
    setActiveTab('register')
  }

  const statusCards = [
    {
      label: 'Connected PMUs',
      value: `${connectedCount}/${filteredRegistry.length || 0}`,
      tone: 'ok',
      icon: <Wifi size={18} />,
    },
    {
      label: 'Frequency',
      value: latest ? `${round(latest.frequency, 3)} Hz` : '--',
      tone: 'neutral',
      icon: <Gauge size={18} />,
    },
    {
      label: 'Total Frames',
      value: `${totalFrames}`,
      tone: 'neutral',
      icon: <Signal size={18} />,
    },
    {
      label: 'Pipeline Errors',
      value: `${totalErrors}`,
      tone: 'warn',
      icon: <AlertTriangle size={18} />,
    },
    {
      label: 'Spool Queue',
      value: `${totalSpool}`,
      tone: 'neutral',
      icon: <Activity size={18} />,
    },
  ]

  const chartSeries = [
    { key: 'frequency', label: 'Frequency', color: '#4de0ff', unit: 'Hz' },
    { key: 'mw', label: 'MW', color: '#7dff93', unit: 'MW' },
    { key: 'mvar', label: 'MVAR', color: '#ffd166', unit: 'MVAR' },
    { key: 'rocof', label: 'ROCOF', color: '#ff7b7b', unit: 'Hz/s' },
  ] as const

  const dashboardTabs = [
    { id: 'dashboard' as const, label: 'Dashboard' },
    { id: 'register' as const, label: 'Register PMU' },
  ]

  return (
    <div className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Synchrophasor Operations Console</p>
          <h1>Wide-Area PMU Monitoring Dashboard</h1>
          <p className="subtitle">
            Register devices, supervise regional health, and analyze live synchrophasor trends from one operational view.
          </p>
        </div>

        <div className="status-pills">
          <span className={`pill ${streamOnline ? 'ok' : 'warn'}`}>
            <Radio size={14} /> Stream {streamOnline ? 'online' : 'reconnecting'}
          </span>
          <span className="pill neutral">
            <TimerReset size={14} /> Snapshot {new Date(dashboard.nowUtc).toLocaleTimeString()}
          </span>
          <span className="pill neutral">
            <MapPin size={14} /> {dashboard.eventCount} total events
          </span>
        </div>
      </header>

      <nav className="tabbar" aria-label="Dashboard sections">
        {dashboardTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`tab-button ${activeTab === tab.id ? 'active' : ''}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </nav>

      {activeTab === 'dashboard' ? (
        <>
          <section className="kpi-grid">
            {statusCards.map((card) => (
              <article key={card.label} className={`kpi-card ${card.tone}`}>
                <div className="icon-wrap">{card.icon}</div>
                <p className="kpi-meta">{card.label}</p>
                <h2 className="kpi-value">{card.value}</h2>
              </article>
            ))}
          </section>

          <section className="main-grid dashboard-grid">
            <div className="stacked-panels">
              <div className="panel filter-panel">
                <div className="panel-head">
                  <div>
                    <h3>Region Filter</h3>
                    <p>Select PMUs by RLDC region.</p>
                  </div>
                  <SlidersHorizontal size={18} />
                </div>
                <div className="region-pills">
                  {regionOrder.map((region) => {
                    const present = region === 'ALL' ? true : regions.includes(region)
                    const active = selectedRegion === region
                    if (!present && region !== 'ALL') return null
                    return (
                      <button
                        key={region}
                        type="button"
                        className={`region-pill ${active ? 'active' : ''}`}
                        onClick={() => setSelectedRegion(region)}
                      >
                        {region}
                      </button>
                    )
                  })}
                </div>
              </div>

              <div className="panel table-panel">
                <div className="panel-head">
                  <div>
                    <h3>PMU Status Table</h3>
                    <p>Click a row to drive the charts below.</p>
                  </div>
                  <span className="table-caption">{filteredRegistry.length} PMUs</span>
                </div>

                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>PMU</th>
                        <th>Region</th>
                        <th>Status</th>
                        <th>Live</th>
                        <th>Frames</th>
                        <th>Latency</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredRegistry.map((entry) => {
                        const active = entry.record.displayName === selectedPMU
                        return (
                          <tr
                            key={`${entry.record.id}-${entry.record.displayName}`}
                            className={active ? 'active' : ''}
                            onClick={() => setSelectedPMU(entry.record.displayName)}
                          >
                            <td>
                              <strong>{entry.record.displayName}</strong>
                              <span>{entry.record.substation}</span>
                            </td>
                            <td>{entry.record.region}</td>
                            <td>
                              <span className={`status-chip ${statusTone(entry.record.status)}`}>
                                {entry.record.status}
                              </span>
                            </td>
                            <td>
                              <span className={`status-chip ${entry.connected ? 'ok' : 'bad'}`}>
                                {entry.connectionText}
                              </span>
                            </td>
                            <td>{entry.frames}</td>
                            <td>{entry.record.latency}</td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              </div>

              <section className="panel events-panel">
                <div className="panel-head">
                  <div>
                    <h3>Live Event Tape</h3>
                    <p>Most recent stream updates from the PDC.</p>
                  </div>
                </div>

                <div className="events-list">
                  {eventFeed.length === 0 && <p className="empty">Waiting for stream events...</p>}
                  {eventFeed.map((evt, idx) => (
                    <div key={`${evt.time}-${idx}`} className="event-row">
                      <div className="event-time">{new Date(evt.time).toLocaleTimeString()}</div>
                      <div className="event-main">
                        <strong>{evt.pmu}</strong>
                        <span>
                          {evt.stage} / {evt.status}
                        </span>
                      </div>
                      <p className="event-msg">{evt.message}</p>
                    </div>
                  ))}
                </div>
              </section>
            </div>

            <div className="stacked-panels">
              <div className="panel detail-panel">
                <div className="panel-head">
                  <div>
                    <h3>{selected?.record.displayName ?? 'Select a PMU'}</h3>
                    <p>
                      {selected?.record.region} region · {selected?.record.vendorModel}
                    </p>
                  </div>
                  <span className={`status-chip ${selected?.connected ? 'ok' : 'bad'}`}>
                    {selected?.connected ? 'Live' : 'Offline'}
                  </span>
                </div>

                <div className="detail-grid">
                  <div className="detail-card">
                    <p>PMU ID</p>
                    <strong>{selected?.record.id ?? '--'}</strong>
                  </div>
                  <div className="detail-card">
                    <p>Voltage Class</p>
                    <strong>{selected?.record.voltageClass ?? '--'}</strong>
                  </div>
                  <div className="detail-card">
                    <p>Availability</p>
                    <strong>{selected?.record.dataAvailability ?? '--'}</strong>
                  </div>
                  <div className="detail-card">
                    <p>Jitter</p>
                    <strong>{selected?.record.jitter ?? '--'}</strong>
                  </div>
                </div>

                <div className="detail-notes">
                  <p><strong>Channels:</strong> {selected?.record.signalChannels ?? '--'}</p>
                  <p><strong>Notes:</strong> {selected?.record.notes ?? '--'}</p>
                </div>
              </div>

              <aside className="panel phasor-panel">
                <div className="panel-head">
                  <div>
                    <h3>Phasors</h3>
                    <p>VA, VB, VC, IA from the live stream.</p>
                  </div>
                </div>
                <PhasorPlot snapshot={selected?.lastPhasor} />
              </aside>
            </div>
          </section>

          <section className="charts-grid">
            {chartSeries.map((series) => (
              <article key={series.key} className="panel chart-panel">
                <div className="panel-head">
                  <div>
                    <h3>{selected?.record.displayName ?? 'PMU'} {series.label}</h3>
                    <p>Last {selectedTrend.length} samples</p>
                  </div>
                </div>
                <div className="chart-wrap small">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={selectedTrend}>
                      <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                      <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 12 }} />
                      <YAxis tick={{ fill: '#9eb0c5', fontSize: 12 }} />
                      <Tooltip
                        labelFormatter={(value) => formatTS(Number(value))}
                        contentStyle={{
                          background: 'rgba(10, 16, 26, 0.96)',
                          border: '1px solid rgba(125, 175, 255, 0.2)',
                          borderRadius: '12px',
                        }}
                      />
                      <Legend />
                      <Line type="monotone" dataKey={series.key} stroke={series.color} dot={false} />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              </article>
            ))}
          </section>
        </>
      ) : (
        <section className="panel register-panel">
          <div className="panel-head register-head">
            <div>
              <h3>Register PMU</h3>
              <p>Create or update a PMU entry from the full device details.</p>
            </div>
            <Plus size={18} />
          </div>

          <div className="form-grid register-grid">
            <div className="preset-strip field-wide">
              <div className="preset-copy">
                <strong>Quick simulator presets</strong>
                <p>Use these to register all three PMUs fast, then edit the values if needed.</p>
              </div>
              <div className="preset-buttons">
                {pmuPresets.map((preset) => (
                  <button key={preset.id} type="button" className="preset-button" onClick={() => applyPreset(preset)}>
                    {preset.label}
                  </button>
                ))}
              </div>
            </div>

            {[
              ['id', 'PMU ID'],
              ['displayName', 'PMU Name'],
              ['substation', 'Substation'],
              ['region', 'Region (RLDC)'],
              ['voltageClass', 'Voltage Class'],
              ['vendorModel', 'Vendor / Model'],
              ['primaryIp', 'Primary IP'],
              ['redundantIp', 'Redundant IP'],
              ['reportingRate', 'Reporting Rate'],
              ['commissioned', 'Commissioned'],
              ['status', 'Status'],
              ['dataAvailability', 'Data Availability'],
              ['latency', 'Latency'],
              ['jitter', 'Jitter'],
              ['packetLoss', 'Packet Loss'],
            ].map(([key, label]) => (
              <label key={key} className="field">
                <span>{label}</span>
                <input
                  value={form[key as keyof PMUFormState] as string}
                  onChange={(e) => updateForm(key as keyof PMUFormState, e.target.value as never)}
                  placeholder={label}
                />
              </label>
            ))}

            <label className="field field-wide">
              <span>Signal Channels</span>
              <textarea
                rows={4}
                value={form.signalChannels}
                onChange={(e) => updateForm('signalChannels', e.target.value)}
                placeholder="Voltage phasors, current phasors, analog, digital"
              />
            </label>

            <label className="field field-wide">
              <span>Notes</span>
              <textarea
                rows={3}
                value={form.notes}
                onChange={(e) => updateForm('notes', e.target.value)}
                placeholder="Operator notes or corrective actions"
              />
            </label>

            <div className="register-actions field-wide">
              <button type="button" className="primary-btn" onClick={registerPMU}>
                Save PMU Profile
              </button>
              <p>
                Saved PMUs are kept locally in your browser. Switch back to the dashboard tab to review status and live telemetry.
              </p>
            </div>
          </div>
        </section>
      )}
    </div>
  )
}

export default App