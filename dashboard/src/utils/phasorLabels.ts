import type { NamedPhasorView } from '../types/dashboard'

export type DisplayPhasor = NamedPhasorView & {
  label: string
  cfgName: string
}

const STANDARD_ORDER = ['VA', 'VB', 'VC', 'IA', 'IB', 'IC'] as const

/** Map CFG channel names (e.g. PZR.AV, VA) to standard labels. */
export function standardPhasorLabel(cfgName: string): string | null {
  const n = cfgName.trim().toUpperCase()
  if (n === 'VA' || /(^|[.\-_])AV$/.test(n) || n.endsWith('AV')) return 'VA'
  if (n === 'VB' || /(^|[.\-_])BV$/.test(n) || n.endsWith('BV')) return 'VB'
  if (n === 'VC' || /(^|[.\-_])CV$/.test(n) || n.endsWith('CV')) return 'VC'
  if (n === 'IA' || /(^|[.\-_])AI$/.test(n) || n.endsWith('AI')) return 'IA'
  if (n === 'IB' || /(^|[.\-_])BI$/.test(n) || n.endsWith('BI')) return 'IB'
  if (n === 'IC' || /(^|[.\-_])CI$/.test(n) || n.endsWith('CI')) return 'IC'
  return null
}

/** Relabel + sort: VA, VB, VC, IA, IB, IC with CFG name kept as secondary. */
export function displayPhasors(phasors: NamedPhasorView[] | undefined): DisplayPhasor[] {
  if (!phasors?.length) return []

  return [...phasors]
    .map((p, index) => {
      const label = standardPhasorLabel(p.name) ?? p.name
      const order = STANDARD_ORDER.indexOf(label as (typeof STANDARD_ORDER)[number])
      return {
        name: p.name,
        magnitude: p.magnitude,
        angleDeg: p.angleDeg,
        label,
        cfgName: p.name,
        order: order >= 0 ? order : 100 + index,
      }
    })
    .sort((a, b) => a.order - b.order)
    .map(({ name, magnitude, angleDeg, label, cfgName }) => ({
      name,
      magnitude,
      angleDeg,
      label,
      cfgName,
    }))
}

/** CFG-2 list for the profile panel: "VA (PZR.AV), …" */
export function formatPhasorCfgList(names: string[] | undefined, fallback = '—') {
  if (!names?.length) return fallback
  return names
    .map((name) => {
      const label = standardPhasorLabel(name)
      return label && label !== name ? `${label} (${name})` : name
    })
    .join(', ')
}
