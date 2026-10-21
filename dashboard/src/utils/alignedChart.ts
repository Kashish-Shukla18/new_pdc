import type { AlignedBatch, AlignedPoint, TrendPoint } from '../types/dashboard'
import { pmuKey } from './pmu'

/** How many aligned ticks to keep on overview / frame charts. */
export const ALIGNED_CHART_WINDOW = 90

const ALIGNED_PHASOR_FIELDS = [
  { label: 'VA', mag: 'va', angle: 'vaAngle' },
  { label: 'VB', mag: 'vb', angle: 'vbAngle' },
  { label: 'VC', mag: 'vc', angle: 'vcAngle' },
  { label: 'IA', mag: 'ia', angle: 'iaAngle' },
  { label: 'IB', mag: 'ib', angle: 'ibAngle' },
  { label: 'IC', mag: 'ic', angle: 'icAngle' },
] as const

export type AlignedPhasorRow = {
  label: string
  cfgName: string
  magnitude: number
  angleDeg: number
}

/**
 * Newest aligned tick for diagrams / snapshots.
 * Always the latest tick (partials included) so missing PMUs match frequency-chart gaps.
 */
export function latestAlignedBatch(
  batches: AlignedBatch[] | undefined,
  preferPmuNames?: string[],
): AlignedBatch | undefined {
  if (!batches?.length) return undefined
  const names = preferPmuNames?.filter(Boolean) ?? []
  const hasAny = (b: AlignedBatch) => {
    if (!names.length) return Object.keys(b.points ?? {}).length > 0
    return names.some((n) => Boolean(b.points?.[n]))
  }
  for (let i = batches.length - 1; i >= 0; i--) {
    const b = batches[i]
    if (hasAny(b)) return b
  }
  return undefined
}

/** VA–IC mag/angle from one aligned point (same fields as Overview / Analytics trends). */
export function phasorsFromAlignedPoint(point: AlignedPoint | undefined): AlignedPhasorRow[] {
  if (!point) return []
  const out: AlignedPhasorRow[] = []
  for (const f of ALIGNED_PHASOR_FIELDS) {
    const magnitude = point[f.mag]
    const angleDeg = point[f.angle]
    if (typeof magnitude !== 'number' || typeof angleDeg !== 'number') continue
    if (magnitude === 0 && angleDeg === 0) continue
    out.push({ label: f.label, cfgName: f.label, magnitude, angleDeg })
  }
  return out
}

/**
 * Build one chart row per aligned tick for the selected PMUs.
 * Missing PMUs get no value (gap on the chart when connectNulls is false).
 */
export function rowsFromAlignedBatches(
  batches: AlignedBatch[] | undefined,
  pmuNames: string[],
  window = ALIGNED_CHART_WINDOW,
): Record<string, number | undefined>[] {
  if (!batches?.length || !pmuNames.length) return []

  return batches.slice(-window).map((batch) => {
    const row: Record<string, number | undefined> = { ts: batch.ts }
    for (const name of pmuNames) {
      const key = pmuKey(name)
      const point = batch.points?.[name]
      if (!point) continue
      row[`${key}__frequency`] = point.frequency
      if (typeof point.frequencyDev === 'number') {
        row[`${key}__frequencyDev`] = point.frequencyDev
      }
      if (typeof point.rocof === 'number') {
        row[`${key}__rocof`] = point.rocof
      }
    }
    return row
  })
}

/**
 * Single-PMU frequency series from aligned ticks (Data Frames page).
 * Gaps stay as holes (no fabricated points).
 */
export function trendFromAlignedBatches(
  batches: AlignedBatch[] | undefined,
  pmuName: string,
  window = ALIGNED_CHART_WINDOW,
): TrendPoint[] {
  if (!batches?.length || !pmuName) return []

  const out: TrendPoint[] = []
  for (const batch of batches.slice(-window)) {
    const point = batch.points?.[pmuName]
    if (!point) continue
    const analogs = point.analogs ?? {}
    const analogVals = Object.values(analogs)
    out.push({
      ts: batch.ts,
      frequency: point.frequency,
      frequencyDev: point.frequencyDev ?? 0,
      rocof: point.rocof ?? 0,
      mw: analogs.Analog1 ?? analogVals[0] ?? 0,
      mvar: analogs.Analog2 ?? analogVals[1] ?? 0,
      va: point.va ?? 0,
      vb: point.vb ?? 0,
      vc: point.vc ?? 0,
      ia: point.ia ?? 0,
      ib: point.ib ?? 0,
      ic: point.ic ?? 0,
    })
  }
  return out
}
