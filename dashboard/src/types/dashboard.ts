export type TabId =
  | 'overview'
  | 'devices'
  | 'dataframes'
  | 'connectivity'
  | 'analytics'
  | 'help'
  | 'docs'

export type TrendPoint = {
  ts: number
  frequency: number
  frequencyDev?: number
  mw: number
  mvar: number
  rocof: number
  statDataError?: boolean
  va?: number
  vb?: number
  vc?: number
  ia?: number
  ib?: number
  ic?: number
}

export type PhasorVector = {
  magnitude: number
  angleDeg: number
}

export type PhasorSnapshot = {
  va: PhasorVector
  vb: PhasorVector
  vc: PhasorVector
  ia: PhasorVector
  ts: number
}

export type FrameStamp = {
  soc: number
  fracSecRaw: number
  fracSecCount: number
  timeQuality: number
  msgTq?: {
    raw: number
    time_quality_code?: number
    timeQualityCode?: number
  }
  stat: number
  statDetail?: {
    data_error?: boolean
    dataError?: boolean
    data_error_code?: number
    dataErrorCode?: number
    cfg_change?: boolean
    cfgChange?: boolean
    trigger_detected?: boolean
    triggerDetected?: boolean
    pmu_sync_status?: boolean
    pmuSyncStatus?: boolean
    pmu_time_quality?: number
    pmuTimeQuality?: number
    unlocked_duration?: number
    unlockedDuration?: number
  }
  idCode: number
  syncWord: number
  digital?: number
  digitals?: number[]
}

export type NamedPhasorView = {
  name: string
  magnitude: number
  angleDeg: number
}

export type NamedAnalogView = {
  name: string
  value: number
}

export type DigitalBitView = {
  name: string
  bit: number
  set: boolean
}

export type ChannelSnapshot = {
  phasors: NamedPhasorView[]
  analogs: NamedAnalogView[]
  digitalBits: DigitalBitView[]
  ts: number
}

export type CFGSummary = {
  available: boolean
  syncWord: number
  idCode: number
  station: string
  fnomHz: number
  dataRate: number
  format: number
  polar: boolean
  phFloat: boolean
  anFloat: boolean
  freqFloat: boolean
  phasors: string[]
  analogs: string[]
  digitalWords: number
  cfgCnt: number
  headerText?: string
}

export type LivePMUState = {
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
  lastChannels?: ChannelSnapshot
  lastFrame?: FrameStamp
  cfg?: CFGSummary
  trends: TrendPoint[]
  fnomHz?: number
  statDataError?: boolean
  lastHops?: Record<string, number>
}

export type LatencyStage = {
  id: string
  label: string
  group: 'connection' | 'ingest' | 'process' | 'dashboard' | 'sink' | 'e2e' | string
  lastMs: number
  avgMs: number
  p95Ms: number
  maxMs: number
  count: number
}

export type PipelineLatency = {
  slowestStage: string
  slowestLabel: string
  slowestAvgMs: number
  stages: LatencyStage[]
}

export type UiTiming = {
  fetchMs: number
  jsonMs: number
  totalMs: number
}

export type DashboardState = {
  nowUtc: string
  pmus: LivePMUState[]
  eventCount: number
  latency?: PipelineLatency
}

export type ConversationEvent = {
  time: string
  pmu: string
  stage: string
  status: string
  message: string
}

export type PMUMeta = {
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

export type PMUConfig = {
  name: string
  ip: string
  port: number
  /** For UDP unicast: TCP control port (Connection Tester "Local TCP Port"). */
  tcp_port?: number
  idcode: number
  region: string
  protocol: string
  timeout_sec?: number
  reconnect_sec?: number
  data_rate?: number
  lat: number
  lon: number
}

export type PMUWithMeta = LivePMUState & { meta: PMUMeta }

export type ConnectivityRow = PMUWithMeta & {
  loss: number
  latency: number
  jitter: number
  avail: number
  recommendation: string
  tone: 'ok' | 'warn' | 'bad'
  statusLabel: 'Healthy' | 'Degraded' | 'Offline'
  link: 'Primary' | 'Down'
  tableRecommendation: string
}

export type LiveAlert = {
  sev: 'bad' | 'warn' | 'info'
  title: string
  msg: string
  time: string
}

export type NewPMUForm = PMUConfig

export type EditPMUForm = PMUConfig

export type NavItem = {
  id: TabId
  label: string
  section: 'Monitoring' | 'Resources'
  badge?: string
  bad?: boolean
}
