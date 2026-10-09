import type { NamedPhasorView } from '../types/dashboard'

export type DisplayPhasor = NamedPhasorView & {
  label: string
  cfgName: string
}

const STANDARD_ORDER = ['VA', 'VB', 'VC', 'IA', 'IB', 'IC'] as const

/**
 * Map CFG channel names onto VA–IC.
 * Mirrors parser.ClassifyPhasorName (V1/I1, "PHASOR CH 1:V1", …AV/…AI, etc.).
 */
export function standardPhasorLabel(cfgName: string): string | null {
  let n = cfgName.trim().toUpperCase()
  const colon = n.lastIndexOf(':')
  if (colon >= 0 && colon + 1 < n.length) {
    n = n.slice(colon + 1)
  }
  n = n.replace(/[\s_\-.]/g, '')
  if (!n) return null

  switch (n) {
    case 'VA':
    case 'VAN':
    case 'V1':
    case 'PHASEA':
    case 'PHASEAV':
    case 'VOLTAGEA':
      return 'VA'
    case 'VB':
    case 'VBN':
    case 'V2':
    case 'PHASEB':
    case 'PHASEBV':
    case 'VOLTAGEB':
      return 'VB'
    case 'VC':
    case 'VCN':
    case 'V3':
    case 'PHASEC':
    case 'PHASECV':
    case 'VOLTAGEC':
      return 'VC'
    case 'IA':
    case 'IAN':
    case 'I1':
    case 'CURRENTA':
    case 'PHASEAI':
      return 'IA'
    case 'IB':
    case 'IBN':
    case 'I2':
    case 'CURRENTB':
    case 'PHASEBI':
      return 'IB'
    case 'IC':
    case 'ICN':
    case 'I3':
    case 'CURRENTC':
    case 'PHASECI':
      return 'IC'
  }

  if (n.endsWith('AV') || n.startsWith('VA') || n.startsWith('V1') || n.endsWith('V1')) return 'VA'
  if (n.endsWith('BV') || n.startsWith('VB') || n.startsWith('V2') || n.endsWith('V2')) return 'VB'
  if (n.endsWith('CV') || n.startsWith('VC') || n.startsWith('V3') || n.endsWith('V3')) return 'VC'
  if (n.endsWith('AI') || n.startsWith('IA') || n.startsWith('I1') || n.endsWith('I1')) return 'IA'
  if (n.endsWith('BI') || n.startsWith('IB') || n.startsWith('I2') || n.endsWith('I2')) return 'IB'
  if (n.endsWith('CI') || n.startsWith('IC') || n.startsWith('I3') || n.endsWith('I3')) return 'IC'
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
