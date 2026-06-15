import { useCallback, useEffect, useMemo, useState } from 'react'
import { defaultNewPMU, emptyDashboardState, HELP_SECTIONS } from '../constants'
import type {
  ConnectivityRow,
  ConversationEvent,
  DashboardState,
  LiveAlert,
  NavItem,
  NewPMUForm,
  PMUConfig,
  PMUWithMeta,
  TabId,
} from '../types/dashboard'
import { ageText, round } from '../utils/format'
import {
  availabilityOf,
  buildOfflinePMU,
  jitterOf,
  latencyOf,
  metaForDB,
  packetLossOf,
  pmuKey,
  toneFromStatus,
} from '../utils/pmu'

export function useDashboard() {
  const [dashboard, setDashboard] = useState<DashboardState>(emptyDashboardState)
  const [dbConfigs, setDbConfigs] = useState<PMUConfig[]>([])
  const [events, setEvents] = useState<ConversationEvent[]>([])
  const [streamOnline, setStreamOnline] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
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
  const [newPMU, setNewPMU] = useState<NewPMUForm>(defaultNewPMU)

  const handleAddPMU = useCallback(async (e: React.FormEvent) => {
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
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      alert('Error connecting PMU: ' + message)
      console.error(err)
    }
  }, [newPMU])

  const handleDeletePMU = useCallback(async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm(`Disconnect and delete PMU ${name}?`)) return
    try {
      const res = await fetch(`/api/pmus/${name}`, { method: 'DELETE' })
      if (!res.ok) {
        const text = await res.text()
        alert('Failed to delete PMU: ' + text)
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      alert('Error deleting PMU: ' + message)
      console.error(err)
    }
  }, [])

  useEffect(() => {
    if (isPaused) return

    const refresh = async () => {
      try {
        const [stateRes, configRes] = await Promise.all([
          fetch('/conversation/state'),
          fetch('/api/pmus'),
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

  const pmus = useMemo<PMUWithMeta[]>(() => {
    return dbConfigs.map((cfg) => {
      const activeState = dashboard.pmus.find((p) => p.name === cfg.name)
      if (activeState) {
        return {
          ...activeState,
          meta: metaForDB(cfg.name, cfg),
        }
      }
      return buildOfflinePMU(cfg.name, metaForDB(cfg.name, cfg))
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

  const liveAlerts = useMemo<LiveAlert[]>(() => {
    const fromEvents = events.slice(0, 18).map((evt) => {
      const lowered = `${evt.status} ${evt.message}`.toLowerCase()
      const sev = lowered.includes('error')
        ? 'bad'
        : lowered.includes('warn') || lowered.includes('reject')
          ? 'warn'
          : 'info'
      return {
        sev: sev as LiveAlert['sev'],
        title: evt.stage,
        msg: `${evt.pmu} - ${evt.message}`,
        time: new Date(evt.time).toLocaleTimeString(),
      }
    })

    const fromState = pmus
      .filter((pmu) => !pmu.connected || packetLossOf(pmu) > 1)
      .map((pmu) => ({
        sev: (pmu.connected ? 'warn' : 'bad') as LiveAlert['sev'],
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

  const connectivityRows = useMemo<ConnectivityRow[]>(() => {
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

  const filteredHelp = HELP_SECTIONS.filter((item) => {
    if (!helpQuery.trim()) return true
    return `${item.title} ${item.content}`.toLowerCase().includes(helpQuery.toLowerCase())
  })

  const navItems = useMemo<NavItem[]>(() => [
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
  ], [pmus.length, connectivityRows])

  const phasorItems = useMemo(() => [
    { label: 'VA', value: selectedFramePMU?.lastPhasor?.va },
    { label: 'VB', value: selectedFramePMU?.lastPhasor?.vb },
    { label: 'VC', value: selectedFramePMU?.lastPhasor?.vc },
    { label: 'IA', value: selectedFramePMU?.lastPhasor?.ia },
  ], [selectedFramePMU])

  return {
    dashboard,
    pmus,
    streamOnline,
    isPaused,
    setIsPaused,
    activeTab,
    setActiveTab,
    selectedFramePMU,
    selectedFramePMUName,
    setSelectedFramePMUName,
    drawerPMU,
    drawerPMUName,
    setDrawerPMUName,
    deviceSearch,
    setDeviceSearch,
    regionFilter,
    setRegionFilter,
    statusFilter,
    setStatusFilter,
    mapFilter,
    setMapFilter,
    helpQuery,
    setHelpQuery,
    frameLines,
    openFaq,
    setOpenFaq,
    showAddPMU,
    setShowAddPMU,
    newPMU,
    setNewPMU,
    handleAddPMU,
    handleDeletePMU,
    regionSummary,
    mergedTrend,
    systemCounts,
    frameRate,
    systemTone,
    systemMessage,
    liveAlerts,
    filteredDevices,
    connectivityRows,
    anglePairs,
    analyticsRecs,
    filteredHelp,
    navItems,
    phasorItems,
  }
}

export type DashboardContextValue = ReturnType<typeof useDashboard>
