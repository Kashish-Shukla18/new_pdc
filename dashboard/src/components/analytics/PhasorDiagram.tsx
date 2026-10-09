import { useEffect, useMemo, useState } from 'react'
import {
  Box,
  Card,
  CardContent,
  CardHeader,
  FormControl,
  MenuItem,
  Select,
  Stack,
  Typography,
} from '@mui/material'
import { round } from '../../utils/format'

type Kind = 'voltage' | 'current'

/** One arrow on the polar plot (may come from any selected PMU). */
export type DiagramVector = {
  id: string
  pmuName: string
  /** CFG station (or endpoint fallback) for legends / compare UI. */
  pmuLabel: string
  label: string
  cfgName: string
  magnitude: number
  angleDeg: number
  color: string
}

type Props = {
  vectors: DiagramVector[]
  kind: Kind
  emptyMessage?: string
}

function toXY(mag: number, angleDeg: number, scale: number) {
  const rad = (angleDeg * Math.PI) / 180
  return { x: mag * scale * Math.cos(rad), y: mag * scale * Math.sin(rad) }
}

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

function stationLabels(vectors: DiagramVector[]): Map<string, string> {
  const out = new Map<string, string>()
  for (const v of vectors) {
    if (!out.has(v.pmuName)) {
      out.set(v.pmuName, v.pmuLabel || v.pmuName)
    }
  }
  return out
}

function formatPhaseLine(p: DiagramVector) {
  return `${p.label} ${round(p.magnitude, 1)}∠${round(p.angleDeg, 0)}°`
}

export function PhasorDiagram({ vectors, kind, emptyMessage }: Props) {
  const size = 300
  const cx = size / 2
  const cy = size / 2
  const plotR = 118

  const title = kind === 'voltage' ? 'Voltage Phasor Diagram' : 'Current Phasor Diagram'
  const { order: allPmuNames, meta: pmuInfo } = useMemo(() => pmuMeta(vectors), [vectors])
  const labels = useMemo(() => stationLabels(vectors), [vectors])

  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const [compareA, setCompareA] = useState('')
  const [compareB, setCompareB] = useState('')
  const [hoverPmu, setHoverPmu] = useState<string | null>(null)

  const visibleNames = allPmuNames.filter((n) => !hidden.has(n))
  const visibleVectors = useMemo(
    () => vectors.filter((v) => !hidden.has(v.pmuName)),
    [vectors, hidden],
  )

  // Keep compare picks on visible streams; seed defaults from the first two visible.
  useEffect(() => {
    if (!visibleNames.length) {
      setCompareA('')
      return
    }
    setCompareA((prev) => (prev && visibleNames.includes(prev) ? prev : visibleNames[0]))
  }, [visibleNames.join('|')])

  useEffect(() => {
    if (!visibleNames.length) {
      setCompareB('')
      return
    }
    setCompareB((prev) => {
      if (prev && visibleNames.includes(prev) && prev !== compareA) return prev
      return visibleNames.find((n) => n !== compareA) ?? ''
    })
  }, [visibleNames.join('|'), compareA])

  const maxMag = Math.max(...visibleVectors.map((p) => p.magnitude), 1e-9)
  const scale = plotR / maxMag
  const ticks = [0.33, 0.66, 1]

  const placed = useMemo(() => placeVectors(visibleVectors), [visibleVectors])

  const byPmu = useMemo(() => {
    const map = new Map<string, { color: string; items: DiagramVector[] }>()
    for (const v of vectors) {
      const cur = map.get(v.pmuName) ?? { color: v.color, items: [] }
      cur.items.push(v)
      map.set(v.pmuName, cur)
    }
    return map
  }, [vectors])

  const compareNames = [compareA, compareB].filter(Boolean)

  const toggleVisibility = (name: string) => {
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

  const renderCompareCard = (
    slot: 'A' | 'B',
    value: string,
    onChange: (name: string) => void,
  ) => {
    if (visibleNames.length === 0) return null
    if (slot === 'B' && visibleNames.length < 2) return null

    const options = visibleNames.filter((n) => (slot === 'A' ? n !== compareB : n !== compareA) || n === value)
    const name = value && options.includes(value) ? value : options[0] ?? ''
    const group = name ? byPmu.get(name) : undefined
    const color = name ? pmuInfo.get(name)?.color ?? '#94a3b8' : '#94a3b8'

    return (
      <Box
        key={slot}
        onMouseEnter={() => name && setHoverPmu(name)}
        onMouseLeave={() => setHoverPmu(null)}
        sx={{
          px: 1,
          py: 0.85,
          borderRadius: 1.5,
          border: '1px solid',
          borderColor: hoverPmu === name ? 'primary.main' : 'divider',
          bgcolor: 'action.hover',
        }}
      >
        <FormControl size="small" fullWidth sx={{ mb: 0.75 }}>
          <Select
            value={name}
            onChange={(e) => onChange(String(e.target.value))}
            sx={{
              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
              fontSize: 12,
              fontWeight: 700,
              '.MuiSelect-select': { py: 0.6, display: 'flex', alignItems: 'center', gap: 1 },
            }}
            renderValue={(selected) => (
              <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
                <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: color, flex: 'none' }} />
                <span>{labels.get(selected) ?? selected}</span>
              </Stack>
            )}
          >
            {options.map((n) => (
              <MenuItem key={n} value={n} sx={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}>
                {labels.get(n) ?? n}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <Stack spacing={0.2}>
          {(group?.items ?? []).map((p) => (
            <Typography
              key={p.id}
              variant="caption"
              sx={{
                color: 'text.primary',
                fontVariantNumeric: 'tabular-nums',
                lineHeight: 1.35,
                display: 'block',
              }}
            >
              <Box component="span" sx={{ fontWeight: 700, color: p.color, mr: 0.5 }}>
                {p.label}
              </Box>
              {round(p.magnitude, 1)}∠{round(p.angleDeg, 0)}°
            </Typography>
          ))}
        </Stack>
      </Box>
    )
  }

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader
        title={title}
        slotProps={{
          title: { variant: 'h6', sx: { fontSize: '1rem' } },
        }}
      />
      <CardContent sx={{ pt: 0 }}>
        {!vectors.length ? (
          <Typography sx={{ p: 1.5, color: 'text.secondary', bgcolor: 'action.hover', borderRadius: 1 }}>
            {emptyMessage ?? `No live ${kind} phasors for the selected online PMUs / channels.`}
          </Typography>
        ) : (
          <Stack spacing={1.25} sx={{ alignItems: 'stretch' }}>
            <Stack
              direction={{ xs: 'column', sm: 'row' }}
              spacing={1.5}
              sx={{ alignItems: { xs: 'stretch', sm: 'flex-start' }, justifyContent: 'center' }}
            >
              <Box sx={{ width: '100%', maxWidth: 300, flex: '0 0 auto', mx: { xs: 'auto', sm: 0 } }}>
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
                    const inCompare = compareNames.includes(p.pmuName)
                    const dim = hoverPmu != null && p.pmuName !== hoverPmu
                    const dash = DASH_BY_INDEX[p.pmuIndex % DASH_BY_INDEX.length]
                    const tipR = Math.hypot(x, y)
                    const showTip = visibleNames.length === 1
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
                        opacity={dim ? 0.2 : 1}
                        onMouseEnter={() => setHoverPmu(p.pmuName)}
                        onMouseLeave={() => setHoverPmu(null)}
                      >
                        <line
                          x1={cx}
                          y1={cy}
                          x2={x2}
                          y2={y2}
                          stroke={p.color}
                          strokeWidth={inCompare || hoverPmu === p.pmuName ? 3 : 2.3}
                          strokeOpacity={0.95}
                          strokeDasharray={dash || undefined}
                        />
                        <line x1={cx} y1={cy} x2={x2} y2={y2} stroke="transparent" strokeWidth={14} />
                        <circle cx={x2} cy={y2} r={3.1} fill={p.color} />
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

              <Stack
                spacing={1}
                sx={{
                  flex: '1 1 180px',
                  minWidth: { sm: 168 },
                  maxWidth: { sm: 220 },
                }}
              >
                {renderCompareCard('A', compareA, setCompareA)}
                {renderCompareCard('B', compareB, setCompareB)}
              </Stack>
            </Stack>

            <Stack
              direction="row"
              spacing={0.75}
              useFlexGap
              sx={{
                flexWrap: 'wrap',
                justifyContent: 'center',
                pt: 0.5,
                borderTop: '1px solid',
                borderColor: 'divider',
                maxHeight: 80,
                overflowY: 'auto',
              }}
            >
              {allPmuNames.map((pmuName) => {
                const info = pmuInfo.get(pmuName)!
                const isOn = !hidden.has(pmuName)
                const dash = DASH_BY_INDEX[info.index % DASH_BY_INDEX.length]
                const short = labels.get(pmuName) ?? pmuName
                const group = byPmu.get(pmuName)
                const tip = group
                  ? `${short}\n${group.items.map(formatPhaseLine).join('\n')}`
                  : short
                return (
                  <Box
                    key={pmuName}
                    component="button"
                    type="button"
                    onClick={() => toggleVisibility(pmuName)}
                    onMouseEnter={() => setHoverPmu(pmuName)}
                    onMouseLeave={() => setHoverPmu(null)}
                    title={isOn ? tip : `Show ${short}`}
                    sx={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 0.6,
                      px: 0.85,
                      py: 0.35,
                      borderRadius: 1,
                      bgcolor: isOn ? 'action.hover' : 'transparent',
                      border: '1px solid',
                      borderColor: isOn ? 'divider' : 'transparent',
                      cursor: 'pointer',
                      opacity: isOn ? 1 : 0.4,
                      color: 'text.primary',
                      font: 'inherit',
                      '&:hover': { bgcolor: 'action.selected' },
                    }}
                  >
                    <Box
                      component="svg"
                      width={18}
                      height={8}
                      viewBox="0 0 18 8"
                      aria-hidden
                      sx={{ flex: 'none', display: 'block' }}
                    >
                      <line
                        x1={1}
                        y1={4}
                        x2={17}
                        y2={4}
                        stroke={info.color}
                        strokeWidth={2.5}
                        strokeLinecap="round"
                        strokeDasharray={dash || undefined}
                        strokeOpacity={isOn ? 1 : 0.4}
                      />
                    </Box>
                    <Typography
                      variant="caption"
                      sx={{
                        fontWeight: 700,
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                        color: 'text.primary',
                        textDecoration: isOn ? 'none' : 'line-through',
                      }}
                    >
                      {short}
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
