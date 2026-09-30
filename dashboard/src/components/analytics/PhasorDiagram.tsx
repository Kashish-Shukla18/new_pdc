import { useMemo, useState } from 'react'
import { Box, Card, CardContent, CardHeader, Stack, Typography } from '@mui/material'
import { round } from '../../utils/format'

type Kind = 'voltage' | 'current'

/** One arrow on the polar plot (may come from any selected PMU). */
export type DiagramVector = {
  id: string
  pmuName: string
  label: string
  cfgName: string
  magnitude: number
  angleDeg: number
  color: string
}

type Props = {
  /** All candidate vectors (typically every online PMU). Legend toggles visibility. */
  vectors: DiagramVector[]
  kind: Kind
  subtitle?: string
  emptyMessage?: string
}

function toXY(mag: number, angleDeg: number, scale: number) {
  const rad = (angleDeg * Math.PI) / 180
  return { x: mag * scale * Math.cos(rad), y: mag * scale * Math.sin(rad) }
}

/** Stable PMU order → dash pattern so overlapping arrows stay distinguishable. */
const DASH_BY_INDEX = ['', '6 4', '2 3', '8 3 2 3', '1 4']

type Placed = DiagramVector & {
  pmuIndex: number
  stackIndex: number
  stackCount: number
}

function placeVectors(vectors: DiagramVector[]): Placed[] {
  const pmuOrder: string[] = []
  for (const v of vectors) {
    if (!pmuOrder.includes(v.pmuName)) pmuOrder.push(v.pmuName)
  }

  const buckets = new Map<string, DiagramVector[]>()
  for (const v of vectors) {
    const key = `${v.label}|${Math.round(v.angleDeg)}`
    const list = buckets.get(key) ?? []
    list.push(v)
    buckets.set(key, list)
  }

  const out: Placed[] = []
  for (const list of buckets.values()) {
    list.sort((a, b) => pmuOrder.indexOf(a.pmuName) - pmuOrder.indexOf(b.pmuName))
    list.forEach((v, stackIndex) => {
      out.push({
        ...v,
        pmuIndex: Math.max(0, pmuOrder.indexOf(v.pmuName)),
        stackIndex,
        stackCount: list.length,
      })
    })
  }
  return out
}

function pmuMeta(vectors: DiagramVector[]) {
  const order: string[] = []
  const meta = new Map<string, { color: string; index: number }>()
  for (const v of vectors) {
    if (!meta.has(v.pmuName)) {
      meta.set(v.pmuName, { color: v.color, index: order.length })
      order.push(v.pmuName)
    }
  }
  return { order, meta }
}

export function PhasorDiagram({ vectors, kind, subtitle, emptyMessage }: Props) {
  const size = 300
  const cx = size / 2
  const cy = size / 2
  const plotR = 118

  const title = kind === 'voltage' ? 'Voltage Phasor Diagram' : 'Current Phasor Diagram'
  const { order: allPmuNames, meta: pmuInfo } = useMemo(() => pmuMeta(vectors), [vectors])

  // Legend-style hide (like ECharts / Chart.js). Never hide the last visible PMU.
  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const visibleNames = allPmuNames.filter((n) => !hidden.has(n))
  const visibleVectors = useMemo(
    () => vectors.filter((v) => !hidden.has(v.pmuName)),
    [vectors, hidden],
  )

  const maxMag = Math.max(...visibleVectors.map((p) => p.magnitude), 1e-9)
  const scale = plotR / maxMag
  const ticks = [0.33, 0.66, 1]
  const pmuCount = visibleNames.length
  const defaultSub = visibleVectors.length
    ? `${pmuCount} PMU${pmuCount === 1 ? '' : 's'} · ${visibleVectors.length} vector${visibleVectors.length === 1 ? '' : 's'}`
    : vectors.length
      ? 'All PMUs hidden — click a legend label to show'
      : 'Waiting for phasor channels'

  const placed = useMemo(() => placeVectors(visibleVectors), [visibleVectors])

  const byPmu = new Map<string, { color: string; items: DiagramVector[] }>()
  for (const v of visibleVectors) {
    const cur = byPmu.get(v.pmuName) ?? { color: v.color, items: [] }
    cur.items.push(v)
    byPmu.set(v.pmuName, cur)
  }

  const [hoverId, setHoverId] = useState<string | null>(null)

  const togglePmu = (name: string) => {
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(name)) {
        next.delete(name)
        return next
      }
      const stillVisible = allPmuNames.filter((n) => n !== name && !next.has(n))
      if (stillVisible.length === 0) return prev
      next.add(name)
      return next
    })
  }

  const gridStroke = 'rgba(226, 232, 240, 0.35)'
  const axisStroke = 'rgba(226, 232, 240, 0.45)'

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader
        title={title}
        subheader={subtitle ?? defaultSub}
        slotProps={{
          title: { variant: 'h6', sx: { fontSize: '1rem' } },
          subheader: { sx: { color: 'text.secondary', fontSize: 13 } },
        }}
      />
      <CardContent sx={{ pt: 0 }}>
        {!vectors.length ? (
          <Typography sx={{ p: 1.5, color: 'text.secondary', bgcolor: 'action.hover', borderRadius: 1 }}>
            {emptyMessage ?? `No live ${kind} phasors for the selected online PMUs / channels.`}
          </Typography>
        ) : (
          <Stack spacing={1.5} sx={{ alignItems: 'stretch' }}>
            <Stack
              direction={{ xs: 'column', sm: 'row' }}
              spacing={2}
              sx={{ alignItems: 'center', justifyContent: 'center' }}
            >
              <Box sx={{ width: '100%', maxWidth: 300 }}>
                <Box
                  component="svg"
                  viewBox={`0 0 ${size} ${size}`}
                  role="img"
                  aria-label={title}
                  sx={{
                    width: '100%',
                    height: 'auto',
                    display: 'block',
                    bgcolor: 'rgba(7, 12, 22, 0.55)',
                    borderRadius: 2,
                    border: '1px solid',
                    borderColor: 'divider',
                  }}
                >
                  {ticks.map((t) => (
                    <circle
                      key={t}
                      cx={cx}
                      cy={cy}
                      r={plotR * t}
                      fill="none"
                      stroke={gridStroke}
                      strokeWidth={1.25}
                    />
                  ))}
                  <line x1={cx - plotR} y1={cy} x2={cx + plotR} y2={cy} stroke={axisStroke} strokeWidth={1.25} />
                  <line x1={cx} y1={cy - plotR} x2={cx} y2={cy + plotR} stroke={axisStroke} strokeWidth={1.25} />

                  {placed.map((p) => {
                    const { x, y } = toXY(p.magnitude, p.angleDeg, scale)
                    const x2 = cx + x
                    const y2 = cy - y
                    const dim = hoverId != null && hoverId !== p.id
                    const dash = DASH_BY_INDEX[p.pmuIndex % DASH_BY_INDEX.length]
                    // Multi-PMU: skip tip text (side list + legend). Single PMU: phase at tip, pushed out if tiny.
                    const tipR = Math.hypot(x, y)
                    const showTip = pmuCount === 1
                    let lx = x2 + (x >= 0 ? 10 : -10)
                    let ly = y2 + (y >= 0 ? -6 : 14)
                    if (showTip && tipR < plotR * 0.35 && tipR > 0) {
                      const boost = (plotR * 0.42) / tipR
                      lx = cx + x * boost + (x >= 0 ? 8 : -8)
                      ly = cy - y * boost + (y >= 0 ? -6 : 12)
                    }

                    return (
                      <g
                        key={p.id}
                        style={{ cursor: 'default' }}
                        opacity={dim ? 0.22 : 1}
                        onMouseEnter={() => setHoverId(p.id)}
                        onMouseLeave={() => setHoverId(null)}
                      >
                        <line
                          x1={cx}
                          y1={cy}
                          x2={x2}
                          y2={y2}
                          stroke={p.color}
                          strokeWidth={hoverId === p.id ? 3.2 : 2.4}
                          strokeOpacity={0.95}
                          strokeDasharray={dash || undefined}
                        />
                        <line x1={cx} y1={cy} x2={x2} y2={y2} stroke="transparent" strokeWidth={14} />
                        <circle cx={x2} cy={y2} r={hoverId === p.id ? 4 : 3.1} fill={p.color} />
                        {showTip && (
                          <text
                            x={lx}
                            y={ly}
                            fill={p.color}
                            fontSize={11}
                            fontWeight={700}
                            textAnchor={x >= 0 ? 'start' : 'end'}
                          >
                            {p.label}
                          </text>
                        )}
                      </g>
                    )
                  })}
                  <circle cx={cx} cy={cy} r={2.5} fill="#cbd5e1" />
                </Box>
              </Box>

              <Stack spacing={1.25} sx={{ minWidth: { sm: 160 }, width: { xs: '100%', sm: 'auto' } }}>
                {visibleVectors.length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    Click a PMU label below to show vectors.
                  </Typography>
                ) : (
                  [...byPmu.entries()].map(([pmuName, group]) => (
                    <Box key={pmuName}>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 0.5 }}>
                        <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: group.color, flex: 'none' }} />
                        <Typography
                          variant="caption"
                          sx={{
                            letterSpacing: '0.06em',
                            textTransform: 'uppercase',
                            color: 'text.secondary',
                            fontWeight: 700,
                          }}
                        >
                          {pmuName}
                        </Typography>
                      </Stack>
                      <Stack spacing={0.75}>
                        {group.items.map((p) => (
                          <Stack
                            key={p.id}
                            direction="row"
                            spacing={1}
                            onMouseEnter={() => setHoverId(p.id)}
                            onMouseLeave={() => setHoverId(null)}
                            sx={{
                              alignItems: 'center',
                              cursor: 'default',
                              opacity: hoverId && hoverId !== p.id ? 0.45 : 1,
                            }}
                          >
                            <Box
                              sx={{
                                width: 8,
                                height: 8,
                                borderRadius: '50%',
                                bgcolor: p.color,
                                flex: 'none',
                              }}
                            />
                            <Typography variant="body2" sx={{ color: 'text.primary' }}>
                              <Box component="strong" sx={{ mr: 0.5 }}>
                                {p.label}
                              </Box>
                              {round(p.magnitude, 1)}∠{round(p.angleDeg, 0)}°
                            </Typography>
                          </Stack>
                        ))}
                      </Stack>
                    </Box>
                  ))
                )}
              </Stack>
            </Stack>

            {/* Clickable legend — toggle streams like ECharts / Chart.js */}
            <Stack
              direction="row"
              spacing={1}
              useFlexGap
              sx={{
                flexWrap: 'wrap',
                justifyContent: 'center',
                pt: 0.75,
                borderTop: '1px solid',
                borderColor: 'divider',
              }}
            >
              {allPmuNames.map((pmuName) => {
                const info = pmuInfo.get(pmuName)!
                const isOn = !hidden.has(pmuName)
                const dash = DASH_BY_INDEX[info.index % DASH_BY_INDEX.length]
                return (
                  <Box
                    key={pmuName}
                    component="button"
                    type="button"
                    onClick={() => togglePmu(pmuName)}
                    title={isOn ? `Hide ${pmuName}` : `Show ${pmuName}`}
                    sx={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 0.75,
                      px: 1,
                      py: 0.5,
                      borderRadius: 1,
                      bgcolor: isOn ? 'action.hover' : 'transparent',
                      border: '1px solid',
                      borderColor: isOn ? 'divider' : 'transparent',
                      cursor: 'pointer',
                      opacity: isOn ? 1 : 0.45,
                      color: 'text.primary',
                      font: 'inherit',
                      '&:hover': { bgcolor: 'action.selected' },
                    }}
                  >
                    <Box
                      component="svg"
                      width={22}
                      height={10}
                      viewBox="0 0 22 10"
                      aria-hidden
                      sx={{ flex: 'none', display: 'block' }}
                    >
                      <line
                        x1={1}
                        y1={5}
                        x2={21}
                        y2={5}
                        stroke={info.color}
                        strokeWidth={3}
                        strokeLinecap="round"
                        strokeDasharray={dash || undefined}
                        strokeOpacity={isOn ? 1 : 0.4}
                      />
                    </Box>
                    <Typography
                      variant="caption"
                      sx={{
                        fontWeight: 700,
                        color: 'text.primary',
                        textDecoration: isOn ? 'none' : 'line-through',
                      }}
                    >
                      {pmuName}
                    </Typography>
                  </Box>
                )
              })}
            </Stack>
          </Stack>
        )}
      </CardContent>
    </Card>
  )
}
