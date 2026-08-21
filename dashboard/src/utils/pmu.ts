import type { LivePMUState, PMUConfig, PMUMeta, PMUWithMeta } from '../types/dashboard'

/** Display-only labels for the field PMU — everything else comes from live config/telemetry. */
export const FIELD_PMU_VENDOR = 'ABB'
export const FIELD_PMU_REGION = 'FIELD'

export function pmuKey(name: string) {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-')
}

export function metaForDB(name: string, config?: PMUConfig): PMUMeta {
  return {
    substation: config?.name || name,
    region: config?.region || FIELD_PMU_REGION,
    state: '-',
    voltage: '-',
    vendor: FIELD_PMU_VENDOR,
    primaryIp: config?.ip || '0.0.0.0',
    redundantIp: '-',
    // 0 = use CFG-2 DATA_RATE from live state (30/60/120), not a hardcoded 30.
    targetFps: config?.data_rate && config.data_rate > 0 ? config.data_rate : 0,
    lat: config?.lat ?? 0,
    lon: config?.lon ?? 0,
  }
}

export function toneFromStatus(connected: boolean, loss: number): 'ok' | 'warn' | 'bad' {
  if (!connected) return 'bad'
  if (loss > 1) return 'warn'
  return 'ok'
}

/** Live stream FPS: prefer 1s frame counter; fall back to inter-frame wait. */
export function effectiveFps(pmu: LivePMUState): number {
  const approx = pmu.approxFps || 0
  if (approx >= 5) return approx
  const wait = hopMs(pmu, 'tcp_wait') ?? hopMs(pmu, 'tcp_interarrival')
  if (wait != null && wait > 1 && wait < 500) return 1000 / wait
  return approx
}

function snapSynchroRate(live: number): number {
  if (live >= 100) return 120
  if (live >= 45) return 60
  if (live >= 20) return 30
  if (live >= 8) return 10
  return Math.max(1, Math.round(live))
}

/**
 * Target FPS for availability.
 * If CFG advertises 120 but the wire steadily delivers ~30, use the live rate
 * (frame-diag already proved the PDC is not dropping those frames).
 */
export function resolveTargetFps(pmu: LivePMUState, metaTarget = 0): number {
  const live = effectiveFps(pmu)
  let cfg = 0
  if (typeof pmu.cfg?.dataRate === 'number' && pmu.cfg.dataRate !== 0) {
    cfg = pmu.cfg.dataRate > 0 ? pmu.cfg.dataRate : 1 / Math.abs(pmu.cfg.dataRate)
  } else if (metaTarget > 0) {
    cfg = metaTarget
  }
  if (live >= 8 && cfg > 0 && live < cfg * 0.55) {
    return snapSynchroRate(live)
  }
  if (cfg > 0) return cfg
  if (live > 1) return snapSynchroRate(live)
  return 60
}

export function availabilityOf(pmu: LivePMUState, targetFps: number) {
  if (!pmu.connected) return 0
  const target = resolveTargetFps(pmu, targetFps)
  const fps = effectiveFps(pmu)
  if (!Number.isFinite(fps) || fps <= 0) return 100
  const ratio = fps / Math.max(1, target)
  if (ratio >= 0.7) return 100
  return Math.max(0, Math.min(100, Math.round(ratio * 1000) / 10))
}

export function packetLossOf(pmu: LivePMUState) {
  if (pmu.totalFrames <= 0) return 0
  return (pmu.qualityRejects / pmu.totalFrames) * 100
}

export function latencyOf(pmu: LivePMUState) {
  const e2e = pmu.lastHops?.e2e_recv_to_dashboard
  if (typeof e2e === 'number' && Number.isFinite(e2e) && e2e >= 0) {
    return e2e
  }
  if (!pmu.connected) return 999
  // No inventing latency from FPS — missing hop → NaN (chart holds last sample).
  return Number.NaN
}

/** Latest hop sample in ms from backend lastHops. */
export function hopMs(pmu: LivePMUState, stage: string): number | undefined {
  const v = pmu.lastHops?.[stage]
  if (typeof v === 'number' && Number.isFinite(v) && v >= 0 && v < 10_000) return v
  return undefined
}

/**
 * One sample-cycle breakdown:
 * - pmuWait ≈ inter-frame gap (50 FPS → ~20 ms) — NOT PDC processing
 * - parse = DATA decode time
 * - pipeline = receive-complete → dashboard (includes parse)
 * - total = pmuWait + pipeline  (full cycle the user asked to plot)
 */
export type CycleLatency = {
  pmuWait: number
  parse: number
  pipeline: number
  pipelineRest: number
  total: number
  fpsHint: number
}

export function cycleLatencyOf(pmu: LivePMUState): CycleLatency | null {
  if (!pmu.connected) return null

  const wait =
    hopMs(pmu, 'tcp_wait') ??
    hopMs(pmu, 'tcp_interarrival') ??
    (pmu.approxFps > 1 ? 1000 / pmu.approxFps : undefined)
  const parse = hopMs(pmu, 'parse')
  const pipeline = hopMs(pmu, 'e2e_recv_to_dashboard') ?? latencyOf(pmu)

  if (wait == null || !Number.isFinite(pipeline) || pipeline >= 900) return null

  const parseMs = parse != null && Number.isFinite(parse) ? parse : 0
  const rest = Math.max(0, pipeline - parseMs)
  return {
    pmuWait: wait,
    parse: parseMs,
    pipeline,
    pipelineRest: rest,
    total: wait + pipeline,
    fpsHint: wait > 0 ? 1000 / wait : pmu.approxFps || 0,
  }
}

/** Average cycle across connected PMUs that have hop data. */
export function averageCycleLatency(pmus: LivePMUState[]): CycleLatency | null {
  const samples = pmus.map(cycleLatencyOf).filter((c): c is CycleLatency => c != null)
  if (!samples.length) return null
  const n = samples.length
  const sum = samples.reduce(
    (acc, c) => ({
      pmuWait: acc.pmuWait + c.pmuWait,
      parse: acc.parse + c.parse,
      pipeline: acc.pipeline + c.pipeline,
      pipelineRest: acc.pipelineRest + c.pipelineRest,
      total: acc.total + c.total,
      fpsHint: acc.fpsHint + c.fpsHint,
    }),
    { pmuWait: 0, parse: 0, pipeline: 0, pipelineRest: 0, total: 0, fpsHint: 0 },
  )
  return {
    pmuWait: sum.pmuWait / n,
    parse: sum.parse / n,
    pipeline: sum.pipeline / n,
    pipelineRest: sum.pipelineRest / n,
    total: sum.total / n,
    fpsHint: sum.fpsHint / n,
  }
}

export function jitterOf(pmu: LivePMUState) {
  if (!pmu.connected) return 99
  const points = pmu.trends.slice(-30)
  if (points.length < 3) return 2
  const mean = points.reduce((sum, p) => sum + p.frequency, 0) / points.length
  const variance = points.reduce((sum, p) => sum + (p.frequency - mean) ** 2, 0) / points.length
  return Math.sqrt(variance) * 100
}

export function buildOfflinePMU(name: string, meta: PMUMeta): PMUWithMeta {
  return {
    name,
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
    lastReading: { ts: 0, frequency: 0, frequencyDev: 0, mw: 0, mvar: 0, rocof: 0, statDataError: false },
    lastPhasor: {
      va: { magnitude: 0, angleDeg: 0 },
      vb: { magnitude: 0, angleDeg: 0 },
      vc: { magnitude: 0, angleDeg: 0 },
      ia: { magnitude: 0, angleDeg: 0 },
      ts: 0,
    },
    lastFrame: {
      soc: 0,
      fracSecRaw: 0,
      fracSecCount: 0,
      timeQuality: 0,
      stat: 0,
      idCode: 0,
      syncWord: 0,
      digital: 0,
    },
    lastChannels: { phasors: [], analogs: [], digitalBits: [], ts: 0 },
    cfg: { available: false, syncWord: 0, idCode: 0, station: '', fnomHz: 0, dataRate: 0, format: 0, polar: false, phFloat: false, anFloat: false, freqFloat: false, phasors: [], analogs: [], digitalWords: 0, cfgCnt: 0 },
    trends: [],
    fnomHz: 0,
    statDataError: false,
    lastHops: {},
    meta,
  }
}
