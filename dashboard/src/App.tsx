import { AddPMUModal } from './components/drawer/AddPMUModal'
import { PMUDetailDrawer } from './components/drawer/PMUDetailDrawer'
import { Header } from './components/layout/Header'
import { PageHeader } from './components/layout/PageHeader'
import { Sidebar } from './components/layout/Sidebar'
import { DashboardProvider, useDashboardContext } from './context/DashboardContext'
import { PageRouter } from './pages'

import "leaflet/dist/leaflet.css"
import {
  MapContainer,
  TileLayer,
  Marker,
  Popup,
  Tooltip as LeafletTooltip,
} from "react-leaflet";



type TabId = 'overview' | 'devices' | 'dataframes' | 'connectivity' | 'analytics' | 'help' | 'docs'

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

type PMUMeta = {
  substation: string
  region: string
  state: string
  voltage: string
  vendor: string
  primaryIp: string
  redundantIp: string
  targetFps: number
  lat: number
  lon: number
}

type PMUConfig = {
  name: string
  ip: string
  port: number
  idcode: number
  region: string
  protocol: string
  lat: number
  lon: number
}

const emptyState: DashboardState = {
  nowUtc: new Date().toISOString(),
  pmus: [],
  eventCount: 0,
}

function round(v: number, digits = 2) {
  return Number(v.toFixed(digits))
}

function formatTS(ts: number) {
  return new Date(ts).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function toneFromStatus(connected: boolean, loss: number) {
  if (!connected) return 'bad'
  if (loss > 1) return 'warn'
  return 'ok'
}

function pmuKey(name: string) {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-')
}

function metaForDB(name: string, config?: PMUConfig): PMUMeta {
  return {
    substation: config?.name || name,
    region: config?.region || 'Unknown',
    state: '-',
    voltage: '-',
    vendor: 'Generic PMU',
    primaryIp: config?.ip || '0.0.0.0',
    redundantIp: '-',
    targetFps: 50,
    lat: config?.lat || 20,
    lon: config?.lon || 70,
  }
}

function availabilityOf(pmu: LivePMUState, targetFps: number) {
  if (!pmu.connected) return 0
  const fpsRatio = Math.min(1.05, pmu.approxFps / Math.max(1, targetFps))
  const rejectPenalty = Math.min(8, pmu.qualityRejects * 0.03)
  return Math.max(0, Math.min(100, fpsRatio * 100 - rejectPenalty))
}

function packetLossOf(pmu: LivePMUState) {
  if (pmu.totalFrames <= 0) return 0
  return (pmu.qualityRejects / pmu.totalFrames) * 100
}

function latencyOf(pmu: LivePMUState) {
  if (!pmu.connected) return 999
  const base = 1000 / Math.max(1, pmu.approxFps)
  const rocofDrift = Math.abs(pmu.lastReading?.rocof ?? 0) * 350
  return Math.max(8, base * 7 + rocofDrift)
}

function jitterOf(pmu: LivePMUState) {
  if (!pmu.connected) return 99
  const points = pmu.trends.slice(-30)
  if (points.length < 3) return 2
  const mean = points.reduce((sum, p) => sum + p.frequency, 0) / points.length
  const variance = points.reduce((sum, p) => sum + (p.frequency - mean) ** 2, 0) / points.length
  return Math.sqrt(variance) * 100
}

function ageText(iso: string) {
  if (!iso) return '--'
  const deltaMs = Date.now() - new Date(iso).getTime()
  if (deltaMs < 2000) return `${Math.max(1, Math.round(deltaMs))} ms ago`
  if (deltaMs < 60000) return `${round(deltaMs / 1000, 1)} s ago`
  return `${Math.floor(deltaMs / 60000)} min ago`
}

function App() {
  const [dashboard, setDashboard] = useState<DashboardState>(emptyState)
  const [dbConfigs, setDbConfigs] = useState<PMUConfig[]>([])
  const [events, setEvents] = useState<ConversationEvent[]>([])
  const [streamOnline, setStreamOnline] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
  const [clockLabel, setClockLabel] = useState('')
  const [activeTab, setActiveTab] = useState<TabId>('overview')
  const [selectedPMUName, setSelectedPMUName] = useState('')
  const [selectedFramePMUName, setSelectedFramePMUName] = useState('')
  const [drawerPMUName, setDrawerPMUName] = useState('')
  const [deviceSearch, setDeviceSearch] = useState('')
  const [regionFilter, setRegionFilter] = useState('ALL')
  const [statusFilter, setStatusFilter] = useState('ALL')
  const [mapFilter, setMapFilter] = useState<'all' | 'issues'>('all')
  const [helpQuery, setHelpQuery] = useState('')
  const [frameLines, setFrameLines] = useState<string[]>([])
  const [openFaq, setOpenFaq] = useState<number | null>(null)
  const [showAddPMU, setShowAddPMU] = useState(false)
  const [newPMU, setNewPMU] = useState({ name: '', ip: '', port: 4712, idcode: 1, region: 'NRLDC', protocol: 'tcp', lat: 20.0, lon: 70.0 })
  const INDIA_CENTER: [number, number] = [22.5, 80.5]; 

  const handleAddPMU = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const res = await fetch('/api/pmus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newPMU),
      })
      if (!res.ok) {
        const text = await res.text()
        alert('Failed to connect PMU: ' + text)
        return
      }
      setShowAddPMU(false)
    } catch (err: any) {
      alert('Error connecting PMU: ' + err.message)
      console.error(err)
    }
  }

  const handleDeletePMU = async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm(`Disconnect and delete PMU ${name}?`)) return
    try {
      const res = await fetch(`/api/pmus/${name}`, { method: 'DELETE' })
      if (!res.ok) {
        const text = await res.text()
        alert('Failed to delete PMU: ' + text)
      }
    } catch (err: any) {
      alert('Error deleting PMU: ' + err.message)
      console.error(err)
    }
  }

  useEffect(() => {
    const tickClock = () => {
      setClockLabel(
        new Date().toLocaleTimeString('en-IN', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        }),
      )
    }
    tickClock()
    const timer = window.setInterval(tickClock, 1000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    if (isPaused) return

    const refresh = async () => {
      try {
        const [stateRes, configRes] = await Promise.all([
          fetch('/conversation/state'),
          fetch('/api/pmus')
        ])
        
        if (stateRes.ok) {
          const data = (await stateRes.json()) as DashboardState
          setDashboard(data)
        }
        if (configRes.ok) {
          const configs = (await configRes.json()) as PMUConfig[]
          setDbConfigs(configs)
        }
      } catch {
        // Keep last good snapshot visible.
      }
    }

    void refresh()
    const timer = window.setInterval(refresh, 1000)
    return () => window.clearInterval(timer)
  }, [isPaused])

  useEffect(() => {
    if (isPaused) {
      setStreamOnline(false)
      return
    }

    const es = new EventSource('/conversation/events')
    es.onopen = () => setStreamOnline(true)
    es.onerror = () => setStreamOnline(false)
    es.onmessage = (evt) => {
      try {
        const parsed = JSON.parse(evt.data) as ConversationEvent
        setEvents((current) => [parsed, ...current].slice(0, 120))
      } catch {
        // Ignore malformed messages.
      }
    }
    return () => es.close()
  }, [isPaused])

  const pmus = useMemo(() => {
    // We map all PMUs from DB configs and overlay active state if present
    return dbConfigs.map((cfg) => {
      const activeState = dashboard.pmus.find(p => p.name === cfg.name)
      if (activeState) {
        return {
          ...activeState,
          meta: metaForDB(cfg.name, cfg),
        }
      }
      
      // If PMU is configured but not active/no events yet, create a dummy offline state
      const dummyState: LivePMUState = {
        name: cfg.name,
        connected: false,
        connectionText: 'disconnected',
        lastEventTime: '',
        lastFrameTime: '',
        lastHandshake: '',
        lastError: 'waiting for connection',
        totalFrames: 0,
        approxFps: 0,
        qualityRejects: 0,
        kafkaErrors: 0,
        sinkErrors: 0,
        spoolQueued: 0,
        lastReading: { ts: 0, frequency: 0, mw: 0, mvar: 0, rocof: 0 },
        lastPhasor: { va: { magnitude: 0, angleDeg: 0 }, vb: { magnitude: 0, angleDeg: 0 }, vc: { magnitude: 0, angleDeg: 0 }, ia: { magnitude: 0, angleDeg: 0 }, ts: 0 },
        trends: []
      }
      return {
        ...dummyState,
        meta: metaForDB(cfg.name, cfg),
      }
    })
  }, [dashboard.pmus, dbConfigs])

  useEffect(() => {
    if (!pmus.length) {
      setSelectedPMUName('')
      setSelectedFramePMUName('')
      return
    }

    if (!pmus.some((pmu) => pmu.name === selectedPMUName)) {
      setSelectedPMUName(pmus[0].name)
    }
    if (!pmus.some((pmu) => pmu.name === selectedFramePMUName)) {
      setSelectedFramePMUName(pmus[0].name)
    }
  }, [pmus, selectedPMUName, selectedFramePMUName])

  const selectedPMU = pmus.find((pmu) => pmu.name === selectedPMUName) ?? pmus[0]
  const selectedFramePMU = pmus.find((pmu) => pmu.name === selectedFramePMUName) ?? pmus[0]
  const drawerPMU = pmus.find((pmu) => pmu.name === drawerPMUName)

  const regionSummary = useMemo(() => {
    const map = new Map<string, { total: number; connected: number; availability: number }>()
    for (const pmu of pmus) {
      const current = map.get(pmu.meta.region) ?? { total: 0, connected: 0, availability: 0 }
      current.total += 1
      if (pmu.connected) current.connected += 1
      current.availability += availabilityOf(pmu, pmu.meta.targetFps)
      map.set(pmu.meta.region, current)
    }
    return Array.from(map.entries()).map(([region, value]) => ({
      region,
      total: value.total,
      connected: value.connected,
      availability: value.total ? value.availability / value.total : 0,
    }))
  }, [pmus])

  const mergedTrend = useMemo(() => {
    const rows = new Map<number, Record<string, number>>()
    for (const pmu of pmus) {
      const key = pmuKey(pmu.name)
      const trend = pmu.trends.slice(-120)
      for (const point of trend) {
        const row = rows.get(point.ts) ?? { ts: point.ts }
        row[`${key}__frequency`] = point.frequency
        row[`${key}__mw`] = point.mw
        row[`${key}__mvar`] = point.mvar
        row[`${key}__rocof`] = point.rocof
        rows.set(point.ts, row)
      }
    }
    return Array.from(rows.values()).sort((a, b) => a.ts - b.ts)
  }, [pmus])

  const systemCounts = useMemo(() => {
    const connected = pmus.filter((pmu) => pmu.connected).length
    const disconnected = pmus.length - connected
    const totalErrors = pmus.reduce((sum, pmu) => sum + pmu.kafkaErrors + pmu.sinkErrors, 0)
    const spool = pmus.reduce((sum, pmu) => sum + pmu.spoolQueued, 0)
    return { connected, disconnected, totalErrors, spool }
  }, [pmus])

  const frameRate = selectedPMU?.approxFps ?? 0
  const systemTone = systemCounts.disconnected >= 2 ? 'bad' : systemCounts.disconnected > 0 ? 'warn' : 'ok'
  const systemMessage =
    systemCounts.disconnected >= 2
      ? `${systemCounts.disconnected} streams offline - operator action required`
      : systemCounts.disconnected > 0
        ? `${systemCounts.disconnected} streams need attention`
        : 'All systems nominal'

  const healthyCount = pmus.filter(p => p.connected && packetLossOf(p) <= 1).length;
  const degradedCount = pmus.filter(p => p.connected && packetLossOf(p) > 1).length;
  const offlineCount = systemCounts.disconnected;
  const avgAvailability = pmus.length ? round(pmus.reduce((sum, p) => sum + availabilityOf(p, p.meta.targetFps), 0) / pmus.length, 1) : 0;
  const totalFps = Math.round(pmus.reduce((sum, p) => sum + p.approxFps, 0));
  const uniqueSubstations = new Set(pmus.map(p => p.meta.substation)).size;

  const statusCards = [
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
  ]

  const liveAlerts = useMemo(() => {
    const fromEvents = events.slice(0, 18).map((evt) => {
      const lowered = `${evt.status} ${evt.message}`.toLowerCase()
      const sev = lowered.includes('error')
        ? 'bad'
        : lowered.includes('warn') || lowered.includes('reject')
          ? 'warn'
          : 'info'
      return {
        sev,
        title: evt.stage,
        msg: `${evt.pmu} - ${evt.message}`,
        time: new Date(evt.time).toLocaleTimeString(),
      }
    })

    const fromState = pmus
      .filter((pmu) => !pmu.connected || packetLossOf(pmu) > 1)
      .map((pmu) => ({
        sev: pmu.connected ? 'warn' : 'bad',
        title: pmu.connected ? 'Elevated packet loss' : 'PMU offline',
        msg: `${pmu.name} - ${pmu.connected ? `${round(packetLossOf(pmu), 2)}% loss` : 'stream disconnected'}`,
        time: ageText(pmu.lastEventTime),
      }))

    return [...fromState, ...fromEvents].slice(0, 24)
  }, [events, pmus])

  const filteredDevices = useMemo(() => {
    return pmus.filter((pmu) => {
      const matchesSearch = `${pmu.name} ${pmu.meta.substation} ${pmu.meta.vendor} ${pmu.meta.primaryIp}`
        .toLowerCase()
        .includes(deviceSearch.toLowerCase())
      const tone = toneFromStatus(pmu.connected, packetLossOf(pmu))
      const matchesStatus = statusFilter === 'ALL' ? true : statusFilter === tone.toUpperCase()
      const matchesRegion = regionFilter === 'ALL' ? true : pmu.meta.region === regionFilter
      return matchesSearch && matchesStatus && matchesRegion
    })
  }, [pmus, deviceSearch, statusFilter, regionFilter])

  useEffect(() => {
    if (!selectedFramePMU) return
    if (isPaused) return

    const timer = window.setInterval(() => {
      setFrameLines((current) => {
        const selected = pmus.find((pmu) => pmu.name === selectedFramePMUName)
        if (!selected) return current
        const now = Date.now()
        const soc = Math.floor(now / 1000)
        const frac = String(Math.floor((now % 1000) * 1000)).padStart(6, '0')
        const line = `SOC ${soc}  FRACSEC ${frac}  F ${(selected.lastReading?.frequency ?? 0).toFixed(4)}  ROCOF ${(selected.lastReading?.rocof ?? 0).toFixed(4)}  STAT ${selected.connected ? '0x0000' : '0x2000'}`
        return [line, ...current].slice(0, 45)
      })
    }, 1000)

    return () => window.clearInterval(timer)
  }, [isPaused, pmus, selectedFramePMU, selectedFramePMUName])

  const connectivityRows = useMemo(() => {
    return pmus
      .map((pmu) => {
        const loss = packetLossOf(pmu)
        const latency = latencyOf(pmu)
        const jitter = jitterOf(pmu)
        const avail = availabilityOf(pmu, pmu.meta.targetFps)
        let recommendation = 'Healthy'
        if (!pmu.connected) recommendation = 'Restart stream and verify source link'
        else if (loss > 2) recommendation = 'Inspect quality gate and packet path'
        else if (latency > 120) recommendation = 'Review network route and queueing'
        return {
          ...pmu,
          loss,
          latency,
          jitter,
          avail,
          recommendation,
          tone: toneFromStatus(pmu.connected, loss),
        }
      })
      .sort((a, b) => {
        if (a.connected !== b.connected) return a.connected ? 1 : -1
        return b.loss - a.loss
      })
  }, [pmus])

  const anglePairs = useMemo(() => {
    if (pmus.length < 2) return [] as Array<{ name: string; value: number }>
    const pairs: Array<{ name: string; value: number }> = []
    for (let i = 0; i < pmus.length; i++) {
      for (let j = i + 1; j < pmus.length; j++) {
        const a = pmus[i]
        const b = pmus[j]
        const diff =
          Math.abs((a.lastPhasor?.va?.angleDeg ?? 0) - (b.lastPhasor?.va?.angleDeg ?? 0)) +
          Math.abs((a.lastReading?.frequency ?? 50) - (b.lastReading?.frequency ?? 50)) * 800
        pairs.push({ name: `${a.name} ↔ ${b.name}`, value: diff })
      }
    }
    return pairs
  }, [pmus])

  const analyticsRecs = useMemo(() => {
    const recs: Array<{ sev: 'bad' | 'warn' | 'info'; title: string; desc: string }> = []
    const worstAngle = [...anglePairs].sort((a, b) => b.value - a.value)[0]
    if (worstAngle && worstAngle.value > 25) {
      recs.push({
        sev: 'bad',
        title: `Reduce corridor stress on ${worstAngle.name}`,
        desc: `Angle separation ${round(worstAngle.value, 1)}° exceeds advisory margin.`,
      })
    }

    connectivityRows.forEach((row) => {
      if (!row.connected) {
        recs.push({
          sev: 'warn',
          title: `Recover ${row.name}`,
          desc: 'Stream disconnected. Restart simulator stream and check receiver path.',
        })
      }
    })

    if (!recs.length) {
      recs.push({
        sev: 'info',
        title: 'All simulator lanes stable',
        desc: 'Continue monitoring frequency, MW, MVAR and ROCOF drift windows.',
      })
    }
    return recs.slice(0, 5)
  }, [anglePairs, connectivityRows])

  const helpSections = [
    {
      title: 'Getting Started',
      content: 'This dashboard now uses the three simulator streams from /conversation/state in every tab.',
    },
    {
      title: 'Data Frames',
      content: 'Use Data Frames tab to inspect per-simulator SOC/FRACSEC and phasor values in near real-time.',
    },
    {
      title: 'Connectivity',
      content: 'Connectivity metrics are computed from live simulator frame rates, rejects, and trend variance.',
    },
  ]

  const filteredHelp = helpSections.filter((item) => {
    if (!helpQuery.trim()) return true
    return `${item.title} ${item.content}`.toLowerCase().includes(helpQuery.toLowerCase())
  })

  const phasorItems: Array<{ label: string; value: PhasorVector | undefined }> = [
    { label: 'VA', value: selectedFramePMU?.lastPhasor?.va },
    { label: 'VB', value: selectedFramePMU?.lastPhasor?.vb },
    { label: 'VC', value: selectedFramePMU?.lastPhasor?.vc },
    { label: 'IA', value: selectedFramePMU?.lastPhasor?.ia },
  ]

  const navItems: Array<{ id: TabId; label: string; section: 'Monitoring' | 'Resources'; badge?: string; bad?: boolean }> = [
    { id: 'overview', label: 'Overview', section: 'Monitoring' },
    { id: 'devices', label: 'Device Inventory', section: 'Monitoring', badge: `${pmus.length}` },
    { id: 'dataframes', label: 'Data Frames', section: 'Monitoring' },
    {
      id: 'connectivity',
      label: 'Connectivity',
      section: 'Monitoring',
      badge: `${connectivityRows.filter((row) => row.tone !== 'ok').length}`,
      bad: connectivityRows.some((row) => row.tone !== 'ok'),
    },
    { id: 'analytics', label: 'Analytics', section: 'Monitoring' },
    { id: 'help', label: 'Help & Support', section: 'Resources' },
    { id: 'docs', label: 'Documentation', section: 'Resources' },
  ]

  return (
    <div className="console-root">
      <Header />

      <div className="console-shell">
        <Sidebar />

        <main className="main">
          <div className="app-shell">
            <div className="page-title">
              <div>
                <h2>{activeTab[0].toUpperCase() + activeTab.slice(1)}</h2>
                <p>Live telemetry mapped from the three simulator streams across this section.</p>
              </div>
              <div className="page-actions">
                <button type="button" className="btn ghost">
                  <BookOpen size={14} /> Runbook
                </button>
                <button type="button" className="btn">
                  <TimerReset size={14} /> {new Date(dashboard.nowUtc).toLocaleTimeString()}
                </button>
                <button type="button" className="btn primary">
                  <Radio size={14} /> {streamOnline && !isPaused ? 'Live stream' : 'Standby'}
                </button>
              </div>
            </div>

            {activeTab === 'overview' && (
              <>
                <section className="kpi-grid">
                  {statusCards.map((card) => (
                    <article key={card.label} className={`kpi-card ${card.tone}`}>
                      <div className="icon-wrap">{card.icon}</div>
                      <p className="kpi-meta">{card.label}</p>
                      <h2 className="kpi-value">{card.value}</h2>
                      {card.subtext && <p className="kpi-subtext" style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{card.subtext}</p>}
                      {card.trend && <p className="kpi-trend" style={{ fontSize: '0.8rem', color: 'var(--ok-color)', marginTop: '4px' }}>{card.trend}</p>}
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
                              const color = ['#4de0ff', '#7dff93', '#ffd166', '#ff719a', '#ab82ff', '#ffa84d'][idx % 6];
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
                              );
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
                          <button type="button" className="plot-action" onClick={() => setMapFilter('all')}>All</button>
                          <button type="button" className="plot-action" onClick={() => setMapFilter('issues')}>Issues</button>
                        </div>
                      </div>
                      <div className="map-wrap-react">
  <MapContainer
    center={INDIA_CENTER}
    zoom={5}
    style={{
      height: "450px",
      width: "100%",
    }}
  >
    <TileLayer
      url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
    />

    {pmus
      .filter((pmu) =>
        mapFilter === "issues"
          ? !pmu.connected || packetLossOf(pmu) > 1
          : true
      )
      .map((pmu) => (
        <Marker
          key={pmu.name}
          position={[
            pmu.meta.lat,
            pmu.meta.lon,
          ]}
          eventHandlers={{
            click: () => setDrawerPMUName(pmu.name),
          }}
        >
          <LeafletTooltip direction="top">
            {pmu.name}
          </LeafletTooltip>

          <Popup>
            <strong>{pmu.name}</strong>
            <br />
            Region: {pmu.meta.region}
            <br />
            Status: {pmu.connected ? "Connected" : "Offline"}
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
                              <span className={`status-chip ${alert.sev === 'bad' ? 'bad' : alert.sev === 'warn' ? 'warn' : 'ok'}`}>
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
            )}

            {activeTab === 'devices' && (
              <section className="panel">
                <div className="panel-head">
                  <div>
                    <h3>Device Inventory</h3>
                    <p>Manage and monitor PMU connections.</p>
                  </div>
                  <div className="panel-tools-inline">
                    <button type="button" className="btn primary" onClick={() => setShowAddPMU(true)}>
                      + Add PMU
                    </button>
                  </div>
                </div>
                <div className="device-filter-row">
                  <input
                    value={deviceSearch}
                    onChange={(event) => setDeviceSearch(event.target.value)}
                    placeholder="Search PMU / substation / IP"
                  />
                  <select value={regionFilter} onChange={(event) => setRegionFilter(event.target.value)}>
                    <option value="ALL">All regions</option>
                    {Array.from(new Set(pmus.map((pmu) => pmu.meta.region))).map((region) => (
                      <option key={region} value={region}>{region}</option>
                    ))}
                  </select>
                  <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
                    <option value="ALL">All status</option>
                    <option value="OK">Healthy</option>
                    <option value="WARN">Degraded</option>
                    <option value="BAD">Offline</option>
                  </select>
                </div>

                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>PMU</th>
                        <th>Substation</th>
                        <th>Region</th>
                        <th>Vendor</th>
                        <th>IP</th>
                        <th>FPS</th>
                        <th>Status</th>
                        <th>Availability</th>
                        <th>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredDevices.map((pmu) => {
                        const loss = packetLossOf(pmu)
                        const tone = toneFromStatus(pmu.connected, loss)
                        return (
                          <tr key={pmu.name} onClick={() => setDrawerPMUName(pmu.name)}>
                            <td><strong>{pmu.name}</strong></td>
                            <td>{pmu.meta.substation}</td>
                            <td>{pmu.meta.region}</td>
                            <td>{pmu.meta.vendor}</td>
                            <td>{pmu.meta.primaryIp}</td>
                            <td>{round(pmu.approxFps, 1)}</td>
                            <td><span className={`status-chip ${tone}`}>{pmu.connected ? 'Healthy' : 'Offline'}</span></td>
                            <td>{round(availabilityOf(pmu, pmu.meta.targetFps), 1)}%</td>
                            <td>
                              <button type="button" className="btn ghost" style={{color: 'var(--err)', padding: '2px 6px'}} onClick={(e) => handleDeletePMU(pmu.name, e)}>
                                Disconnect
                              </button>
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>

                <div className="charts-grid compact-gap">
                  <article className="panel chart-panel">
                    <div className="panel-head"><h3>Availability by PMU</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
                        <BarChart data={pmus.map((pmu) => ({ name: pmu.name, avail: round(availabilityOf(pmu, pmu.meta.targetFps), 2) }))}>
                          <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                          <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis domain={[0, 100]} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <Tooltip />
                          <Bar dataKey="avail" radius={[8, 8, 0, 0]}>
                            {pmus.map((pmu) => (
                              <Cell key={pmu.name} fill={pmu.connected ? '#5de8aa' : '#f0706a'} />
                            ))}
                          </Bar>
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  </article>

                  <article className="panel chart-panel">
                    <div className="panel-head"><h3>Status Distribution</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
                        <PieChart>
                          <Pie
                            data={[
                              { name: 'Healthy', value: pmus.filter((pmu) => pmu.connected).length, color: '#5de8aa' },
                              { name: 'Offline', value: pmus.filter((pmu) => !pmu.connected).length, color: '#f0706a' },
                            ]}
                            dataKey="value"
                            nameKey="name"
                            outerRadius={90}
                          >
                            {[
                              { name: 'Healthy', value: pmus.filter((pmu) => pmu.connected).length, color: '#5de8aa' },
                              { name: 'Offline', value: pmus.filter((pmu) => !pmu.connected).length, color: '#f0706a' },
                            ].map((entry) => (
                              <Cell key={entry.name} fill={entry.color} />
                            ))}
                          </Pie>
                          <Tooltip />
                          <Legend />
                        </PieChart>
                      </ResponsiveContainer>
                    </div>
                  </article>
                </div>
              </section>
            )}

            {activeTab === 'dataframes' && selectedFramePMU && (
              <>
                <section className="panel">
                  <div className="panel-head">
                    <div>
                      <h3>Data Frame Information</h3>
                      <p>Live C37.118-like frame decoding from simulator stream.</p>
                    </div>
                    <div className="panel-tools-inline">
                      <select value={selectedFramePMUName} onChange={(event) => setSelectedFramePMUName(event.target.value)}>
                        {pmus.map((pmu) => (
                          <option key={pmu.name} value={pmu.name}>{pmu.name}</option>
                        ))}
                      </select>
                      <button type="button" className="btn" onClick={() => setIsPaused((current) => !current)}>
                        {isPaused ? 'Resume stream' : 'Pause stream'}
                      </button>
                    </div>
                  </div>

                  <div className="main-grid dashboard-grid">
                    <div className="panel frame-panel">
                      <div className="panel-head"><h3>Configuration Frame (CFG-2)</h3></div>
                      <div className="frame-box-react">
                        <div>SYNC: 0xAA31</div>
                        <div>IDCODE: {selectedFramePMU.name}</div>
                        <div>STATION: {selectedFramePMU.meta.substation}</div>
                        <div>FPS: {round(selectedFramePMU.approxFps, 2)}</div>
                        <div>PHASORS: VA, VB, VC, IA</div>
                        <div>ANALOG: MW, MVAR</div>
                        <div>DIGITAL: quality/status flags</div>
                      </div>
                    </div>

                    <div className="panel frame-panel">
                      <div className="panel-head"><h3>Live Data Frame</h3></div>
                      <div className="frame-box-react">
                        {frameLines.length === 0 && <div>Waiting for live frames...</div>}
                        {frameLines.map((line, idx) => (
                          <div key={`${line}-${idx}`}>{line}</div>
                        ))}
                      </div>
                    </div>
                  </div>
                </section>

                <section className="charts-grid compact-gap">
                  <article className="panel chart-panel">
                    <div className="panel-head"><h3>Frequency & ROCOF</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={selectedFramePMU.trends.slice(-120)}>
                          <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                          <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis yAxisId="left" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis yAxisId="right" orientation="right" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <Tooltip labelFormatter={(value) => formatTS(Number(value))} />
                          <Legend />
                          <Line yAxisId="left" type="monotone" dataKey="frequency" stroke="#4de0ff" dot={false} strokeWidth={2} />
                          <Line yAxisId="right" type="monotone" dataKey="rocof" stroke="#ffd166" dot={false} strokeWidth={2} />
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                  </article>

                  <article className="panel">
                    <div className="panel-head"><h3>Phasor Quantities</h3></div>
                    <div className="phasor-grid-react">
                      {phasorItems.map((item) => (
                        <article key={item.label} className="phasor-react-card">
                          <p>{item.label}</p>
                          <strong>{item.value ? round(item.value.magnitude, 2) : '--'}</strong>
                          <span>{item.value ? `${round(item.value.angleDeg, 2)}°` : '--'}</span>
                        </article>
                      ))}
                    </div>
                  </article>
                </section>
              </>
            )}

            {activeTab === 'connectivity' && (
              <>
                <section className="kpi-grid">
                  {[
                    { label: 'Active issues', value: connectivityRows.filter((row) => row.tone !== 'ok').length, sub: 'across simulator streams' },
                    { label: 'Offline streams', value: connectivityRows.filter((row) => !row.connected).length, sub: 'no fresh frames' },
                    { label: 'High loss (>1%)', value: connectivityRows.filter((row) => row.loss > 1).length, sub: 'quality rejects ratio' },
                    { label: 'Avg latency', value: `${round(connectivityRows.reduce((sum, row) => sum + row.latency, 0) / Math.max(1, connectivityRows.length), 1)} ms`, sub: 'derived from fps + rocof drift' },
                  ].map((item) => (
                    <article key={item.label} className="kpi-card neutral">
                      <p className="kpi-meta">{item.label}</p>
                      <h2 className="kpi-value">{item.value}</h2>
                      <p className="kpi-meta">{item.sub}</p>
                    </article>
                  ))}
                </section>

                <section className="main-grid dashboard-grid">
                  <div className="panel">
                    <div className="panel-head"><h3>Connectivity Recommendations</h3></div>
                    <div className="rec-list-react">
                      {connectivityRows.map((row) => (
                        <article key={row.name} className={`rec-react ${row.tone}`}>
                          <strong>{row.name}</strong>
                          <p>{row.recommendation}</p>
                        </article>
                      ))}
                    </div>
                  </div>

                  <div className="panel chart-panel">
                    <div className="panel-head"><h3>Latency Trend by PMU</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={mergedTrend}>
                          <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                          <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <Tooltip labelFormatter={(value) => formatTS(Number(value))} />
                          <Legend />
                          {pmus.map((pmu, idx) => (
                            <Line
                              key={`${pmu.name}-latency`}
                              type="monotone"
                              dataKey={`${pmuKey(pmu.name)}__rocof`}
                              name={pmu.name}
                              stroke={['#ff7b7b', '#ffd166', '#4de0ff'][idx % 3]}
                              dot={false}
                              strokeWidth={2}
                            />
                          ))}
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                  </div>
                </section>

                <section className="panel table-panel">
                  <div className="panel-head"><h3>Per-PMU Connectivity Matrix</h3></div>
                  <div className="table-wrap">
                    <table>
                      <thead>
                        <tr>
                          <th>PMU</th>
                          <th>Region</th>
                          <th>Latency (ms)</th>
                          <th>Jitter (ms)</th>
                          <th>Loss %</th>
                          <th>Avail %</th>
                          <th>Last frame</th>
                          <th>Status</th>
                          <th>Recommendation</th>
                        </tr>
                      </thead>
                      <tbody>
                        {connectivityRows.map((row) => (
                          <tr key={row.name} onClick={() => setDrawerPMUName(row.name)}>
                            <td><strong>{row.name}</strong></td>
                            <td>{row.meta.region}</td>
                            <td>{round(row.latency, 1)}</td>
                            <td>{round(row.jitter, 2)}</td>
                            <td>{round(row.loss, 3)}</td>
                            <td>{round(row.avail, 1)}</td>
                            <td>{ageText(row.lastFrameTime)}</td>
                            <td><span className={`status-chip ${row.tone}`}>{row.tone}</span></td>
                            <td>{row.recommendation}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              </>
            )}

            {activeTab === 'analytics' && (
              <>
                <section className="kpi-grid">
                  <article className="kpi-card warn">
                    <p className="kpi-meta">Max angle delta</p>
                    <h2 className="kpi-value">{round(Math.max(0, ...anglePairs.map((item) => item.value)), 2)}°</h2>
                  </article>
                  <article className="kpi-card ok">
                    <p className="kpi-meta">Avg frequency</p>
                    <h2 className="kpi-value">
                      {round(pmus.reduce((sum, pmu) => sum + (pmu.lastReading?.frequency ?? 0), 0) / Math.max(1, pmus.length), 4)} Hz
                    </h2>
                  </article>
                  <article className="kpi-card neutral">
                    <p className="kpi-meta">Advisories</p>
                    <h2 className="kpi-value">{analyticsRecs.length}</h2>
                  </article>
                </section>

                <section className="main-grid dashboard-grid">
                  <div className="panel chart-panel">
                    <div className="panel-head"><h3>Voltage Angle Differences</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
                        <BarChart data={anglePairs}>
                          <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                          <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <Tooltip />
                          <Bar dataKey="value" fill="#8b6cf5" radius={[8, 8, 0, 0]} />
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  </div>

                  <div className="panel chart-panel">
                    <div className="panel-head"><h3>Oscillation Detection</h3></div>
                    <div className="chart-wrap small">
                      <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
                        <ScatterChart>
                          <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                          <XAxis type="number" dataKey="freq" name="freq" unit="Hz" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <YAxis type="number" dataKey="damping" name="damping" unit="%" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                          <Tooltip cursor={{ strokeDasharray: '3 3' }} />
                          <Scatter
                            name="Modes"
                            data={pmus.map((pmu) => ({
                              freq: Math.abs(pmu.lastReading?.rocof ?? 0) * 10 + 0.2,
                              damping: Math.max(2, 14 - Math.abs(pmu.lastReading?.rocof ?? 0) * 80),
                              pmu: pmu.name,
                            }))}
                            fill="#4de0ff"
                          />
                        </ScatterChart>
                      </ResponsiveContainer>
                    </div>
                  </div>
                </section>

                <section className="panel">
                  <div className="panel-head"><h3>Operator Recommendations</h3></div>
                  <div className="rec-list-react">
                    {analyticsRecs.map((rec, idx) => (
                      <article key={`${rec.title}-${idx}`} className={`rec-react ${rec.sev}`}>
                        <strong>{rec.title}</strong>
                        <p>{rec.desc}</p>
                      </article>
                    ))}
                  </div>
                </section>
              </>
            )}

            {activeTab === 'help' && (
              <section className="panel">
                <div className="panel-head">
                  <div>
                    <h3>Help & Support</h3>
                    <p>Updated for dynamic live React dashboard workflow.</p>
                  </div>
                </div>
                <div className="device-filter-row">
                  <input
                    value={helpQuery}
                    onChange={(event) => setHelpQuery(event.target.value)}
                    placeholder="Search help topics"
                  />
                </div>
                <div className="help-list-react">
                  {filteredHelp.map((item) => (
                    <article key={item.title} className="help-card-react">
                      <h4>{item.title}</h4>
                      <p>{item.content}</p>
                    </article>
                  ))}
                </div>

                <div className="faq-list-react">
                  {[
                    {
                      q: 'Why are some PMUs offline?',
                      a: 'This means they are configured in the database, but no live data is currently streaming.',
                    },
                    {
                      q: 'Where does data come from?',
                      a: 'The dashboard polls /conversation/state and subscribes to /conversation/events (SSE) exposed by the Go backend.',
                    },
                    {
                      q: 'How do I pause updates?',
                      a: 'Use Pause in the header. It pauses both polling and SSE consumption in this React dashboard.',
                    },
                  ].map((item, idx) => (
                    <article key={item.q} className="faq-item-react">
                      <button type="button" onClick={() => setOpenFaq((current) => (current === idx ? null : idx))}>
                        {item.q}
                      </button>
                      {openFaq === idx && <p>{item.a}</p>}
                    </article>
                  ))}
                </div>
              </section>
            )}

            {activeTab === 'docs' && (
              <section className="panel help-card-react">
                <h3>Documentation</h3>
                <p>This React dashboard now maps fully dynamic data from InfluxDB across all sections.</p>
                <ul>
                  <li>Live source: /conversation/state (poll 1s) and /conversation/events (SSE).</li>
                  <li>Config source: /api/pmus mapped from InfluxDB.</li>
                  <li>Overview: map, alerts, frequency charts from live trends.</li>
                  <li>Data Frames: selected PMU frame stream, CFG and phasor values.</li>
                  <li>Connectivity: latency/jitter/loss/availability derived per simulator PMU.</li>
                  <li>Analytics: angle deltas and modal hints derived from live phasor and trend data.</li>
                </ul>
              </section>
            )}
          </div>
        </main>
      </div>

      <PMUDetailDrawer />
      <AddPMUModal />
    </div>
  )
}

function App() {
  return (
    <DashboardProvider>
      <DashboardShell />
    </DashboardProvider>
  )
}

export default App
