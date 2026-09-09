/** Phase palette only: yellow / red / blue. Same hue for V and I of that phase. */
export const PHASE_YELLOW = '#f5d76e'
export const PHASE_RED = '#f94144'
export const PHASE_BLUE = '#3b82f6'

export const PHASOR_V_COLORS: Record<string, string> = {
  VA: PHASE_YELLOW,
  VB: PHASE_RED,
  VC: PHASE_BLUE,
}

export const PHASOR_I_COLORS: Record<string, string> = {
  IA: PHASE_YELLOW,
  IB: PHASE_RED,
  IC: PHASE_BLUE,
}

export const PHASOR_V_BAR_COLORS = [PHASOR_V_COLORS.VA, PHASOR_V_COLORS.VB, PHASOR_V_COLORS.VC]
export const PHASOR_I_BAR_COLORS = [PHASOR_I_COLORS.IA, PHASOR_I_COLORS.IB, PHASOR_I_COLORS.IC]

export const ANGLE_PAIR_COLORS = [
  '#2563eb',
  '#15803d',
  '#b45309',
  '#b91c1c',
  '#6d28d9',
  '#c2410c',
  '#0f766e',
  '#be185d',
  '#4d7c0f',
  '#475569',
]