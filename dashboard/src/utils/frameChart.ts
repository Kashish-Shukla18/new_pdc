import type { PMUWithMeta, TrendPoint } from '../types/dashboard'

export const FRAME_TREND_WINDOW = 90

/** Seed chart history from real trend samples only — no synthetic padding. */
export function seedFrameTrendHistory(pmu: PMUWithMeta | undefined): TrendPoint[] {
  if (!pmu) return []
  return pmu.trends.slice(-FRAME_TREND_WINDOW)
}

/** Keep the rolling window aligned with parser trends (real timestamps). */
export function appendFrameTrendPoint(history: TrendPoint[], pmu: PMUWithMeta | undefined): TrendPoint[] {
  if (!pmu) return history
  return pmu.trends.slice(-FRAME_TREND_WINDOW)
}
