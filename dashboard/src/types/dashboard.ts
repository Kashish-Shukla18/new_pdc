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
  mw: number
  mvar: number
  rocof: number
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
  trends: TrendPoint[]
}

export type DashboardState = {
  nowUtc: string
  pmus: LivePMUState[]
  eventCount: number
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
  idcode: number
  region: string
  protocol: string
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

export type NewPMUForm = {
  name: string
  ip: string
  port: number
  idcode: number
  region: string
  protocol: string
  lat: number
  lon: number
}

export type NavItem = {
  id: TabId
  label: string
  section: 'Monitoring' | 'Resources'
  badge?: string
  bad?: boolean
}
