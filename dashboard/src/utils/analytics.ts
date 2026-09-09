import { ANGLE_PAIR_COLORS } from './analyticsColors'
import type {
  AnalyticsKpi,
  AnalyticsRecommendation,
  AnglePair,
} from '../types/analytics'
import type { ConnectivityRow, ConversationEvent, PMUWithMeta } from '../types/dashboard'
import { round } from './format'
import { packetLossOf, pmuKey } from './pmu'

function fnomOf(pmu: PMUWithMeta, fallback = 60) {
  return pmu.fnomHz && pmu.fnomHz > 0 ? pmu.fnomHz : fallback
}

function fleetFnom(pmus: PMUWithMeta[], fallback = 60) {
  const values = pmus.map((p) => p.fnomHz).filter((v): v is number => !!v && v > 0)
  return values.length ? values[0] : fallback
}

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

export function anglePairColors(pairs: AnglePair[]) {
  return pairs.map((pair, idx) => ({
    ...pair,
    color: ANGLE_PAIR_COLORS[idx % ANGLE_PAIR_COLORS.length],
  }))
}

export function computeAnalyticsKpis(
  pmus: PMUWithMeta[],
  anglePairs: AnglePair[],
  recommendations: AnalyticsRecommendation[],
): AnalyticsKpi[] {
  const online = pmus.filter((pmu) => pmu.connected)
  const maxPair = anglePairs[0]
  const maxAngleDelta = maxPair?.value ?? 0

  const nom = fleetFnom(online)
  const freqs = online.map((pmu) => pmu.lastReading?.frequency ?? fnomOf(pmu, nom))
  const avgFreq = freqs.length ? freqs.reduce((sum, value) => sum + value, 0) / freqs.length : nom

  let worstDf = 0
  let worstDfPMU = '—'
  let maxRocof = 0
  let maxRocofPMU = '—'
  for (const pmu of online) {
    const fnom = fnomOf(pmu, nom)
    const df =
      typeof pmu.lastReading?.frequencyDev === 'number'
        ? pmu.lastReading.frequencyDev
        : (pmu.lastReading?.frequency ?? fnom) - fnom
    if (Math.abs(df) >= Math.abs(worstDf)) {
      worstDf = df
      worstDfPMU = pmu.name
    }
    const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)
    if (rocof >= maxRocof) {
      maxRocof = rocof
      maxRocofPMU = pmu.name
    }
  }

  return [
    {
      label: 'Max angle Δ',
      value: online.length < 2 ? '—' : `${round(maxAngleDelta, 1)}°`,
      sub: online.length < 2 ? 'needs ≥2 online PMUs' : maxPair ? maxPair.name : 'inter-PMU VA',
      tone: online.length < 2 ? 'neutral' : maxAngleDelta > 20 ? 'warn' : 'ok',
    },
    {
      label: 'Avg frequency',
      value: `${round(avgFreq, 3)} Hz`,
      sub: online.length ? `${online.length} online · FNOM ${nom} Hz` : 'no online streams',
      tone: 'ok',
    },
    {
      label: 'Worst Δf',
      value: online.length ? `${worstDf >= 0 ? '+' : ''}${round(worstDf, 4)} Hz` : '—',
      sub: online.length ? worstDfPMU : 'no online streams',
      tone: Math.abs(worstDf) > 0.05 ? 'warn' : 'ok',
    },
    {
      label: 'Max |ROCOF|',
      value: online.length ? `${round(maxRocof, 4)} Hz/s` : '—',
      sub: online.length ? maxRocofPMU : 'no online streams',
      tone: maxRocof > 0.1 ? 'warn' : 'neutral',
    },
    {
      label: 'Online / total',
      value: `${online.length} / ${pmus.length}`,
      sub: 'streams receiving frames',
      tone: online.length < pmus.length ? 'warn' : 'ok',
    },
    {
      label: 'Open advisories',
      value: `${recommendations.length}`,
      sub: 'from live telemetry',
      tone: recommendations.length ? 'accent' : 'ok',
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
      title: `Large angle separation: ${worstAngle.name}`,
      desc: `Inter-PMU VA angle Δ = ${round(worstAngle.value, 1)}° (from live phasors).`,
    })
  }

  pmus.forEach((pmu) => {
    if (!pmu.connected) return

    const fnom = fnomOf(pmu)
    const freq = pmu.lastReading?.frequency ?? fnom
    const df =
      typeof pmu.lastReading?.frequencyDev === 'number'
        ? pmu.lastReading.frequencyDev
        : freq - fnom
    const rocof = Math.abs(pmu.lastReading?.rocof ?? 0)

    if (Math.abs(df) > 0.05) {
      recs.push({
        sev: Math.abs(df) > 0.1 ? 'warn' : 'info',
        title: `Frequency deviation on ${pmu.name}`,
        desc: `f = ${round(freq, 3)} Hz · Δf = ${df >= 0 ? '+' : ''}${round(df, 4)} Hz vs CFG FNOM ${fnom} Hz.`,
      })
    }

    if (rocof > 0.05) {
      recs.push({
        sev: rocof > 0.1 ? 'warn' : 'info',
        title: `Elevated ROCOF on ${pmu.name}`,
        desc: `|ROCOF| = ${round(rocof, 4)} Hz/s from DATA frame DFREQ.`,
      })
    }

    if (pmu.statDataError || pmu.lastReading?.statDataError) {
      recs.push({
        sev: 'bad',
        title: `STAT data error on ${pmu.name}`,
        desc: 'Latest STAT word reports a data-error condition.',
      })
    }
  })

  connectivityRows.forEach((row) => {
    if (!row.connected) {
      recs.push({
        sev: 'bad',
        title: `Offline: ${row.name}`,
        desc: 'No recent DATA frames. Check reachability and PDC receiver link.',
      })
    } else if (packetLossOf(row) > 1) {
      recs.push({
        sev: 'warn',
        title: `Quality rejects on ${row.name}`,
        desc: `Reject rate ≈ ${round(packetLossOf(row), 2)}% of frames (qualityRejects / totalFrames).`,
      })
    }
  })

  events.slice(0, 5).forEach((evt) => {
    const lowered = `${evt.status} ${evt.message}`.toLowerCase()
    if (!lowered.includes('error') && !lowered.includes('reject') && !lowered.includes('timeout')) return
    recs.push({
      sev: lowered.includes('error') ? 'bad' : 'info',
      title: `${evt.stage} · ${evt.pmu}`,
      desc: evt.message,
    })
  })

  const seen = new Set<string>()
  return recs
    .filter((rec) => {
      if (seen.has(rec.title)) return false
      seen.add(rec.title)
      return true
    })
    .slice(0, 8)
}
