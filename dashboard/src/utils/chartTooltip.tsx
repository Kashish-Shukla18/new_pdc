import type { CSSProperties, ReactNode } from 'react'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import type { TooltipContentProps } from 'recharts/types/component/Tooltip'

type Props = Partial<TooltipContentProps<ValueType, NameType>> & {
  valueFormatter?: (
    value: ValueType | undefined,
    name: NameType | undefined,
  ) => ReactNode
}

const panelStyle: CSSProperties = {
  background: 'rgba(15, 23, 42, 0.98)',
  border: '1px solid rgba(148, 163, 184, 0.55)',
  borderRadius: 8,
  color: '#f8fafc',
  fontSize: 13,
  padding: '10px 12px',
  minWidth: 160,
  boxShadow: '0 8px 24px rgba(0,0,0,0.4)',
}

function resolveColor(entry: {
  color?: string
  stroke?: string
  fill?: string
  payload?: { fill?: string; stroke?: string; color?: string }
}): string {
  return (
    entry.color ||
    entry.stroke ||
    entry.fill ||
    entry.payload?.color ||
    entry.payload?.stroke ||
    entry.payload?.fill ||
    '#94a3b8'
  )
}

/**
 * Recharts tooltip: color dot + series tag (PMU) + value.
 */
export function ChartSeriesTooltip({
  active,
  payload,
  label,
  labelFormatter,
  formatter,
  valueFormatter,
}: Props) {
  if (!active || !payload?.length) return null

  const title =
    typeof labelFormatter === 'function'
      ? labelFormatter(label as ReactNode, payload)
      : label != null && label !== ''
        ? String(label)
        : null

  const rows = payload.filter((entry) => {
    if (entry.hide) return false
    if (entry.value == null) return false
    if (typeof entry.value === 'number' && Number.isNaN(entry.value)) return false
    return true
  })
  if (!rows.length) return null

  return (
    <div style={panelStyle}>
      {title != null && title !== '' && (
        <div style={{ color: '#e2e8f0', marginBottom: 8, fontSize: 12 }}>{title}</div>
      )}
      <div style={{ display: 'grid', gap: 7 }}>
        {rows.map((entry, index) => {
          const color = resolveColor(entry)
          const seriesName =
            entry.name != null && entry.name !== ''
              ? String(entry.name)
              : String(entry.dataKey ?? 'series')

          let valueNode: ReactNode = String(entry.value)
          let nameNode: ReactNode = seriesName

          if (typeof valueFormatter === 'function') {
            valueNode = valueFormatter(entry.value, entry.name)
          } else if (typeof formatter === 'function') {
            const out = formatter(entry.value as ValueType, entry.name as NameType, entry, index, payload)
            if (Array.isArray(out)) {
              valueNode = out[0]
              if (out[1] != null && out[1] !== '') nameNode = out[1]
            } else if (out != null) {
              valueNode = out
            }
          }

          return (
            <div
              key={`${String(entry.dataKey)}-${index}`}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                lineHeight: 1.3,
              }}
            >
              <span
                aria-hidden
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  background: color,
                  flex: 'none',
                  boxShadow: `0 0 0 2px rgba(255,255,255,0.18), 0 0 0 1px ${color}`,
                }}
              />
              <span style={{ flex: 1, minWidth: 0, color: '#f1f5f9', fontWeight: 600 }}>{nameNode}</span>
              <span
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  color: '#f8fafc',
                  fontVariantNumeric: 'tabular-nums',
                  fontWeight: 600,
                }}
              >
                <span
                  aria-hidden
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    background: color,
                    flex: 'none',
                  }}
                />
                {valueNode}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export const CHART_AXIS_TICK = {
  fill: '#e2e8f0',
  fontSize: 12,
} as const

export const CHART_GRID_STROKE = 'rgba(226, 232, 240, 0.22)'

export const CHART_LEGEND_STYLE = {
  color: '#f1f5f9',
  fontSize: 12,
  cursor: 'pointer',
} as const

/** Legacy style bag — prefer content={ChartSeriesTooltip}. */
export const CHART_TOOLTIP_STYLE = {
  contentStyle: {
    background: 'rgba(15, 23, 42, 0.98)',
    border: '1px solid rgba(148, 163, 184, 0.45)',
    borderRadius: 8,
    color: '#f8fafc',
    fontSize: 13,
  },
  labelStyle: {
    color: '#e2e8f0',
    marginBottom: 4,
    fontSize: 12,
  },
  itemStyle: {
    color: '#f8fafc',
  },
} as const
