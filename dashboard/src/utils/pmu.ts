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
    targetFps: config?.data_rate && config.data_rate > 0 ? config.data_rate : 30,
    lat: config?.lat ?? 0,
    lon: config?.lon ?? 0,
  }
}

export function toneFromStatus(connected: boolean, loss: number): 'ok' | 'warn' | 'bad' {
  if (!connected) return 'bad'
  if (loss > 1) return 'warn'
  return 'ok'
}

export function availabilityOf(pmu: LivePMUState, targetFps: number) {
  if (!pmu.connected) return 0
  const fpsRatio = Math.min(1.05, pmu.approxFps / Math.max(1, targetFps))
  const rejectPenalty = Math.min(8, pmu.qualityRejects * 0.03)
  return Math.max(0, Math.min(100, fpsRatio * 100 - rejectPenalty))
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
