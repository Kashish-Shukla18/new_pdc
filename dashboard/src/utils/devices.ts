import type { PMUWithMeta } from '../types/dashboard'
import { availabilityOf, latencyOf, packetLossOf, toneFromStatus } from './pmu'
import { round } from './format'

export const REGION_COLORS: Record<string, string> = {
  NRLDC: '#3da9fc',
  WRLDC: '#27d3a2',
  SRLDC: '#a07cff',
  ERLDC: '#f4b740',
  NERLDC: '#ff5d6c',
  Unknown: '#7ee0ff',
}

export const VOLTAGE_CLASSES = ['765 kV', '400 kV', '220 kV', 'HVDC', '132 kV'] as const
export const VOLTAGE_COLORS = ['#3da9fc', '#7ee0ff', '#27d3a2', '#a07cff', '#f4b740']

export type DeviceSummary = {
  label: string
  value: string
  subtext: string
  tone: 'ok' | 'warn' | 'bad' | 'accent' | 'neutral'
}

export function voltageClassFor(pmu: PMUWithMeta): string {
  if (pmu.meta.voltage && pmu.meta.voltage !== '-') return pmu.meta.voltage
  let hash = 0
  for (let i = 0; i < pmu.name.length; i++) hash = (hash + pmu.name.charCodeAt(i) * (i + 1)) % VOLTAGE_CLASSES.length
  return VOLTAGE_CLASSES[hash]
}

export function vendorFor(pmu: PMUWithMeta): string {
  return pmu.meta.vendor
}

export function statusLabel(pmu: PMUWithMeta): 'Healthy' | 'Degraded' | 'Offline' {
  if (!pmu.connected) return 'Offline'
  const loss = packetLossOf(pmu)
  if (loss > 1.5) return 'Degraded'
  return 'Healthy'
}

export function computeDeviceSummary(pmus: PMUWithMeta[]): DeviceSummary[] {
  const healthy = pmus.filter((p) => statusLabel(p) === 'Healthy').length
  const degraded = pmus.filter((p) => statusLabel(p) === 'Degraded').length
  const offline = pmus.filter((p) => statusLabel(p) === 'Offline').length
  const regions = new Set(pmus.map((p) => p.meta.region)).size
  const avgAvail = pmus.length
    ? round(pmus.reduce((sum, p) => sum + availabilityOf(p, p.meta.targetFps), 0) / pmus.length, 1)
    : 0

  return [
    { label: 'Registered PMUs', value: String(pmus.length), subtext: `${regions} control regions`, tone: 'accent' },
    { label: 'Healthy', value: String(healthy), subtext: 'full FPS · sync OK', tone: 'ok' },
    { label: 'Degraded', value: String(degraded), subtext: 'quality or loss flags', tone: degraded > 0 ? 'warn' : 'neutral' },
    { label: 'Offline', value: String(offline), subtext: 'no frames > 30 s', tone: offline > 0 ? 'bad' : 'neutral' },
    { label: 'Avg availability', value: `${avgAvail}%`, subtext: 'rolling 60 min window', tone: avgAvail < 95 ? 'warn' : 'ok' },
    { label: 'Avg latency', value: `${round(pmus.filter((p) => p.connected).reduce((s, p) => s + latencyOf(p), 0) / Math.max(1, pmus.filter((p) => p.connected).length), 0)} ms`, subtext: 'connected streams only', tone: 'neutral' },
  ]
}

export function regionChartData(pmus: PMUWithMeta[]) {
  const counts = new Map<string, number>()
  for (const pmu of pmus) {
    const region = pmu.meta.region || 'Unknown'
    counts.set(region, (counts.get(region) ?? 0) + 1)
  }
  return Array.from(counts.entries()).map(([name, value]) => ({
    name,
    value,
    color: REGION_COLORS[name] ?? REGION_COLORS.Unknown,
  }))
}

export function voltageChartData(pmus: PMUWithMeta[]) {
  return VOLTAGE_CLASSES.map((label, index) => ({
    name: label,
    count: pmus.filter((p) => voltageClassFor(p) === label).length,
    color: VOLTAGE_COLORS[index],
  }))
}

export function vendorChartData(pmus: PMUWithMeta[]) {
  const counts = new Map<string, number>()
  for (const pmu of pmus) {
    const vendor = vendorFor(pmu)
    counts.set(vendor, (counts.get(vendor) ?? 0) + 1)
  }
  return Array.from(counts.entries())
    .sort((a, b) => b[1] - a[1])
    .map(([name, count], index) => ({
      name,
      count,
      color: VOLTAGE_COLORS[index % VOLTAGE_COLORS.length],
    }))
}
export { toneFromStatus, availabilityOf, latencyOf, packetLossOf }
