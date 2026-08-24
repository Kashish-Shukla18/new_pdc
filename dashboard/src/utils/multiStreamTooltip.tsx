import { CHART_TOOLTIP_STYLE } from './chartTooltip'
import { formatTSMs } from './format'

type PayloadItem = {
  name?: string
  value?: number | string
  color?: string
  dataKey?: string | number
  payload?: Record<string, unknown>
}

type Props = {
  active?: boolean
  label?: string | number
  payload?: PayloadItem[]
  valueSuffix?: string
  digits?: number
  /** Header prefix — use "Aligned tick" for phasor multi-PMU charts. */
  tickLabel?: string
  /** Optional per-series unit override by series name. */
  unitForName?: (name: string) => string
}

/** Multi-series tooltip with millisecond timestamp for alignment checks. */
export function MultiStreamTooltip({
  active,
  label,
  payload,
  valueSuffix = '',
  digits = 4,
  tickLabel = 'Aligned tick',
  unitForName,
}: Props) {
  if (!active || !payload?.length) return null

  const ts = Number(label)
  const present = payload.filter((item) => item.value != null && item.value !== '')

  return (
    <div style={CHART_TOOLTIP_STYLE.contentStyle}>
      <div style={{ ...CHART_TOOLTIP_STYLE.labelStyle, fontFamily: 'var(--mono, monospace)' }}>
        {tickLabel} · {formatTSMs(ts)}
      </div>
      <div style={{ fontSize: 11, color: '#8a9aab', marginTop: 2 }}>
        {present.length} series at this tick
        {Number.isFinite(ts) ? ` · epoch ${ts}` : ''}
      </div>
      <div style={{ marginTop: 6, display: 'grid', gap: 4 }}>
        {payload.map((item) => {
          const n = Number(item.value)
          const unit = unitForName?.(String(item.name ?? '')) ?? valueSuffix
          const text = Number.isFinite(n) ? `${n.toFixed(digits)}${unit}` : '—'
          return (
            <div
              key={String(item.dataKey ?? item.name)}
              style={{ display: 'flex', justifyContent: 'space-between', gap: 16 }}
            >
              <span style={{ color: item.color ?? '#e8eef6' }}>{item.name}</span>
              <span style={{ fontFamily: 'var(--mono, monospace)', color: '#e8eef6' }}>{text}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
