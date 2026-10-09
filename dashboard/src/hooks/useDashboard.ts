import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { defaultNewPMU, emptyDashboardState, HELP_SECTIONS } from '../constants'
import type { AngleHistoryPoint } from '../types/analytics'
import {
  computeAnalyticsKpis,
  computeAnalyticsRecs,
  computeAnglePairs,
} from '../utils/analytics'
import {
  computeConnectivityKpis,
  computeConnectivityRecs,
  computeConnectivityRows,
} from '../utils/connectivity'
import { useArrivalSpreadHistory } from './useArrivalSpreadHistory'
import { useCycleLatencyHistory } from './useCycleLatencyHistory'
import { useRttHistory } from './useRttHistory'
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
  endpointIdentity,
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
  const [deviceInventoryTab, setDeviceInventoryTab] = useState<'connected' | 'disconnected'>('connected')
  const [mapFilter, setMapFilter] = useState<'all' | 'issues'>('all')
  const [helpQuery, setHelpQuery] = useState('')
  const [frameLines, setFrameLines] = useState<string[]>([])
  const [openFaq, setOpenFaq] = useState<number | null>(null)
  const [showAddPMU, setShowAddPMU] = useState(false)
  const [newPMU, setNewPMU] = useState<NewPMUForm>(defaultNewPMU)
  const [editingPMUName, setEditingPMUName] = useState('')
  const [editPMU, setEditPMU] = useState<EditPMUForm | null>(null)
  const [addSaving, setAddSaving] = useState(false)
  const [addError, setAddError] = useState('')
  const [updateSaving, setUpdateSaving] = useState(false)
  const [updateError, setUpdateError] = useState('')
  const [deletingPMUName, setDeletingPMUName] = useState('')
  const [connectingPMUName, setConnectingPMUName] = useState('')
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; message: string } | null>(null)
  const [loadError, setLoadError] = useState('')
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
      station: cfg.station ?? '',
      timestamp_tz: cfg.timestamp_tz ?? '',
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
    if (!editPMU || !editingPMUName) return
    setUpdateSaving(true)
    setUpdateError('')
    try {
      const identity = endpointIdentity(editPMU)
      const payload = {
        ...editPMU,
        name: '',
        replace: editingPMUName !== identity ? editingPMUName : undefined,
      }
      const res = await fetch('/api/pmus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok) {
        const text = await res.text()
        setUpdateError(text || 'Failed to update device')
        return
      }
      const saved = (await res.json()) as PMUConfig
      const configRes = await fetch('/api/pmus')
      if (configRes.ok) {
        setDbConfigs((await configRes.json()) as PMUConfig[])
      } else {
        setDbConfigs((current) => {
          const withoutOld = current.filter(
            (config) => config.name !== editingPMUName && config.name !== saved.name,
          )
          return [...withoutOld, saved]
        })
      }
      setNotice({
        type: 'success',
        message: `${saved.station?.trim() || saved.name} was updated successfully.`,
      })
      closeEditPMU()
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      setUpdateError(message)
      console.error(err)
    } finally {
      setUpdateSaving(false)
    }
  }, [closeEditPMU, editPMU, editingPMUName])

  const handleAddPMU = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    setAddSaving(true)
    setAddError('')
    try {
      const payload = { ...newPMU, name: '' }
      const res = await fetch('/api/pmus', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok) {
        const text = await res.text()
        setAddError(text || 'Failed to connect PMU.')
        return
      }
      const saved = (await res.json()) as PMUConfig
      setShowAddPMU(false)
      const configRes = await fetch('/api/pmus')
      if (configRes.ok) {
        setDbConfigs((await configRes.json()) as PMUConfig[])
      } else {
        setDbConfigs((current) =>
          current.some((config) => config.name === saved.name)
            ? current.map((config) => (config.name === saved.name ? saved : config))
            : [...current, saved],
        )
      }
      setNewPMU(defaultNewPMU)
      setNotice({
        type: 'success',
        message: `${saved.name} was added successfully.`,
      })
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      setAddError(`Could not connect PMU: ${message}`)
      console.error(err)
    } finally {
      setAddSaving(false)
    }
  }, [newPMU])

  const handleDisconnectPMU = useCallback(async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm(`Disconnect ${name}? It will stay registered so you can connect again.`)) return
    setDeletingPMUName(name)
    try {
      const res = await fetch(`/api/pmus/${encodeURIComponent(name)}/disconnect`, { method: 'POST' })
      if (!res.ok) {
        const text = await res.text()
        setNotice({ type: 'error', message: text || `Failed to disconnect ${name}.` })
        return
      }
      setDbConfigs((current) =>
        current.map((config) => (config.name === name ? { ...config, active: false } : config)),
      )
      setDeviceInventoryTab('disconnected')
      setNotice({ type: 'success', message: `${name} disconnected. Open the Disconnected tab to reconnect.` })
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      setNotice({ type: 'error', message: `Could not disconnect ${name}: ${message}` })
      console.error(err)
    } finally {
      setDeletingPMUName('')
    }
  }, [])

  const handleConnectPMU = useCallback(async (name: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setConnectingPMUName(name)
    try {
      const res = await fetch(`/api/pmus/${encodeURIComponent(name)}/connect`, { method: 'POST' })
      if (!res.ok) {
        const text = await res.text()
        setNotice({ type: 'error', message: text || `Failed to connect ${name}.` })
        return
      }
      const saved = (await res.json()) as PMUConfig
      setDbConfigs((current) =>
        current.map((config) => (config.name === name ? { ...config, ...saved, active: true } : config)),
      )
      setDeviceInventoryTab('connected')
      setNotice({ type: 'success', message: `${name} is connecting again.` })
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      setNotice({ type: 'error', message: `Could not connect ${name}: ${message}` })
      console.error(err)
    } finally {
      setConnectingPMUName('')
    }
  }, [])

  const handleDeletePMU = handleDisconnectPMU

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
          setLoadError('')
        } else {
          setLoadError(`Live state request failed (${stateRes.status}).`)
        }
      } catch {
        // Keep last good snapshot visible.
        setLoadError('Live data is unavailable. Check that the PDC is running on port 2112.')
      }
    }

    const refreshConfigs = async () => {
      try {
        const configRes = await fetch('/api/pmus')
        if (configRes.ok) {
          const configs = (await configRes.json()) as PMUConfig[]
          setDbConfigs(configs)
        } else {
          setLoadError(`PMU configuration request failed (${configRes.status}).`)
        }
      } catch {
        // Keep last configs.
        setLoadError('PMU configuration is unavailable. Check the API on port 8081.')
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
          active: cfg.active !== false,
          cfg: {
            ...(activeState.cfg ?? {
              available: false, syncWord: 0, idCode: 0, station: '', fnomHz: 0, dataRate: 0,
              format: 0, polar: false, phFloat: false, anFloat: false, freqFloat: false,
              phasors: [], analogs: [], digitalWords: 0, cfgCnt: 0,
            }),
            station: activeState.cfg?.station || cfg.station || '',
          },
          meta: metaForDB(cfg.name, cfg),
        }
      }
      const offline = buildOfflinePMU(cfg.name, metaForDB(cfg.name, cfg))
      offline.active = cfg.active !== false
      if (cfg.station && offline.cfg) {
        offline.cfg = { ...offline.cfg, station: cfg.station }
      }
      return offline
    })
  }, [dashboard.pmus, dbConfigs])

  const enabledPmus = useMemo(
    () => pmus.filter((pmu) => pmu.active !== false),
    [pmus],
  )

  useEffect(() => {
    if (!enabledPmus.length) {
      if (!pmus.length) {
        setSelectedPMUName('')
        setSelectedFramePMUName('')
      }
      return
    }

    if (!enabledPmus.some((pmu) => pmu.name === selectedPMUName)) {
      setSelectedPMUName(enabledPmus[0].name)
    }
    if (!enabledPmus.some((pmu) => pmu.name === selectedFramePMUName)) {
      setSelectedFramePMUName(enabledPmus[0].name)
    }
  }, [pmus.length, enabledPmus, selectedPMUName, selectedFramePMUName])

  const selectedPMU = enabledPmus.find((pmu) => pmu.name === selectedPMUName) ?? enabledPmus[0]
  const selectedFramePMU = enabledPmus.find((pmu) => pmu.name === selectedFramePMUName) ?? enabledPmus[0]
  const drawerPMU = pmus.find((pmu) => pmu.name === drawerPMUName)
  const editingPMU = pmus.find((pmu) => pmu.name === editingPMUName)

  const regionSummary = useMemo(() => {
    const map = new Map<string, { total: number; connected: number; availability: number }>()
    for (const pmu of enabledPmus) {
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
  }, [enabledPmus])

  const systemCounts = useMemo(() => {
    const connected = enabledPmus.filter((pmu) => pmu.connected).length
    const disconnected = enabledPmus.length - connected
    const totalErrors = enabledPmus.reduce((sum, pmu) => sum + pmu.qualityRejects, 0)
    return { connected, disconnected, totalErrors }
  }, [enabledPmus])

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

    const fromState = enabledPmus
      .filter((pmu) => !pmu.connected || packetLossOf(pmu) > 1)
      .map((pmu) => ({
        sev: (pmu.connected ? 'warn' : 'bad') as LiveAlert['sev'],
        title: pmu.connected ? 'Elevated packet loss' : 'PMU offline',
        msg: `${pmu.name} - ${pmu.connected ? `${round(packetLossOf(pmu), 2)}% loss` : 'stream disconnected'}`,
        time: ageText(pmu.lastEventTime),
      }))

    return [...fromState, ...fromEvents].slice(0, 24)
  }, [events, enabledPmus])

  const filteredDevices = useMemo(() => {
    return pmus.filter((pmu) => {
      const isActive = pmu.active !== false
      if (deviceInventoryTab === 'connected' && !isActive) return false
      if (deviceInventoryTab === 'disconnected' && isActive) return false
      const matchesSearch = `${pmu.name} ${pmu.meta.substation} ${pmu.meta.vendor} ${pmu.meta.primaryIp}`
        .toLowerCase()
        .includes(deviceSearch.toLowerCase())
      const tone = toneFromStatus(pmu.connected, packetLossOf(pmu))
      const matchesStatus = statusFilter === 'ALL' ? true : statusFilter === tone.toUpperCase()
      const matchesRegion = regionFilter === 'ALL' ? true : pmu.meta.region === regionFilter
      return matchesSearch && matchesStatus && matchesRegion
    })
  }, [pmus, deviceInventoryTab, deviceSearch, statusFilter, regionFilter])

  const deviceTabCounts = useMemo(() => {
    let connected = 0
    let disconnected = 0
    for (const cfg of dbConfigs) {
      if (cfg.active === false) disconnected += 1
      else connected += 1
    }
    return { connected, disconnected }
  }, [dbConfigs])

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

  const connectivityRows = useMemo(() => computeConnectivityRows(enabledPmus), [enabledPmus])
  const connectivityKpis = useMemo(() => computeConnectivityKpis(connectivityRows), [connectivityRows])
  const connectivityRecs = useMemo(() => computeConnectivityRecs(connectivityRows), [connectivityRows])
  const { rttHistory, rttStreams } = useRttHistory(connectivityRows, dashboard.nowUtc, isPaused)
  const { cycleHistory, cycleLatest } = useCycleLatencyHistory(enabledPmus, dashboard.nowUtc, isPaused)
  const { spreadHistory, spreadLatest } = useArrivalSpreadHistory(enabledPmus, dashboard.nowUtc, isPaused)

  const onlinePMUs = useMemo(() => enabledPmus.filter((pmu) => pmu.connected), [enabledPmus])
  const anglePairs = useMemo(
    () => computeAnglePairs(onlinePMUs, onlinePMUs.length * (onlinePMUs.length - 1) / 2),
    [onlinePMUs],
  )
  const chartAnglePairs = anglePairs

  const analyticsRecs = useMemo(
    () => computeAnalyticsRecs(enabledPmus, anglePairs, connectivityRows, events),
    [enabledPmus, anglePairs, connectivityRows, events],
  )

  const analyticsKpis = useMemo(
    () => computeAnalyticsKpis(enabledPmus, anglePairs, analyticsRecs),
    [enabledPmus, anglePairs, analyticsRecs],
  )

  const angleHistory = useMemo(() => {
    const batches = dashboard.alignedBatches ?? []
    if (!chartAnglePairs.length || !batches.length) return []

    const wrap = (deg: number) => {
      const raw = Math.abs(deg)
      return raw > 180 ? 360 - raw : raw
    }

    return batches.slice(-60).map((batch) => {
      const point: AngleHistoryPoint = {
        ts: batch.ts,
        label: new Date(batch.ts).toLocaleTimeString(),
      }
      for (const pair of chartAnglePairs) {
        const [aKey, bKey] = pair.key.split('__')
        const aName = pmus.find((p) => pmuKey(p.name) === aKey)?.name
        const bName = pmus.find((p) => pmuKey(p.name) === bKey)?.name
        const a = aName ? batch.points?.[aName] : undefined
        const b = bName ? batch.points?.[bName] : undefined
        if (a && b && typeof a.vaAngle === 'number' && typeof b.vaAngle === 'number') {
          point[pair.key] = round(wrap(a.vaAngle - b.vaAngle), 2)
        }
      }
      return point
    })
  }, [dashboard.alignedBatches, chartAnglePairs, pmus])

  const filteredHelp = HELP_SECTIONS.filter((item) => {
    if (!helpQuery.trim()) return true
    return `${item.title} ${item.content}`.toLowerCase().includes(helpQuery.toLowerCase())
  })

  const navItems = useMemo<NavItem[]>(() => [
    { id: 'overview', label: 'Overview', section: 'Monitoring' },
    { id: 'devices', label: 'Device Inventory', section: 'Monitoring', badge: `${dbConfigs.length}` },
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
  ], [dbConfigs.length, connectivityRows])

  return {
    dashboard,
    pmus,
    enabledPmus,
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
    addSaving,
    addError,
    handleDeletePMU,
    handleDisconnectPMU,
    handleConnectPMU,
    deletingPMUName,
    connectingPMUName,
    deviceInventoryTab,
    setDeviceInventoryTab,
    deviceTabCounts,
    editingPMUName,
    editingPMU,
    editPMU,
    setEditPMU,
    openEditPMU,
    closeEditPMU,
    handleUpdatePMU,
    updateSaving,
    updateError,
    notice,
    setNotice,
    loadError,
    regionSummary,
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
    cycleHistory,
    cycleLatest,
    spreadHistory,
    spreadLatest,
    anglePairs,
    chartAnglePairs,
    angleHistory,
    analyticsKpis,
    analyticsRecs,
    filteredHelp,
    navItems,
    uiTiming,
  }
}

export type DashboardContextValue = ReturnType<typeof useDashboard>
