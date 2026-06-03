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

const registryStorageKey = 'pdc.dashboard.registry.v3'
const legacySeededDisplayNames = new Set(['Jaipur-PMU1 - Jaipur'])

const emptyState: DashboardState = {
  nowUtc: new Date().toISOString(),
  pmus: [],
  eventCount: 0,
}

const defaultForm: PMUFormState = {
  id: '',
  displayName: '',
  substation: '',
  region: '',
  voltageClass: '',
  vendorModel: '',
  primaryIp: '',
  redundantIp: '',
  reportingRate: '',
  commissioned: '',
  status: '',
  dataAvailability: '',
  latency: '',
  jitter: '',
  packetLoss: '',
  signalChannels: '',
  notes: '',
}

const regionOrder = ['NRLDC', 'WRLDC', 'NR', 'SR', 'ER', 'NER', 'ALL']

const pmuPresets: PMUPreset[] = [

  {
    id: 'pmu-2',
    label: 'Simulator 2',
    values: {
      id: '7005',
      displayName: 'Alwar-PMU2 - Alwar',
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
      displayName: 'Neemrana-PMU3 - Neemrana',
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
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw) as PMURecord[]
    if (!Array.isArray(parsed)) return []
    return parsed.filter((record) => !legacySeededDisplayNames.has(record.displayName))
  } catch {
    return []
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

function parseNumeric(raw: string) {
  const cleaned = raw.replace(/[^0-9.+-]/g, '')
  const value = Number(cleaned)
  return Number.isFinite(value) ? value : null
}

function phasorToXY(vector: PhasorVector, radius: number) {
  const radians = (vector.angleDeg * Math.PI) / 180
  const magnitude = Math.max(0, Math.min(1, vector.magnitude / 100)) * radius
  return {
    x: Math.cos(radians) * magnitude,
    y: -Math.sin(radians) * magnitude,
  }
}

function PhasorPlot({ snapshot, pmuName }: { snapshot?: PhasorSnapshot; pmuName?: string }) {
  const center = 120
  const radius = 82

  const vectors = [
    { label: 'VA', color: '#4de0ff', value: snapshot?.va },
    { label: 'VB', color: '#7dff93', value: snapshot?.vb },
    { label: 'VC', color: '#ffd166', value: snapshot?.vc },
    { label: 'IA', color: '#ff7b7b', value: snapshot?.ia },
  ]

  const compassMarks = [
    { label: '0°', x: center + radius + 13, y: center + 4 },
    { label: '90°', x: center - 11, y: center - radius - 11 },
    { label: '180°', x: center - radius - 38, y: center + 4 },
    { label: '270°', x: center - 17, y: center + radius + 23 },
  ]

  return (
    <div className="phasor-frame">
      <div className="phasor-canvas">
        <svg viewBox="0 0 240 240" className="phasor-svg" aria-label="Phasor plot">
          <defs>
            <radialGradient id="phasor-core" cx="50%" cy="45%" r="70%">
              <stop offset="0%" stopColor="rgba(99, 210, 245, 0.22)" />
              <stop offset="50%" stopColor="rgba(18, 35, 54, 0.45)" />
              <stop offset="100%" stopColor="rgba(6, 11, 18, 0.95)" />
            </radialGradient>
            <filter id="phasor-glow" x="-45%" y="-45%" width="190%" height="190%">
              <feDropShadow dx="0" dy="0" stdDeviation="1.6" floodColor="#7ed6ff" floodOpacity="0.35" />
            </filter>
            <marker
              id="arrow-cyan"
              markerWidth="11"
              markerHeight="9"
              refX="9.5"
              refY="4"
              orient="auto-start-reverse"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L10,4.4 L0,8.8 L2,4.4 Z" fill="#4de0ff" />
            </marker>
            <marker
              id="arrow-mint"
              markerWidth="11"
              markerHeight="9"
              refX="9.5"
              refY="4"
              orient="auto-start-reverse"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L10,4.4 L0,8.8 L2,4.4 Z" fill="#7dff93" />
            </marker>
            <marker
              id="arrow-amber"
              markerWidth="11"
              markerHeight="9"
              refX="9.5"
              refY="4"
              orient="auto-start-reverse"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L10,4.4 L0,8.8 L2,4.4 Z" fill="#ffd166" />
            </marker>
            <marker
              id="arrow-coral"
              markerWidth="11"
              markerHeight="9"
              refX="9.5"
              refY="4"
              orient="auto-start-reverse"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L10,4.4 L0,8.8 L2,4.4 Z" fill="#ff7b7b" />
            </marker>
          </defs>

          <circle cx={center} cy={center} r={radius + 18} className="phasor-aura" />
          <circle cx={center} cy={center} r={radius + 10} className="phasor-shell" />
          <circle cx={center} cy={center} r={radius} fill="url(#phasor-core)" className="phasor-core" />

          <circle cx={center} cy={center} r={radius} className="phasor-ring major" />
          <circle cx={center} cy={center} r={radius * 0.75} className="phasor-ring faint" />
          <circle cx={center} cy={center} r={radius * 0.5} className="phasor-ring faint" />
          <circle cx={center} cy={center} r={radius * 0.25} className="phasor-ring faint" />

          {Array.from({ length: 24 }).map((_, idx) => {
            const a = (idx * Math.PI) / 12
            const isMajor = idx % 3 === 0
            const r1 = radius + (isMajor ? 3 : 1)
            const r2 = radius - (isMajor ? 10 : 5)
            return (
              <line
                key={`tick-${idx}`}
                x1={center + Math.cos(a) * r1}
                y1={center - Math.sin(a) * r1}
                x2={center + Math.cos(a) * r2}
                y2={center - Math.sin(a) * r2}
                className={`phasor-tick ${isMajor ? 'major' : ''}`}
              />
            )
          })}

          {Array.from({ length: 12 }).map((_, idx) => {
            const a = (idx * Math.PI) / 6
            return (
              <line
                key={`grid-${idx}`}
                x1={center}
                y1={center}
                x2={center + Math.cos(a) * radius}
                y2={center - Math.sin(a) * radius}
                className="phasor-spoke"
              />
            )
          })}

          <line x1={center - radius} y1={center} x2={center + radius} y2={center} className="phasor-axis" />
          <line x1={center} y1={center - radius} x2={center} y2={center + radius} className="phasor-axis" />

          {compassMarks.map((mark) => (
            <text key={mark.label} x={mark.x} y={mark.y} className="phasor-mark">
              {mark.label}
            </text>
          ))}

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
                  strokeWidth="2.6"
                  strokeLinecap="round"
                  markerEnd={markerMap[entry.color]}
                  filter="url(#phasor-glow)"
                />
                <circle cx={center + xy.x} cy={center + xy.y} r="4" className="phasor-tip-back" />
                <circle cx={center + xy.x} cy={center + xy.y} r="2.8" fill={entry.color} />
                <text x={center + xy.x + 7} y={center + xy.y - 6} className="phasor-vector-label" fill={entry.color}>
                  {entry.label}
                </text>
              </g>
            )
          })}
          <circle cx={center} cy={center} r="5.2" className="phasor-origin-halo" />
          <circle cx={center} cy={center} r="2.6" className="phasor-origin" />
        </svg>
      </div>

      <div className="phasor-legend">
        <div className="phasor-device-caption">Device: {pmuName ?? 'No PMU selected'}</div>
        {vectors.map((entry) => {
          const magPercent = entry.value ? Math.max(0, Math.min(100, (entry.value.magnitude / 120) * 100)) : 0
          return (
            <div key={entry.label} className="phasor-item">
              <span style={{ background: entry.color, color: entry.color }} />
              <div className="phasor-copy">
                <strong>{entry.label}</strong>
                <p>
                  {entry.value
                    ? `${round(entry.value.magnitude, 1)} pu · ${round(entry.value.angleDeg, 1)}°`
                    : 'No live value'}
                </p>
              </div>
              <div className="phasor-meter">
                <i style={{ width: `${magPercent}%`, background: entry.color }} />
              </div>
            </div>
          )
        })}
      </div>

      <p className="phasor-footnote">Updated: {snapshot ? formatTS(snapshot.ts) : '--'} · Polar reference in degrees</p>
    </div>
  )
}

function App() {
  const [dashboard, setDashboard] = useState<DashboardState>(emptyState)
  const [eventFeed, setEventFeed] = useState<ConversationEvent[]>([])
  const [streamOnline, setStreamOnline] = useState(false)
  const [registry, setRegistry] = useState<PMURecord[]>(() => safeParseRegistry(localStorage.getItem(registryStorageKey)))
  const [selectedRegion, setSelectedRegion] = useState<string>('ALL')
  const [selectedPMU, setSelectedPMU] = useState<string>('')
  const [selectedPMUs, setSelectedPMUs] = useState<string[]>([])
  const [selectedPhasorPMU, setSelectedPhasorPMU] = useState<string>('')
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
    if (!filteredRegistry.length) {
      setSelectedPMU('')
      setSelectedPMUs([])
      setSelectedPhasorPMU('')
      return
    }

    const visibleNames = new Set(filteredRegistry.map((entry) => entry.record.displayName))
    const fallbackName = filteredRegistry[0].record.displayName

    if (!visibleNames.has(selectedPMU)) {
      setSelectedPMU(fallbackName)
    }

    if (!visibleNames.has(selectedPhasorPMU)) {
      const nextPhasor = visibleNames.has(selectedPMU) ? selectedPMU : fallbackName
      setSelectedPhasorPMU(nextPhasor)
    }

    setSelectedPMUs((current) => {
      const visible = current.filter((name) => visibleNames.has(name))
      if (visible.length > 0) return visible
      return [visibleNames.has(selectedPMU) ? selectedPMU : fallbackName]
    })
  }, [filteredRegistry, selectedPMU, selectedPhasorPMU])

  const selected = useMemo(() => {
    return (
      filteredRegistry.find((entry) => entry.record.displayName === selectedPMU) ?? filteredRegistry[0] ?? null
    )
  }, [filteredRegistry, selectedPMU])

  const selectedPhasor = useMemo(() => {
    return filteredRegistry.find((entry) => entry.record.displayName === selectedPhasorPMU) ?? selected ?? null
  }, [filteredRegistry, selected, selectedPhasorPMU])

  const plotSelections = useMemo(() => {
    const palette = ['#4de0ff', '#7dff93', '#ffd166', '#ff7b7b', '#8dd3ff', '#f78fb3', '#63e6be', '#ffa94d']
    return filteredRegistry
      .filter((entry) => selectedPMUs.includes(entry.record.displayName))
      .map((entry, idx) => ({
        ...entry,
        plotKey: `pmu_${idx + 1}`,
        color: palette[idx % palette.length],
      }))
  }, [filteredRegistry, selectedPMUs])

  const mergedTrend = useMemo(() => {
    const maxPoints = 240
    const rows = new Map<number, Record<string, number>>()
    for (const entry of plotSelections) {
      const trendSlice = entry.trends.length > maxPoints ? entry.trends.slice(-maxPoints) : entry.trends
      for (const point of trendSlice) {
        const row = rows.get(point.ts) ?? { ts: point.ts }
        row[`${entry.plotKey}__frequency`] = point.frequency
        row[`${entry.plotKey}__mw`] = point.mw
        row[`${entry.plotKey}__mvar`] = point.mvar
        row[`${entry.plotKey}__rocof`] = point.rocof
        rows.set(point.ts, row)
      }
    }
    return Array.from(rows.values()).sort((a, b) => a.ts - b.ts)
  }, [plotSelections])

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

  const detailRows = [
    ['PMU ID', selected?.record.id ?? '--'],
    ['Substation', selected?.record.substation ?? '--'],
    ['Region (RLDC)', selected?.record.region ?? '--'],
    ['Voltage class', selected?.record.voltageClass ?? '--'],
    ['Vendor / Model', selected?.record.vendorModel ?? '--'],
    ['Primary IP', selected?.record.primaryIp ?? '--'],
    ['Redundant IP', selected?.record.redundantIp ?? '--'],
    ['Reporting rate', selected?.record.reportingRate ?? '--'],
    ['Commissioned', selected?.record.commissioned ?? '--'],
    ['Status', selected?.record.status ?? '--'],
  ]

  const detailMetrics = [
    { label: 'Data Availability', value: selected?.record.dataAvailability ?? '--', tone: 'ok' },
    { label: 'Latency', value: selected?.record.latency ?? '--', tone: 'info' },
    { label: 'Jitter', value: selected?.record.jitter ?? '--', tone: 'neutral' },
    { label: 'Packet Loss', value: selected?.record.packetLoss ?? '--', tone: 'warn' },
  ]

  const signalChannels = (selected?.record.signalChannels ?? '--')
    .split('|')
    .map((part) => part.trim())
    .filter(Boolean)

  const packetLossValue = parseNumeric(selected?.record.packetLoss ?? '')
  const availabilityValue = parseNumeric(selected?.record.dataAvailability ?? '')

  const recommendedAction = useMemo(() => {
    if (!selected) {
      return {
        title: 'No PMU selected',
        message: 'Pick a PMU from the table to view diagnostics and operator recommendations.',
      }
    }

    if ((availabilityValue !== null && availabilityValue < 95) || (packetLossValue !== null && packetLossValue > 3)) {
      return {
        title: 'Data quality degraded',
        message: 'Inspect time sync (PTP/GPS), verify network path health, and review station link errors before raising escalation.',
      }
    }

    if (statusTone(selected.record.status) === 'warn') {
      return {
        title: 'Monitor performance drift',
        message: 'Track latency and jitter trend for 15 minutes and run diagnostics if variance continues to rise.',
      }
    }

    return {
      title: 'Operating normally',
      message: 'Keep stream under watch and export baseline profile after stable operation period.',
    }
  }, [availabilityValue, packetLossValue, selected])

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

  function setPrimaryPMU(name: string) {
    setSelectedPMU(name)
    setSelectedPMUs((current) => (current.includes(name) ? current : [...current, name]))
    if (!selectedPhasorPMU) {
      setSelectedPhasorPMU(name)
    }
  }

  function togglePMUForPlot(name: string) {
    setSelectedPMUs((current) => {
      if (current.includes(name)) {
        if (current.length === 1) return current
        return current.filter((item) => item !== name)
      }
      return [...current, name]
    })
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
                    <p>Click a row for details and use Plot to compare multiple PMUs.</p>
                  </div>
                  <span className="table-caption">{filteredRegistry.length} PMUs</span>
                </div>

                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>PMU</th>
                        <th>Region</th>
                        <th>Plot</th>
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
                            onClick={() => setPrimaryPMU(entry.record.displayName)}
                          >
                            <td>
                              <strong>{entry.record.displayName}</strong>
                              <span>{entry.record.substation}</span>
                            </td>
                            <td>{entry.record.region}</td>
                            <td>
                              <label className="plot-toggle" onClick={(evt) => evt.stopPropagation()}>
                                <input
                                  type="checkbox"
                                  checked={selectedPMUs.includes(entry.record.displayName)}
                                  onChange={() => togglePMUForPlot(entry.record.displayName)}
                                />
                                <span>Plot</span>
                              </label>
                            </td>
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
                    <p>Live PMU profile and diagnostics</p>
                  </div>
                  <span className={`status-chip ${selected?.connected ? 'ok' : 'bad'}`}>
                    {selected?.connected ? 'Live' : 'Offline'}
                  </span>
                </div>

                <div className="detail-profile-grid">
                  {detailRows.map(([label, value]) => (
                    <div key={label} className="detail-profile-row">
                      <span>{label}</span>
                      <strong>{value}</strong>
                    </div>
                  ))}
                </div>

                <div className="detail-metrics-grid">
                  {detailMetrics.map((metric) => (
                    <article key={metric.label} className={`detail-metric ${metric.tone}`}>
                      <p>{metric.label}</p>
                      <strong>{metric.value}</strong>
                    </article>
                  ))}
                </div>

                <h4 className="detail-section-title">Signal Channels</h4>
                <div className="detail-channel-list">
                  {signalChannels.length ? (
                    signalChannels.map((channel) => <p key={channel}>{channel}</p>)
                  ) : (
                    <p>No signal channels provided</p>
                  )}
                </div>

                <h4 className="detail-section-title">Recommended Actions</h4>
                <article className="detail-action-card">
                  <div className="detail-action-icon">
                    <AlertTriangle size={18} />
                  </div>
                  <div>
                    <strong>{recommendedAction.title}</strong>
                    <p>{recommendedAction.message}</p>
                  </div>
                </article>

                <div className="detail-action-buttons">
                  <button type="button" className="detail-btn ghost">Export C37.118</button>
                  <button type="button" className="detail-btn ghost">Open Event Replay</button>
                  <button type="button" className="detail-btn primary">Run Diagnostics</button>
                </div>
              </div>
            </div>
          </section>

          <section className="charts-grid">
            <article className="panel plot-select-panel">
              <div className="panel-head">
                <div>
                  <h3>Plot Selection</h3>
                  <p>Overlay trends from multiple PMUs to compare behavior in the same timeline.</p>
                </div>
                <span className="table-caption">{plotSelections.length} selected</span>
              </div>
              <div className="plot-selection-row">
                <button
                  type="button"
                  className="plot-action"
                  onClick={() => setSelectedPMUs(filteredRegistry.map((entry) => entry.record.displayName))}
                >
                  Select all visible
                </button>
                <button
                  type="button"
                  className="plot-action"
                  onClick={() => {
                    const keep = selected?.record.displayName ?? filteredRegistry[0]?.record.displayName
                    setSelectedPMUs(keep ? [keep] : [])
                  }}
                >
                  Reset to primary
                </button>
              </div>
              <div className="plot-chip-grid">
                {filteredRegistry.map((entry) => {
                  const active = selectedPMUs.includes(entry.record.displayName)
                  return (
                    <button
                      key={`${entry.record.id}-${entry.record.displayName}-chip`}
                      type="button"
                      className={`plot-chip ${active ? 'active' : ''}`}
                      onClick={() => togglePMUForPlot(entry.record.displayName)}
                    >
                      <span className={`dot ${entry.connected ? 'live' : 'off'}`} />
                      {entry.record.displayName}
                    </button>
                  )
                })}
              </div>
            </article>

            {chartSeries.map((series) => (
              <article key={series.key} className="panel chart-panel">
                <div className="panel-head">
                  <div>
                    <h3>{series.label} Comparison</h3>
                    <p>Last {mergedTrend.length} timeline points across selected PMUs</p>
                  </div>
                </div>
                <div className="chart-wrap small">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={mergedTrend}>
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
                      {plotSelections.map((entry) => (
                        <Line
                          key={`${entry.plotKey}-${series.key}`}
                          type="monotone"
                          dataKey={`${entry.plotKey}__${series.key}`}
                          name={entry.record.displayName}
                          stroke={entry.color}
                          strokeWidth={2}
                          dot={false}
                          connectNulls
                          isAnimationActive={false}
                        />
                      ))}
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              </article>
            ))}
          </section>

          <aside className="panel phasor-panel phasor-wide">
            <div className="panel-head">
              <div>
                <h3>Phasor Diagram</h3>
                <p>Choose a PMU and view its live phasor vectors in a dedicated polar scope.</p>
              </div>
            </div>

            <div className="phasor-device-strip" role="tablist" aria-label="Phasor device selection">
              {filteredRegistry.map((entry) => {
                const active = entry.record.displayName === selectedPhasor?.record.displayName
                return (
                  <button
                    key={`${entry.record.id}-${entry.record.displayName}-phasor`}
                    type="button"
                    className={`phasor-device-btn ${active ? 'active' : ''}`}
                    onClick={() => setSelectedPhasorPMU(entry.record.displayName)}
                  >
                    <span className={`dot ${entry.connected ? 'live' : 'off'}`} />
                    {entry.record.displayName}
                  </button>
                )
              })}
            </div>

            <PhasorPlot
              snapshot={selectedPhasor?.lastPhasor}
              pmuName={selectedPhasor?.record.displayName}
            />
          </aside>
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