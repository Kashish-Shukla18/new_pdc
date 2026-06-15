import type {
  AnalyticsKpi,
  AnalyticsRecommendation,
  AnglePair,
  IslandingRow,
  OscillationMode,
  VoltageBus,
} from '../types/analytics'
import type { ConnectivityRow, ConversationEvent, PMUWithMeta } from '../types/dashboard'
import { round } from './format'
import { packetLossOf, pmuKey } from './pmu'

/** Phase-to-neutral RMS from stimulator.py (132 kV system). */
const NOMINAL_PHASE_VOLTAGE = 132_000 / Math.sqrt(3)

/** Primary inter-area mode injected by the simulator (Hz). */
const SIMULATOR_MODE_HZ = 0.05

const ANGLE_PAIR_COLORS = ['#ff5d6c', '#f4b740', '#3da9fc', '#a07cff', '#27d3a2', '#ff719a']

export function pairLabel(a: PMUWithMeta, b: PMUWithMeta) {
  if (a.meta.region !== b.meta.region && a.meta.region !== 'Unknown' && b.meta.region !== 'Unknown') {
    return `${a.meta.region} ↔ ${b.meta.region}`
  }
  return `${a.name} ↔ ${b.name}`
}

export function angleDeltaDeg(a: PMUWithMeta, b: PMUWithMeta) {
  const raw = Math.abs((a.lastPhasor?.va?.angleDeg ?? 0) - (b.lastPhasor?.va?.angleDeg ?? 0))
  return raw > 180 ? 360 - raw : raw
}

export function computeAnglePairs(pmus: PMUWithMeta[], limit = 10): AnglePair[] {
  if (pmus.length < 2) return []

  const pairs: AnglePair[] = []
  for (let i = 0; i < pmus.length; i++) {
    for (let j = i + 1; j < pmus.length; j++) {
      const a = pmus[i]
      const b = pmus[j]
      pairs.push({
        key: `${pmuKey(a.name)}__${pmuKey(b.name)}`,
        name: pairLabel(a, b),
        value: round(angleDeltaDeg(a, b), 2),
        regionA: a.meta.region,
        regionB: b.meta.region,
      })
    }
  }

  return pairs.sort((left, right) => right.value - left.value).slice(0, limit)
}

export function topAnglePairsForChart(pmus: PMUWithMeta[], limit = 5): AnglePair[] {
  const pairs = computeAnglePairs(pmus, pmus.length * 2)
  const seen = new Set<string>()
  const selected: AnglePair[] = []

  for (const pair of pairs) {
    const corridor = [pair.regionA, pair.regionB].sort().join('|')
    if (seen.has(corridor) && pair.regionA !== pair.regionB) continue
    seen.add(corridor)
    selected.push(pair)
    if (selected.length >= limit) break
  }

  return selected.length ? selected : pairs.slice(0, limit)
}

export function anglePairColors(pairs: AnglePair[]) {
  return pairs.map((pair, idx) => ({
    ...pair,
    color: ANGLE_PAIR_COLORS[idx % ANGLE_PAIR_COLORS.length],
  }))
}

export function computeOscillationModes(pmus: PMUWithMeta[]): OscillationMode[] {
  return pmus
    .filter((pmu) => pmu.connected)
    .map((pmu) => {
      const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)
      const freqDev = Math.abs((pmu.lastReading?.frequency ?? 50) - 50)
      const freq = round(SIMULATOR_MODE_HZ + freqDev * 0.4 + rocof * 1.5, 3)
      const damping = round(Math.max(2, Math.min(16, 14 - rocof * 220 - freqDev * 40)), 1)
      return {
        freq,
        damping,
        pmu: pmu.name,
        label: `${freq.toFixed(2)} Hz · ζ ${damping.toFixed(1)}%`,
      }
    })
}

export function computeIslandingRows(pmus: PMUWithMeta[]): IslandingRow[] {
  return pmus.map((pmu) => {
    const angle = Math.abs(pmu.lastPhasor?.va?.angleDeg ?? 0)
    const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)
    const loss = packetLossOf(pmu)

    let risk: IslandingRow['risk'] = 'Low'
    if (!pmu.connected || angle > 25 || rocof > 0.06) risk = 'High'
    else if (loss > 1 || angle > 15 || rocof > 0.03) risk = 'Medium'

    const status = !pmu.connected ? 'Offline' : loss > 1 ? 'Degraded' : 'Online'
    return {
      name: `${pmu.meta.region !== 'Unknown' ? pmu.meta.region : pmu.name} corridor`,
      risk,
      detail: `Angle ${round(angle, 2)}° · ROCOF ${round(rocof, 3)} Hz/s · ${status}`,
    }
  })
}

export function computeVoltageProfile(pmus: PMUWithMeta[]): VoltageBus[] {
  return pmus.map((pmu) => {
    const magnitude = pmu.lastPhasor?.va?.magnitude ?? 0
    const pu = magnitude > 0 ? magnitude / NOMINAL_PHASE_VOLTAGE : 0
    let tone: VoltageBus['tone'] = 'ok'
    if (pu > 0 && pu < 0.97) tone = 'bad'
    else if (pu > 1.02) tone = 'warn'
    else if (!pmu.connected || pu === 0) tone = 'warn'

    const label = pmu.meta.substation !== pmu.name ? pmu.meta.substation : pmu.name.replace(/^PMU-/, '')
    return {
      name: label.length > 12 ? label.slice(0, 11) + '…' : label,
      pu: round(pu || 0, 3),
      tone,
    }
  })
}

export function computeAnalyticsKpis(
  pmus: PMUWithMeta[],
  anglePairs: AnglePair[],
  recommendations: AnalyticsRecommendation[],
): AnalyticsKpi[] {
  const online = pmus.filter((pmu) => pmu.connected)
  const angles = online.map((pmu) => pmu.lastPhasor?.va?.angleDeg ?? 0)
  const maxPair = anglePairs[0]
  const maxAngleDelta = maxPair?.value ?? (angles.length ? round(Math.max(...angles) - Math.min(...angles), 2) : 0)

  const freqs = online.map((pmu) => pmu.lastReading?.frequency ?? 50)
  const avgFreq = freqs.length ? freqs.reduce((sum, value) => sum + value, 0) / freqs.length : 50

  const rocofs = online.map((pmu) => Math.abs(pmu.lastReading?.rocof ?? 0))
  const lowestDamping = rocofs.length
    ? round(Math.max(2, Math.min(16, 14 - Math.max(...rocofs) * 220)), 1)
    : 0
  const oscillationCount = rocofs.filter((value) => value > 0.012).length

  const risk =
    maxAngleDelta > 30 || (rocofs.length && Math.max(...rocofs) > 0.06)
      ? 'High'
      : maxAngleDelta > 20 || (rocofs.length && Math.max(...rocofs) > 0.03)
        ? 'Medium'
        : 'Low'
  const riskTone: AnalyticsKpi['tone'] = risk === 'High' ? 'bad' : risk === 'Medium' ? 'warn' : 'ok'

  return [
    {
      label: 'Max angle Δ',
      value: `${round(maxAngleDelta, 1)}°`,
      sub: maxPair ? maxPair.name : 'across simulator lanes',
      tone: maxAngleDelta > 20 ? 'warn' : 'ok',
    },
    {
      label: 'System frequency',
      value: `${round(avgFreq, 3)} Hz`,
      sub: 'average of active streams',
      tone: 'ok',
    },
    {
      label: 'Lowest damping',
      value: `${lowestDamping}%`,
      sub: `${SIMULATOR_MODE_HZ} Hz inter-area mode`,
      tone: lowestDamping < 5 ? 'bad' : lowestDamping < 8 ? 'warn' : 'ok',
    },
    {
      label: 'Active oscillations',
      value: `${oscillationCount}`,
      sub: 'streams with elevated ROCOF',
      tone: oscillationCount > 0 ? 'warn' : 'ok',
    },
    {
      label: 'Islanding risk',
      value: risk,
      sub: risk === 'Low' ? 'all corridors stable' : 'derived from angle and ROCOF',
      tone: riskTone,
    },
    {
      label: 'Open advisories',
      value: `${recommendations.length}`,
      sub: 'operator recommendations',
      tone: 'accent',
    },
  ]
}

export function computeAnalyticsRecs(
  pmus: PMUWithMeta[],
  anglePairs: AnglePair[],
  connectivityRows: ConnectivityRow[],
  events: ConversationEvent[],
): AnalyticsRecommendation[] {
  const recs: AnalyticsRecommendation[] = []
  const worstAngle = anglePairs[0]

  if (worstAngle && worstAngle.value > 25) {
    recs.push({
      sev: worstAngle.value > 30 ? 'bad' : 'warn',
      title: `Reduce corridor stress on ${worstAngle.name}`,
      desc: `Angle separation ${round(worstAngle.value, 1)}° exceeds advisory margin.`,
    })
  }

  pmus.forEach((pmu) => {
    if (!pmu.connected) return
    const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)
    const freq = pmu.lastReading?.frequency ?? 50
    if (rocof > 0.012) {
      recs.push({
        sev: rocof > 0.04 ? 'warn' : 'info',
        title: `Investigate ${SIMULATOR_MODE_HZ} Hz inter-area oscillation on ${pmu.name}`,
        desc: `ROCOF ${round(rocof, 3)} Hz/s with frequency ${round(freq, 3)} Hz indicates modal activity.`,
      })
    }

    const magnitude = pmu.lastPhasor?.va?.magnitude ?? 0
    const pu = magnitude > 0 ? magnitude / NOMINAL_PHASE_VOLTAGE : 1
    if (pu > 0 && pu < 0.97) {
      recs.push({
        sev: 'warn',
        title: `Reactive support at ${pmu.meta.substation || pmu.name}`,
        desc: `Bus voltage at ${round(pu, 3)} pu — review local VAr reserves.`,
      })
    }

    const angle = Math.abs(pmu.lastPhasor?.va?.angleDeg ?? 0)
    if (angle > 20) {
      recs.push({
        sev: 'warn',
        title: `High voltage angle on ${pmu.name}`,
        desc: `VA angle ${round(angle, 2)}° is elevated; monitor corridor stability against peer simulators.`,
      })
    }
  })

  connectivityRows.forEach((row) => {
    if (!row.connected) {
      recs.push({
        sev: 'bad',
        title: `Recover ${row.name} stream`,
        desc: 'No live frames. Validate receiver connection and restart the simulator lane.',
      })
    }
  })

  events.slice(0, 3).forEach((evt) => {
    const lowered = `${evt.status} ${evt.message}`.toLowerCase()
    if (!lowered.includes('error') && !lowered.includes('trip') && !lowered.includes('reject')) return
    recs.push({
      sev: lowered.includes('error') ? 'bad' : 'info',
      title: `${evt.stage} on ${evt.pmu}`,
      desc: evt.message,
    })
  })

  if (!recs.length) {
    recs.push({
      sev: 'info',
      title: 'All simulator lanes stable',
      desc: 'PMU simulators are tracking nominal frequency with healthy stream quality.',
    })
  }

  const seen = new Set<string>()
  return recs.filter((rec) => {
    if (seen.has(rec.title)) return false
    seen.add(rec.title)
    return true
  }).slice(0, 6)
}
