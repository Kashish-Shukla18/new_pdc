import type { ConnectivityKpi, ConnectivityRec, RttHistoryPoint, RttStream } from '../types/connectivity'
import type { ConnectivityRow, PMUWithMeta } from '../types/dashboard'
import { round } from './format'
import { availabilityOf, jitterOf, latencyOf, packetLossOf, pmuKey, toneFromStatus } from './pmu'

export const RTT_CHART_COLORS = ['#b91c1c', '#b45309', '#6d28d9', '#2563eb', '#15803d', '#475569']
/** Rolling samples for the latency chart (~1 Hz poll → ~1 min). */
export const RTT_WINDOW = 60

function statusLabel(pmu: PMUWithMeta, loss: number, latency: number): ConnectivityRow['statusLabel'] {
  if (!pmu.connected) return 'Offline'
  const latBad = Number.isFinite(latency) && latency > 50
  if (loss > 1.5 || latBad) return 'Degraded'
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
  if (Number.isFinite(latency) && latency > 50) return 'Check path delay'
  if (jitter > 10) return 'Check jitter source'
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
  else if (Number.isFinite(latency) && latency > 50) recommendation = 'Review network route and queueing'

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
  const measured = healthy.filter((row) => Number.isFinite(row.latency) && row.latency < 900)
  const avgLat = measured.length
    ? measured.reduce((sum, row) => sum + row.latency, 0) / measured.length
    : 0

  return [
    {
      label: 'Link issues',
      value: `${issues}`,
      subtext: `${offline} offline · ${highLoss} high loss`,
      tone: issues ? 'warn' : 'ok',
    },
    {
      label: 'Avg latency',
      value: measured.length ? `${round(avgLat, 2)} ms` : '—',
      subtext: 'Frame received → dashboard',
      tone: avgLat > 50 ? 'warn' : 'ok',
    },
    {
      label: 'Online streams',
      value: `${healthy.length}/${rows.length}`,
      subtext: 'Connected now',
      tone: offline ? 'warn' : 'ok',
    },
    {
      label: 'High loss',
      value: `${highLoss}`,
      subtext: '> 1.5% quality rejects',
      tone: highLoss ? 'bad' : 'ok',
    },
  ]
}

export function computeConnectivityRecs(rows: ConnectivityRow[]): ConnectivityRec[] {
  const recs: ConnectivityRec[] = []

  for (const row of rows.filter((r) => !r.connected).slice(0, 4)) {
    recs.push({
      sev: 'bad',
      title: `Offline — ${row.name}`,
      desc: 'No recent frames. Check IP/port, protocol, and that only one DATA client is connected.',
    })
  }

  for (const row of rows.filter((r) => r.connected && r.loss > 2).slice(0, 3)) {
    recs.push({
      sev: 'warn',
      title: `Packet loss — ${row.name}`,
      desc: `Quality rejects ${round(row.loss, 2)}%. Inspect STAT / network path.`,
    })
  }

  for (const row of rows.filter((r) => r.connected && Number.isFinite(r.latency) && r.latency > 50).slice(0, 3)) {
    recs.push({
      sev: 'warn',
      title: `High latency — ${row.name}`,
      desc: `Receive→dashboard ${round(row.latency, 2)} ms (target under ~15 ms for local pipeline).`,
    })
  }

  if (!recs.length) {
    recs.push({
      sev: 'info',
      title: 'Links nominal',
      desc: 'No offline streams or latency/loss alerts on the current snapshot.',
    })
  }

  return recs.slice(0, 8)
}

export function topRttStreams(rows: ConnectivityRow[], limit = 8): RttStream[] {
  // Stable name order so the chart set does not reshuffle every poll.
  return rows
    .filter((row) => row.connected)
    .sort((a, b) => a.name.localeCompare(b.name))
    .slice(0, limit)
    .map((row, index) => ({
      key: pmuKey(row.name),
      name: row.name,
      color: RTT_CHART_COLORS[index % RTT_CHART_COLORS.length],
      latency: Number.isFinite(row.latency) ? row.latency : 0,
    }))
}

/** Append one measured latency sample; hold last value when a hop is briefly missing. */
export function appendRttPoint(
  history: RttHistoryPoint[],
  streams: RttStream[],
  rows: ConnectivityRow[],
): RttHistoryPoint[] {
  const now = Date.now()
  const prev = history[history.length - 1]
  const point: RttHistoryPoint = {
    slot: history.length,
    label: new Date(now).toLocaleTimeString(),
    ts: now,
  }

  let any = false
  for (const stream of streams) {
    const row = rows.find((entry) => pmuKey(entry.name) === stream.key)
    const measured =
      row?.connected && Number.isFinite(row.latency) && row.latency >= 0 && row.latency < 900
        ? row.latency
        : undefined
    const held = typeof prev?.[stream.key] === 'number' ? Number(prev[stream.key]) : undefined
    const value = measured ?? held
    if (typeof value === 'number' && Number.isFinite(value)) {
      point[stream.key] = value
      any = true
    }
  }

  if (!any) return history
  return [...history, point].slice(-RTT_WINDOW)
}
