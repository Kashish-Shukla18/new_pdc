import { useMemo, useState } from 'react'
import {
  Box,
  Card,
  CardContent,
  CardHeader,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Typography,
} from '@mui/material'
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { CHART_COLORS } from '../../constants'
import type { AlignedBatch, PMUWithMeta } from '../../types/dashboard'
import {
  CHART_AXIS_TICK,
  CHART_GRID_STROKE,
  CHART_LEGEND_STYLE,
  ChartSeriesTooltip,
} from '../../utils/chartTooltip'
import { formatTS, formatTSMs, round } from '../../utils/format'
import { pmuKey } from '../../utils/pmu'

export const PHASOR_TREND_WINDOW = 90

type Props = {
  pmus: PMUWithMeta[]
  kind: 'voltage' | 'current'
  alignedBatches?: AlignedBatch[]
}

const V_PHASES = [
  { key: 'va', label: 'VA', unit: 'V' },
  { key: 'vb', label: 'VB', unit: 'V' },
  { key: 'vc', label: 'VC', unit: 'V' },
] as const

const I_PHASES = [
  { key: 'ia', label: 'IA', unit: 'A' },
  { key: 'ib', label: 'IB', unit: 'A' },
  { key: 'ic', label: 'IC', unit: 'A' },
] as const

type PhaseKey = (typeof V_PHASES)[number]['key'] | (typeof I_PHASES)[number]['key']

type SeriesOption = {
  key: string
  label: string
  unit: string
  kind: 'phase' | 'analog'
}

function cfgAnalogNames(pmus: PMUWithMeta[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const pmu of pmus) {
    const names = pmu.cfg?.analogs?.length
      ? pmu.cfg.analogs
      : (pmu.lastChannels?.analogs ?? []).map((a) => a.name)
    for (const name of names) {
      if (!name || seen.has(name)) continue
      seen.add(name)
      out.push(name)
    }
  }
  return out
}

export function PhasorMagnitudeChart({ pmus, kind, alignedBatches = [] }: Props) {
  const phaseKeys = kind === 'voltage' ? V_PHASES : I_PHASES
  const analogNames = useMemo(
    () => (kind === 'voltage' ? cfgAnalogNames(pmus) : []),
    [kind, pmus],
  )

  const seriesOptions = useMemo<SeriesOption[]>(() => {
    const phases: SeriesOption[] = phaseKeys.map((p) => ({
      key: p.key,
      label: p.label,
      unit: p.unit,
      kind: 'phase',
    }))
    const analogs: SeriesOption[] = analogNames.map((name) => ({
      key: `analog:${name}`,
      label: name,
      unit: '',
      kind: 'analog',
    }))
    return [...phases, ...analogs]
  }, [phaseKeys, analogNames])

  const defaultKey = kind === 'voltage' ? 'va' : 'ia'
  const [selectedKey, setSelectedKey] = useState(defaultKey)

  const selected =
    seriesOptions.find((opt) => opt.key === selectedKey) ?? seriesOptions[0] ?? {
      key: defaultKey,
      label: defaultKey.toUpperCase(),
      unit: kind === 'voltage' ? 'V' : 'A',
      kind: 'phase' as const,
    }

  const activeKey = seriesOptions.some((opt) => opt.key === selected.key)
    ? selected.key
    : defaultKey
  const active = seriesOptions.find((opt) => opt.key === activeKey) ?? selected

  const analogName = active.kind === 'analog' ? active.key.slice('analog:'.length) : ''
  const seriesSuffix = active.kind === 'analog' ? pmuKey(analogName) : active.key

  // One line per PMU — click legend labels to show/hide (Recharts default).
  const chartSeries = pmus.map((pmu, idx) => ({
    dataKey: `${pmuKey(pmu.name)}__${seriesSuffix}`,
    name: pmu.name,
    color: CHART_COLORS[idx % CHART_COLORS.length],
  }))

  const data = useMemo(() => {
    const batches = alignedBatches.slice(-PHASOR_TREND_WINDOW)
    return batches.map((batch) => {
      const row: Record<string, number | undefined> & { ts: number } = { ts: batch.ts }
      for (const pmu of pmus) {
        const key = `${pmuKey(pmu.name)}__${seriesSuffix}`
        const point = batch.points?.[pmu.name]
        let value: number | undefined
        if (active.kind === 'analog') {
          const v = point?.analogs?.[analogName]
          value = typeof v === 'number' ? v : undefined
        } else {
          const v = point?.[active.key as PhaseKey]
          value = typeof v === 'number' ? v : undefined
        }
        row[key] = value
      }
      return row
    })
  }, [alignedBatches, pmus, seriesSuffix, active.kind, active.key, analogName])

  const hasNumeric = data.some((row) =>
    chartSeries.some((series) => typeof row[series.dataKey] === 'number'),
  )
  const hasSeries = data.length > 0 && chartSeries.length > 0
  const isAnalog = active.kind === 'analog'

  const subtitle = data.length
    ? isAnalog
      ? `${active.label} · CFG analog · ${pmus.length} PMU${pmus.length === 1 ? '' : 's'} · click legend to toggle`
      : `${active.label} magnitude · ${pmus.length} PMU${pmus.length === 1 ? '' : 's'} · click legend to toggle`
    : `Waiting for aligned ${kind} ticks…`

  const valueFmt = (value: unknown) =>
    value == null || Number.isNaN(Number(value))
      ? '—'
      : `${round(Number(value), isAnalog ? 3 : 2)}${active.unit ? ` ${active.unit}` : ''}`

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader
        title={kind === 'voltage' ? 'Voltage Phasors' : 'Current Phasors'}
        subheader={subtitle}
        action={
          <FormControl size="small" sx={{ minWidth: 88, mr: 1, mt: 0.5 }}>
            <InputLabel id={`${kind}-series-label`}>Series</InputLabel>
            <Select
              labelId={`${kind}-series-label`}
              label="Series"
              value={activeKey}
              onChange={(e) => setSelectedKey(e.target.value)}
            >
              {seriesOptions.map((opt) => (
                <MenuItem key={opt.key} value={opt.key}>
                  {opt.label}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        }
        slotProps={{
          title: { variant: 'h6', sx: { fontSize: '1rem' } },
          subheader: { sx: { color: 'text.secondary', fontSize: 13 } },
        }}
        sx={{
          flexWrap: 'wrap',
          gap: 1,
          '& .MuiCardHeader-action': { m: 0, alignSelf: 'center' },
        }}
      />
      <CardContent sx={{ pt: 0 }}>
        <Box sx={{ width: '100%', height: { xs: 240, sm: 280 }, minWidth: 0 }}>
          {!hasSeries ? (
            <Typography sx={{ p: 2, color: 'text.secondary' }}>
              Waiting for aligned {kind} phasor ticks…
            </Typography>
          ) : !hasNumeric && isAnalog ? (
            <Typography sx={{ p: 2, color: 'text.secondary' }}>
              No values yet for CFG analog “{analogName}”. Waiting for aligned ticks…
            </Typography>
          ) : (
            <ResponsiveContainer width="100%" height="100%" minWidth={1} minHeight={1}>
              <LineChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid stroke={CHART_GRID_STROKE} strokeDasharray="3 3" />
                <XAxis
                  dataKey="ts"
                  tickFormatter={formatTS}
                  tick={CHART_AXIS_TICK}
                  interval="preserveStartEnd"
                  minTickGap={28}
                  stroke="#94a3b8"
                />
                <YAxis
                  tick={CHART_AXIS_TICK}
                  domain={isAnalog ? ['auto', 'auto'] : [0, 'auto']}
                  tickFormatter={(v) => `${round(Number(v), 1)}`}
                  width={52}
                  stroke="#94a3b8"
                />
                <Tooltip
                  content={(props) => (
                    <ChartSeriesTooltip
                      {...props}
                      labelFormatter={(value) => formatTSMs(Number(value))}
                      formatter={(value, name) => [valueFmt(value), String(name)]}
                    />
                  )}
                />
                <Legend wrapperStyle={CHART_LEGEND_STYLE} />
                {chartSeries.map((series) => (
                  <Line
                    key={series.dataKey}
                    type="monotone"
                    dataKey={series.dataKey}
                    name={series.name}
                    stroke={series.color}
                    dot={false}
                    activeDot={{ r: 4, strokeWidth: 2, fill: '#0f172a' }}
                    strokeWidth={2.25}
                    isAnimationActive={false}
                    connectNulls={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          )}
        </Box>
      </CardContent>
    </Card>
  )
}
