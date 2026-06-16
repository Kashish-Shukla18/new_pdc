import type { PMUWithMeta, TrendPoint } from '../types/dashboard'

export const FRAME_TREND_WINDOW = 60

export function seedFrameTrendHistory(pmu: PMUWithMeta | undefined): TrendPoint[] {
  if (!pmu) return []

  const existing = pmu.trends.slice(-FRAME_TREND_WINDOW)
  const baseFreq = pmu.lastReading?.frequency || 50
  const baseRocof = pmu.lastReading?.rocof || 0
  const baseMw = pmu.lastReading?.mw || 0
  const baseMvar = pmu.lastReading?.mvar || 0
  const now = Date.now()

  if (existing.length >= FRAME_TREND_WINDOW) {
    return existing
  }

  const points: TrendPoint[] = []
  const padCount = FRAME_TREND_WINDOW - existing.length

  for (let i = 0; i < padCount; i++) {
    points.push({
      ts: now - (FRAME_TREND_WINDOW - i) * 1000,
      frequency: baseFreq + (Math.random() - 0.5) * 0.06,
      rocof: baseRocof + (Math.random() - 0.5) * 0.03,
      mw: baseMw,
      mvar: baseMvar,
    })
  }

  return [...points, ...existing]
}

export function appendFrameTrendPoint(history: TrendPoint[], pmu: PMUWithMeta | undefined): TrendPoint[] {
  if (!pmu || history.length === 0) return history

  const latestTrend = pmu.trends[pmu.trends.length - 1]
  const point: TrendPoint = latestTrend
    ? { ...latestTrend, ts: Date.now() }
    : {
        ts: Date.now(),
        frequency: pmu.lastReading?.frequency || 50,
        rocof: pmu.lastReading?.rocof || 0,
        mw: pmu.lastReading?.mw || 0,
        mvar: pmu.lastReading?.mvar || 0,
      }

  return [...history.slice(1), point]
}
