import { useEffect, useMemo, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Checkbox,
  FormControl,
  FormControlLabel,
  InputLabel,
  Menu,
  MenuItem,
  Select,
  Stack,
  Typography,
} from '@mui/material'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { AngleHistoryPoint, AnglePair } from '../../types/analytics'
import type { AlignedBatch, AlignedPoint, PMUWithMeta } from '../../types/dashboard'
import { anglePairColors, shortPairLabel } from '../../utils/analytics'
import { ExternalChartLegend, useSeriesVisibility } from '../../utils/chartLegend'
import {
  CHART_AXIS_TICK,
  CHART_GRID_STROKE,
  ChartSeriesTooltip,
} from '../../utils/chartTooltip'
import { formatTS, formatTSMs, round } from '../../utils/format'
import { chartLabel, pmuKey } from '../../utils/pmu'

type AngleChannel = 'va' | 'vb' | 'vc' | 'ia' | 'ib' | 'ic'

const CHANNELS: { key: AngleChannel; label: string }[] = [
  { key: 'va', label: 'VA' },
  { key: 'vb', label: 'VB' },
  { key: 'vc', label: 'VC' },
  { key: 'ia', label: 'IA' },
  { key: 'ib', label: 'IB' },
  { key: 'ic', label: 'IC' },
]

const ANGLE_FIELD: Record<AngleChannel, keyof AlignedPoint> = {
  va: 'vaAngle',
  vb: 'vbAngle',
  vc: 'vcAngle',
  ia: 'iaAngle',
  ib: 'ibAngle',
  ic: 'icAngle',
}

type Props = {
  pmus: PMUWithMeta[]
  alignedBatches?: AlignedBatch[]
}

function wrapDelta(deg: number) {
  const raw = Math.abs(deg)
  return raw > 180 ? 360 - raw : raw
}

function buildPairs(selected: PMUWithMeta[]): AnglePair[] {
  if (selected.length < 2) return []
  const pairs: AnglePair[] = []
  for (let i = 0; i < selected.length; i++) {
    for (let j = i + 1; j < selected.length; j++) {
      const a = selected[i]
      const b = selected[j]
      pairs.push({
        key: `${pmuKey(a.name)}__${pmuKey(b.name)}`,
        name: shortPairLabel(a, b),
        value: 0,
        regionA: a.meta.region,
        regionB: b.meta.region,
      })
    }
  }
  return pairs
}

export function AngleDiffChart({ pmus, alignedBatches = [] }: Props) {
  const onlineNames = useMemo(
    () => pmus.filter((p) => p.connected).map((p) => p.name),
    [pmus],
  )

  const [channel, setChannel] = useState<AngleChannel>('va')
  const [excludedPMUNames, setExcludedPMUNames] = useState<string[]>([])
  const [pmuAnchor, setPmuAnchor] = useState<null | HTMLElement>(null)

  useEffect(() => {
    const offline = pmus.filter((p) => !p.connected).map((p) => p.name)
    setExcludedPMUNames((prev) => {
      const onlineExcluded = prev.filter((n) => onlineNames.includes(n))
      return [...new Set([...onlineExcluded, ...offline])]
    })
  }, [pmus, onlineNames])

  const selectedPMUs = useMemo(
    () => pmus.filter((p) => p.connected && !excludedPMUNames.includes(p.name)),
    [pmus, excludedPMUNames],
  )

  const pairs = useMemo(() => buildPairs(selectedPMUs), [selectedPMUs])
  const coloredPairs = useMemo(() => anglePairColors(pairs), [pairs])
  const seriesKeys = useMemo(() => coloredPairs.map((p) => p.key), [coloredPairs])
  const { hidden, toggle, isHidden } = useSeriesVisibility(seriesKeys)

  const history = useMemo(() => {
    if (!pairs.length || !alignedBatches.length) return [] as AngleHistoryPoint[]
    const field = ANGLE_FIELD[channel]

    return alignedBatches.slice(-60).map((batch) => {
      const point: AngleHistoryPoint = {
        ts: batch.ts,
        label: new Date(batch.ts).toLocaleTimeString(),
      }
      for (const pair of pairs) {
        const [aKey, bKey] = pair.key.split('__')
        const aName = pmus.find((p) => pmuKey(p.name) === aKey)?.name
        const bName = pmus.find((p) => pmuKey(p.name) === bKey)?.name
        const a = aName ? batch.points?.[aName] : undefined
        const b = bName ? batch.points?.[bName] : undefined
        const aAng = a ? a[field] : undefined
        const bAng = b ? b[field] : undefined
        if (typeof aAng === 'number' && typeof bAng === 'number') {
          point[pair.key] = round(wrapDelta(aAng - bAng), 2)
        }
      }
      return point
    })
  }, [alignedBatches, pairs, channel, pmus])

  const channelLabel = CHANNELS.find((c) => c.key === channel)?.label ?? 'VA'

  const togglePMU = (name: string) => {
    const pmu = pmus.find((p) => p.name === name)
    if (!pmu?.connected) return
    setExcludedPMUNames((current) => {
      const isExcluded = current.includes(name)
      if (isExcluded) return current.filter((item) => item !== name)
      if (selectedPMUs.length <= 2) return current
      return [...current, name]
    })
  }

  const legendItems = useMemo(
    () => coloredPairs.map((p) => ({ key: p.key, name: p.name, color: p.color })),
    [coloredPairs],
  )

  return (
    <Card variant="outlined">
      <CardHeader
        title={`Inter-PMU Angle Δ (${channelLabel})`}
        action={
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', pr: 1, pt: 0.5, flexWrap: 'wrap' }}>
            <FormControl size="small" sx={{ minWidth: 96 }}>
              <InputLabel id="angle-channel-label">Channel</InputLabel>
              <Select
                labelId="angle-channel-label"
                label="Channel"
                value={channel}
                onChange={(e) => setChannel(e.target.value as AngleChannel)}
              >
                {CHANNELS.map((c) => (
                  <MenuItem key={c.key} value={c.key}>
                    {c.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <Button size="small" variant="outlined" onClick={(e) => setPmuAnchor(e.currentTarget)}>
              PMUs ({selectedPMUs.length}/{onlineNames.length})
            </Button>
            <Menu
              anchorEl={pmuAnchor}
              open={Boolean(pmuAnchor)}
              onClose={() => setPmuAnchor(null)}
              slotProps={{ paper: { sx: { minWidth: 200, px: 1 } } }}
            >
              <MenuItem
                dense
                onClick={() =>
                  setExcludedPMUNames(pmus.filter((p) => !p.connected).map((p) => p.name))
                }
                sx={{ color: 'primary.main' }}
              >
                Select all online
              </MenuItem>
              {pmus.map((pmu) => (
                <MenuItem key={pmu.name} dense disableRipple sx={{ py: 0 }}>
                  <FormControlLabel
                    control={
                      <Checkbox
                        size="small"
                        disabled={!pmu.connected}
                        checked={pmu.connected && !excludedPMUNames.includes(pmu.name)}
                        onChange={() => togglePMU(pmu.name)}
                      />
                    }
                    label={`${chartLabel(pmu)}${pmu.connected ? '' : ' (offline)'}`}
                    sx={{
                      width: '100%',
                      mr: 0,
                      opacity: pmu.connected ? 1 : 0.55,
                    }}
                  />
                </MenuItem>
              ))}
            </Menu>
          </Stack>
        }
        slotProps={{
          title: { variant: 'h6', sx: { fontSize: '1rem' } },
        }}
        sx={{
          flexWrap: 'wrap',
          gap: 1,
          '& .MuiCardHeader-action': { m: 0, alignSelf: 'center' },
        }}
      />
      <CardContent sx={{ pt: 0 }}>
        <Box sx={{ width: '100%', minWidth: 0 }}>
          {selectedPMUs.length < 2 ? (
            <Typography sx={{ p: 2, color: 'text.secondary' }}>
              Angle Δ needs at least two selected PMUs.
            </Typography>
          ) : !history.length ? (
            <Typography sx={{ p: 2, color: 'text.secondary' }}>
              Waiting for aligned {channelLabel} angle ticks…
            </Typography>
          ) : (
            <>
              <Box sx={{ width: '100%', height: { xs: 240, sm: 280 } }}>
                <ResponsiveContainer width="100%" height="100%" minWidth={1} minHeight={1}>
                  <LineChart data={history} margin={{ top: 8, right: 8, left: 0, bottom: 4 }}>
                    <CartesianGrid stroke={CHART_GRID_STROKE} strokeDasharray="3 3" />
                    <XAxis
                      dataKey="ts"
                      tickFormatter={formatTS}
                      tick={CHART_AXIS_TICK}
                      stroke="#94a3b8"
                      minTickGap={28}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      tick={CHART_AXIS_TICK}
                      domain={[0, 'auto']}
                      tickFormatter={(value) => `${value}°`}
                      stroke="#94a3b8"
                      width={48}
                    />
                    <Tooltip
                      content={(props) => (
                        <ChartSeriesTooltip
                          {...props}
                          labelFormatter={(value) => formatTSMs(Number(value))}
                          formatter={(value, name) => [
                            `${Number(value ?? 0).toFixed(2)}°`,
                            String(name),
                          ]}
                        />
                      )}
                    />
                    {coloredPairs.map((pair) =>
                      isHidden(pair.key) ? null : (
                        <Line
                          key={pair.key}
                          type="linear"
                          dataKey={pair.key}
                          name={pair.name}
                          stroke={pair.color}
                          strokeWidth={2.1}
                          strokeOpacity={0.95}
                          dot={false}
                          isAnimationActive={false}
                          connectNulls={false}
                        />
                      ),
                    )}
                  </LineChart>
                </ResponsiveContainer>
              </Box>
              <ExternalChartLegend
                items={legendItems}
                hidden={hidden}
                onToggle={toggle}
                maxHeight={120}
              />
            </>
          )}
        </Box>
      </CardContent>
    </Card>
  )
}
