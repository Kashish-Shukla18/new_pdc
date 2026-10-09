import { useCallback, useMemo, useState } from 'react'
import { Box, Stack, Typography } from '@mui/material'

export type LegendItem = {
  /** Stable series id (dataKey). */
  key: string
  /** Full name for tooltips. */
  name: string
  color: string
}

/** Shorten endpoint labels when many share a host (:4712). */
export function shortEndpointLabel(name: string, allNames: string[]): string {
  const hosts = allNames.map((n) => {
    // "a ↔ b" pairs: shorten each side
    if (n.includes('↔')) return ''
    const i = n.lastIndexOf(':')
    return i > 0 ? n.slice(0, i) : n
  }).filter(Boolean)

  if (name.includes('↔')) {
    return name
      .split('↔')
      .map((side) => shortEndpointLabel(side.trim(), allNames.flatMap((n) => n.split('↔').map((s) => s.trim()))))
      .join(' ↔ ')
  }

  const sameHost = hosts.length > 1 && hosts.every((h) => h === hosts[0])
  if (!sameHost) return name
  const i = name.lastIndexOf(':')
  return i > 0 ? `:${name.slice(i + 1)}` : name
}

export function useSeriesVisibility(seriesKeys: string[]) {
  const [hidden, setHidden] = useState<Set<string>>(() => new Set())

  const keySig = seriesKeys.join('\0')
  const activeHidden = useMemo(() => {
    const next = new Set<string>()
    for (const k of hidden) {
      if (seriesKeys.includes(k)) next.add(k)
    }
    return next
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hidden, keySig])

  const toggle = useCallback((key: string) => {
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
        return next
      }
      // Never hide the last visible series.
      const visible = seriesKeys.filter((k) => !next.has(k) && k !== key)
      if (visible.length === 0) return prev
      next.add(key)
      return next
    })
  }, [seriesKeys])

  const isHidden = useCallback((key: string) => activeHidden.has(key), [activeHidden])

  return { hidden: activeHidden, toggle, isHidden }
}

type ExternalChartLegendProps = {
  items: LegendItem[]
  hidden: Set<string>
  onToggle: (key: string) => void
  /** Optional short display label; defaults to item.name */
  labelOf?: (item: LegendItem) => string
  maxHeight?: number
}

/** Scrollable legend below the plot — never overlays the chart area. */
export function ExternalChartLegend({
  items,
  hidden,
  onToggle,
  labelOf,
  maxHeight = 88,
}: ExternalChartLegendProps) {
  if (!items.length) return null

  return (
    <Stack
      direction="row"
      spacing={0.75}
      useFlexGap
      sx={{
        flexWrap: 'wrap',
        justifyContent: 'flex-start',
        alignContent: 'flex-start',
        maxHeight,
        overflowY: 'auto',
        pt: 1,
        mt: 0.5,
        borderTop: '1px solid',
        borderColor: 'divider',
      }}
    >
      {items.map((item) => {
        const on = !hidden.has(item.key)
        const label = labelOf ? labelOf(item) : item.name
        return (
          <Box
            key={item.key}
            component="button"
            type="button"
            onClick={() => onToggle(item.key)}
            title={item.name}
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 0.6,
              px: 0.85,
              py: 0.3,
              borderRadius: 1,
              bgcolor: on ? 'action.hover' : 'transparent',
              border: '1px solid',
              borderColor: on ? 'divider' : 'transparent',
              cursor: 'pointer',
              opacity: on ? 1 : 0.4,
              color: 'text.primary',
              font: 'inherit',
              '&:hover': { bgcolor: 'action.selected' },
            }}
          >
            <Box
              sx={{
                width: 10,
                height: 10,
                borderRadius: '50%',
                bgcolor: item.color,
                flex: 'none',
                opacity: on ? 1 : 0.45,
              }}
            />
            <Typography
              variant="caption"
              sx={{
                fontWeight: 650,
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                textDecoration: on ? 'none' : 'line-through',
                whiteSpace: 'nowrap',
              }}
            >
              {label}
            </Typography>
          </Box>
        )
      })}
    </Stack>
  )
}
