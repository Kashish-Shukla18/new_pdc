import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { defaultNewPMU, emptyDashboardState, HELP_SECTIONS } from '../constants'
import type { AngleHistoryPoint } from '../types/analytics'
import {
  computeAnalyticsKpis,
  computeAnalyticsRecs,
  computeAnglePairs,
  topAnglePairsForChart,
} from '../utils/analytics'
import {
  computeConnectivityKpis,
  computeConnectivityRecs,
  computeConnectivityRows,
} from '../utils/connectivity'
import { useRttHistory } from './useRttHistory'
import { useFrameTrendHistory } from './useFrameTrendHistory'
import type {
  ConversationEvent,
  DashboardState,
  EditPMUForm,
  LiveAlert,
  NavItem,
  NewPMUForm,
  PMUConfig,
  PMUWithMeta,
  TabId,
  UiTiming,
} from '../types/dashboard'
import { ageText, round } from '../utils/format'
import {
  availabilityOf,
  buildOfflinePMU,
  metaForDB,
  packetLossOf,
  pmuKey,
  toneFromStatus,
} from '../utils/pmu'
import { displayPhasors } from '../utils/phasorLabels'


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
  const [editingPMUName, setEditingPMUName] = useState('')
  const [editPMU, setEditPMU] = useState<EditPMUForm | null>(null)
  const [updateSaving, setUpdateSaving] = useState(false)
  const [updateError, setUpdateError] = useState('')
  const [uiTiming, setUiTiming] = useState<UiTiming>({ fetchMs: 0, jsonMs: 0, totalMs: 0 })

  const openEditPMU = useCallback((name: string) => {
    const cfg = dbConfigs.find((item) => item.name === name)
    if (!cfg) return
    setEditPMU({
      name: cfg.name,
      ip: cfg.ip,
      port: cfg.port,
      tcp_port: cfg.tcp_port ?? 0,
      idcode: cfg.idcode,
      region: cfg.region,
      protocol: cfg.protocol || 'tcp',
      timeout_sec: cfg.timeout_sec ?? 0,
      reconnect_sec: cfg.reconnect_sec ?? 0,
      lat: cfg.lat,
      lon: cfg.lon,
    })
    setUpdateError('')
    setDrawerPMUName('')
    setEditingPMUName(name)
  }, [dbConfigs])

  const closeEditPMU = useCallback(() => {
    setEditingPMUName('')
    setEditPMU(null)
    setUpdateError('')
  }, [])

  const handleUpdatePMU = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editPMU) return
    setUpdateSaving(true)
    setUpdateError('')
    try {
      const res = await fetch('/api/pmus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editPMU),
      })
      if (!res.ok) {
        const text = await res.text()
        setUpdateError(text || 'Failed to update device')
        return
      }
      closeEditPMU()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      setUpdateError(message)
      console.error(err)
    } finally {
      setUpdateSaving(false)
    }
  }, [closeEditPMU, editPMU])

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
    if (activeTab !== 'devices' && editingPMUName) {
      closeEditPMU()
    }
  }, [activeTab, closeEditPMU, editingPMUName])

  useEffect(() => {
    if (isPaused) return

    const refreshState = async () => {
      const t0 = performance.now()
      try {
        const stateRes = await fetch('/conversation/state')
        const tNet = performance.now()
        if (stateRes.ok) {
          const data = (await stateRes.json()) as DashboardState
          const tJson = performance.now()
          setDashboard(data)
          setUiTiming({
            fetchMs: tNet - t0,
            jsonMs: tJson - tNet,
            totalMs: tJson - t0,
          })
        }
      } catch {
        // Keep last good snapshot visible.
      }
    }

    const refreshConfigs = async () => {
      try {
        const configRes = await fetch('/api/pmus')
        if (configRes.ok) {
          const configs = (await configRes.json()) as PMUConfig[]
          setDbConfigs(configs)
        }
      } catch {
        // Keep last configs.
      }
    }

    void refreshState()
    void refreshConfigs()
    const stateTimer = window.setInterval(refreshState, 1000)
    const configTimer = window.setInterval(refreshConfigs, 10000)
    return () => {
      window.clearInterval(stateTimer)
      window.clearInterval(configTimer)
    }
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
  const editingPMU = pmus.find((pmu) => pmu.name === editingPMUName)

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

  // Fleet charts merge only what Overview selects; keep a light default for other consumers.
  const mergedTrend = useMemo(() => {
    const rows = new Map<number, Record<string, number>>()
    const chartPMUs = pmus.slice(0, 8)
    for (const pmu of chartPMUs) {
      const key = pmuKey(pmu.name)
      const trend = pmu.trends.slice(-90)
      const fnom = pmu.fnomHz && pmu.fnomHz > 0 ? pmu.fnomHz : 60
      for (const point of trend) {
        const row = rows.get(point.ts) ?? { ts: point.ts }
        const freqDev =
          typeof point.frequencyDev === 'number'
            ? point.frequencyDev
            : point.frequency - fnom
        row[`${key}__frequency`] = point.frequency
        row[`${key}__frequencyDev`] = freqDev
        row[`${key}__mw`] = point.mw
        row[`${key}__mvar`] = point.mvar
        row[`${key}__rocof`] = point.rocof
        row.fnom = fnom
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

  const frameLineKeyRef = useRef('')

  useEffect(() => {
    if (!selectedFramePMU || isPaused) return
    const frame = selectedFramePMU.lastFrame
    if (!frame || !frame.soc) return

    const key = `${selectedFramePMU.name}:${frame.soc}:${frame.fracSecCount}:${selectedFramePMU.totalFrames}`
    if (key === frameLineKeyRef.current) return
    frameLineKeyRef.current = key

    const fracHex = `0x${(frame.fracSecRaw >>> 0).toString(16).toUpperCase().padStart(8, '0')}`
    const statHex = `0x${(frame.stat >>> 0).toString(16).toUpperCase().padStart(4, '0')}`
    const digHex = `0x${((frame.digital ?? 0) >>> 0).toString(16).toUpperCase().padStart(4, '0')}`
    const line =
      `SOC ${frame.soc}  FRACSEC ${fracHex} (${frame.fracSecCount} µs)  ` +
      `F ${(selectedFramePMU.lastReading?.frequency ?? 0).toFixed(4)}  ` +
      `ROCOF ${(selectedFramePMU.lastReading?.rocof ?? 0).toFixed(4)}  ` +
      `STAT ${statHex}  DIG ${digHex}`

    setFrameLines((current) => [line, ...current].slice(0, 45))
  }, [isPaused, selectedFramePMU])

  useEffect(() => {
    frameLineKeyRef.current = ''
    setFrameLines([])
  }, [selectedFramePMUName])

  const connectivityRows = useMemo(() => computeConnectivityRows(pmus), [pmus])
  const connectivityKpis = useMemo(() => computeConnectivityKpis(connectivityRows), [connectivityRows])
  const connectivityRecs = useMemo(() => computeConnectivityRecs(connectivityRows), [connectivityRows])
  const { rttHistory, rttStreams } = useRttHistory(connectivityRows, dashboard.nowUtc, isPaused)
  const frameTrendHistory = useFrameTrendHistory(
    selectedFramePMU,
    selectedFramePMUName,
    dashboard.nowUtc,
    isPaused,
  )

  const anglePairs = useMemo(() => computeAnglePairs(pmus), [pmus])
  const chartAnglePairs = useMemo(() => topAnglePairsForChart(pmus), [pmus])

  const analyticsRecs = useMemo(
    () => computeAnalyticsRecs(pmus, anglePairs, connectivityRows, events),
    [pmus, anglePairs, connectivityRows, events],
  )

  const analyticsKpis = useMemo(
    () => computeAnalyticsKpis(pmus, anglePairs, analyticsRecs),
    [pmus, anglePairs, analyticsRecs],
  )

  const angleHistoryRef = useRef<AngleHistoryPoint[]>([])
  const [angleHistory, setAngleHistory] = useState<AngleHistoryPoint[]>([])

  useEffect(() => {
    if (!chartAnglePairs.length || isPaused) return

    const point: AngleHistoryPoint = {
      ts: Date.now(),
      label: new Date().toLocaleTimeString(),
    }
    for (const pair of chartAnglePairs) {
      point[pair.key] = pair.value
    }

    const next = [...angleHistoryRef.current, point].slice(-60)
    angleHistoryRef.current = next
    setAngleHistory(next)
  }, [chartAnglePairs, isPaused, dashboard.nowUtc])

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

  const phasorItems = useMemo(() => {
    const channels = selectedFramePMU?.lastChannels?.phasors
    if (channels?.length) {
      return displayPhasors(channels).map((p) => ({
        label: p.label,
        cfgName: p.cfgName,
        value: { magnitude: p.magnitude, angleDeg: p.angleDeg },
      }))
    }
    return [
      { label: 'VA', cfgName: 'VA', value: selectedFramePMU?.lastPhasor?.va },
      { label: 'VB', cfgName: 'VB', value: selectedFramePMU?.lastPhasor?.vb },
      { label: 'VC', cfgName: 'VC', value: selectedFramePMU?.lastPhasor?.vc },
      { label: 'IA', cfgName: 'IA', value: selectedFramePMU?.lastPhasor?.ia },
    ]
  }, [selectedFramePMU])

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
    editingPMUName,
    editingPMU,
    editPMU,
    setEditPMU,
    openEditPMU,
    closeEditPMU,
    handleUpdatePMU,
    updateSaving,
    updateError,
    regionSummary,
    mergedTrend,
    systemCounts,
    frameRate,
    systemTone,
    systemMessage,
    liveAlerts,
    filteredDevices,
    connectivityRows,
    connectivityKpis,
    connectivityRecs,
    rttHistory,
    rttStreams,
    frameTrendHistory,
    anglePairs,
    chartAnglePairs,
    angleHistory,
    analyticsKpis,
    analyticsRecs,
    filteredHelp,
    navItems,
    phasorItems,
    uiTiming,
  }
}

export type DashboardContextValue = ReturnType<typeof useDashboard>
