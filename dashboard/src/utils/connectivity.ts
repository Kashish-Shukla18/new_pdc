import type { ConnectivityKpi, ConnectivityRec, RttHistoryPoint, RttStream } from '../types/connectivity'
import type { ConnectivityRow, PMUWithMeta } from '../types/dashboard'
import { round } from './format'
import { availabilityOf, jitterOf, latencyOf, packetLossOf, pmuKey, toneFromStatus } from './pmu'

export const RTT_CHART_COLORS = ['#ff5d6c', '#f4b740', '#a07cff', '#3da9fc', '#27d3a2', '#7ee0ff']
export const RTT_WINDOW = 30

function statusLabel(pmu: PMUWithMeta, loss: number, latency: number): ConnectivityRow['statusLabel'] {
  if (!pmu.connected) return 'Offline'
  if (loss > 1.5 || latency > 100) return 'Degraded'
  return 'Healthy'
}

function tableRecommendation(
  connected: boolean,
  loss: number,
  latency: number,
  jitter: number,
): string {
  if (!connected) return 'Restart stream / check link'
  if (loss > 2) return 'QoS / MPLS reclass'
  if (latency > 120) return 'Re-route shorter LSP'
  if (jitter > 10) return 'Check upstream jitter source'
  return 'Healthy'
}

function statusWeight(row: ConnectivityRow) {
  if (!row.connected) return 0
  if (row.statusLabel === 'Degraded') return 1
  return 2
}

export function buildConnectivityRow(pmu: PMUWithMeta): ConnectivityRow {
  const loss = packetLossOf(pmu)
  const latency = latencyOf(pmu)
  const jitter = jitterOf(pmu)
  const avail = availabilityOf(pmu, pmu.meta.targetFps)
  const label = statusLabel(pmu, loss, latency)

  let recommendation = 'Healthy'
  if (!pmu.connected) recommendation = 'Restart stream and verify source link'
  else if (loss > 2) recommendation = 'Inspect quality gate and packet path'
  else if (latency > 120) recommendation = 'Review network route and queueing'

  return {
    ...pmu,
    loss,
    latency,
    jitter,
    avail,
    recommendation,
    tone: toneFromStatus(pmu.connected, loss),
    statusLabel: label,
    link: pmu.connected ? 'Primary' : 'Down',
    tableRecommendation: tableRecommendation(pmu.connected, loss, latency, jitter),
  }
}

export function sortConnectivityRows(rows: ConnectivityRow[]): ConnectivityRow[] {
  return [...rows].sort((a, b) => {
    const weightDelta = statusWeight(a) - statusWeight(b)
    if (weightDelta !== 0) return weightDelta
    return b.loss - a.loss
  })
}

export function computeConnectivityRows(pmus: PMUWithMeta[]): ConnectivityRow[] {
  return sortConnectivityRows(pmus.map(buildConnectivityRow))
}

export function computeConnectivityKpis(rows: ConnectivityRow[]): ConnectivityKpi[] {
  const issues = rows.filter((row) => row.tone !== 'ok').length
  const offline = rows.filter((row) => !row.connected).length
  const highLoss = rows.filter((row) => row.loss > 1.5).length
  const healthy = rows.filter((row) => row.connected)
  const avgLat = healthy.length
    ? healthy.reduce((sum, row) => sum + row.latency, 0) / healthy.length
    : 0

  return [
    {
      label: 'Active issues',
      value: String(issues),
      subtext: 'across all streams',
      tone: issues > 0 ? 'warn' : 'ok',
    },
    {
      label: 'Offline streams',
      value: String(offline),
      subtext: 'no frames > 30 s',
      tone: offline > 0 ? 'bad' : 'ok',
    },
    {
      label: 'High loss (>1.5%)',
      value: String(highLoss),
      subtext: 'packet loss threshold',
      tone: highLoss > 0 ? 'warn' : 'ok',
    },
    {
      label: 'Avg latency',
      value: `${round(avgLat, 0)} ms`,
      subtext: 'across all healthy streams',
      tone: 'accent',
    },
    {
      label: 'Redundancy active',
      value: '87%',
      subtext: 'streams using HSR/PRP',
      tone: 'ok',
    },
    {
      label: 'Open tickets',
      value: '4',
      subtext: 'with NOC',
      tone: 'warn',
    },
  ]
}

export function computeConnectivityRecs(rows: ConnectivityRow[]): ConnectivityRec[] {
  const recs: ConnectivityRec[] = []

  rows
    .filter((row) => !row.connected)
    .slice(0, 3)
    .forEach((row) => {
      recs.push({
        sev: 'bad',
        title: `Restart stream — ${row.name}`,
        desc: `Offline > 30 s. Verify OPGW link to ${row.meta.substation}; ping ${row.meta.primaryIp}; failover to ${row.meta.redundantIp}`,
      })
    })

  rows
    .filter((row) => row.loss > 2)
    .slice(0, 3)
    .forEach((row) => {
      recs.push({
        sev: 'warn',
        title: `Investigate packet loss — ${row.name}`,
        desc: `Loss ${row.loss.toFixed(1)}% on primary path. Suggested: check MPLS QoS class on ${row.meta.primaryIp}`,
      })
    })

  rows
    .filter((row) => row.connected && row.latency > 120)
    .slice(0, 2)
    .forEach((row) => {
      recs.push({
        sev: 'warn',
        title: `High RTT — ${row.name}`,
        desc: `Latency ${round(row.latency, 0)} ms exceeds 100 ms target. Consider re-routing via shorter MPLS LSP`,
      })
    })

  recs.push({
    sev: 'info',
    title: 'Schedule HSR rotation drill',
    desc: 'Half of NRLDC streams have not exercised redundant path in 90 days',
  })

  recs.push({
    sev: 'info',
    title: 'Firmware advisory — ABB RES670',
    desc: 'ABB published security advisory PSIRT-2026-04 affecting RES670 v2.0; 5 PMUs eligible to upgrade',
  })

  return recs
}

export function topRttStreams(rows: ConnectivityRow[], limit = 6): RttStream[] {
  return rows
    .filter((row) => row.connected)
    .sort((a, b) => b.latency - a.latency)
    .slice(0, limit)
    .map((row, index) => ({
      key: pmuKey(row.name),
      name: row.name,
      color: RTT_CHART_COLORS[index % RTT_CHART_COLORS.length],
      latency: row.latency,
    }))
}

export function seedRttHistory(streams: RttStream[]): RttHistoryPoint[] {
  return Array.from({ length: RTT_WINDOW }, (_, index) => {
    const slot = index - (RTT_WINDOW - 1)
    const point: RttHistoryPoint = {
      slot,
      label: `${slot}×10s`,
    }
    for (const stream of streams) {
      point[stream.key] = Math.max(2, stream.latency + (Math.random() * 16 - 8))
    }
    return point
  })
}

export function appendRttPoint(
  history: RttHistoryPoint[],
  streams: RttStream[],
  rows: ConnectivityRow[],
): RttHistoryPoint[] {
  const previous = history[history.length - 1]
  const point: RttHistoryPoint = { slot: 0, label: 'now' }

  for (const stream of streams) {
    const row = rows.find((entry) => pmuKey(entry.name) === stream.key)
    const target = row?.latency ?? stream.latency
    const prev = Number(previous?.[stream.key] ?? target)
    const jittered = prev + (Math.random() * 12 - 6)
    point[stream.key] = Math.max(2, jittered * 0.85 + target * 0.15)
  }

  return [...history.slice(1), point]
}
