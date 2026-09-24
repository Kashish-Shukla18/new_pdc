import { useMemo, useState } from 'react'
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
import { CHART_TOOLTIP_STYLE } from '../../utils/chartTooltip'
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

/** Union of CFG-2 analog channel names across selected PMUs (order preserved). */
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
  const [excludedPMUNames, setExcludedPMUNames] = useState<string[]>([])

  const selected =
    seriesOptions.find((opt) => opt.key === selectedKey) ?? seriesOptions[0] ?? {
      key: defaultKey,
      label: defaultKey.toUpperCase(),
      unit: kind === 'voltage' ? 'V' : 'A',
      kind: 'phase' as const,
    }

  // Keep selection valid when CFG analog list changes.
  const activeKey = seriesOptions.some((opt) => opt.key === selected.key)
    ? selected.key
    : defaultKey
  const active = seriesOptions.find((opt) => opt.key === activeKey) ?? selected

  const selectedPMUs = pmus.filter((pmu) => !excludedPMUNames.includes(pmu.name))
  const analogName = active.kind === 'analog' ? active.key.slice('analog:'.length) : ''
  const seriesSuffix = active.kind === 'analog' ? pmuKey(analogName) : active.key

  const chartSeries = selectedPMUs.map((pmu) => ({
    dataKey: `${pmuKey(pmu.name)}__${seriesSuffix}`,
    name: pmu.name,
    color: CHART_COLORS[pmus.findIndex((candidate) => candidate.name === pmu.name) % CHART_COLORS.length],
  }))

  const data = useMemo(() => {
    const batches = alignedBatches.slice(-PHASOR_TREND_WINDOW)
    return batches.map((batch) => {
      const row: Record<string, number | undefined> & { ts: number } = { ts: batch.ts }
      for (const pmu of selectedPMUs) {
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
  }, [alignedBatches, selectedPMUs, seriesSuffix, active.kind, active.key, analogName])

  const hasNumeric = data.some((row) =>
    chartSeries.some((series) => typeof row[series.dataKey] === 'number'),
  )
  const hasSeries = data.length > 0 && chartSeries.length > 0
  const isAnalog = active.kind === 'analog'

  const togglePMU = (name: string) => {
    setExcludedPMUNames((current) => {
      const isExcluded = current.includes(name)
      if (isExcluded) return current.filter((item) => item !== name)
      if (selectedPMUs.length === 1) return current
      return [...current, name]
    })
  }

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>{kind === 'voltage' ? 'Voltage Phasors' : 'Current Phasors'}</h3>
          <p className="panel-sub">
            {data.length
              ? isAnalog
                ? `${active.label} · CFG analog · ${selectedPMUs.length} of ${pmus.length} PMUs · time-aligned`
                : `${active.label} magnitude · ${selectedPMUs.length} of ${pmus.length} PMUs · time-aligned ticks`
              : `Waiting for aligned ${kind} ticks…`}
          </p>
        </div>
        <div className="phasor-chart-controls">
          <select
            value={activeKey}
            onChange={(event) => setSelectedKey(event.target.value)}
            aria-label={`Select ${kind} series`}
          >
            {seriesOptions.map((opt) => (
              <option key={opt.key} value={opt.key}>
                {opt.label}
              </option>
            ))}
          </select>
          <details className="pmu-multiselect">
            <summary>
              PMUs ({selectedPMUs.length}/{pmus.length})
            </summary>
            <div className="pmu-multiselect-menu">
              <button type="button" onClick={() => setExcludedPMUNames([])}>
                Select all
              </button>
              {pmus.map((pmu) => (
                <label key={pmu.name}>
                  <input
                    type="checkbox"
                    checked={!excludedPMUNames.includes(pmu.name)}
                    onChange={() => togglePMU(pmu.name)}
                  />
                  <span>{pmu.name}</span>
                </label>
              ))}
            </div>
          </details>
        </div>
      </div>
      <div className="chart-wrap small">
        {!hasSeries ? (
          <div className="frame-box-react" style={{ margin: 12 }}>
            Waiting for aligned {kind} phasor ticks…
          </div>
        ) : !hasNumeric && isAnalog ? (
          <div className="frame-box-react" style={{ margin: 12 }}>
            No values yet for CFG analog “{analogName}”. Waiting for aligned ticks…
          </div>
        ) : (
          <ResponsiveContainer width="99%" height={280} minWidth={1} minHeight={1}>
            <LineChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid stroke="rgba(158, 176, 197, 0.1)" strokeDasharray="3 3" />
              <XAxis
                dataKey="ts"
                tickFormatter={formatTS}
                tick={{ fill: '#8a9aab', fontSize: 10 }}
                interval="preserveStartEnd"
                minTickGap={28}
              />
              <YAxis
                tick={{ fill: '#8a9aab', fontSize: 11 }}
                domain={isAnalog ? ['auto', 'auto'] : [0, 'auto']}
                tickFormatter={(v) => `${round(Number(v), 1)}`}
                width={48}
              />
              <Tooltip
                {...CHART_TOOLTIP_STYLE}
                labelFormatter={(value) => formatTSMs(Number(value))}
                formatter={(value, name) => [
                  value == null || Number.isNaN(Number(value))
                    ? '—'
                    : `${round(Number(value), isAnalog ? 3 : 2)}${active.unit ? ` ${active.unit}` : ''}`,
                  String(name),
                ]}
              />
              <Legend />
              {chartSeries.map((series) => (
                <Line
                  key={series.dataKey}
                  type="monotone"
                  dataKey={series.dataKey}
                  name={series.name}
                  stroke={series.color}
                  dot={false}
                  strokeWidth={2}
                  isAnimationActive={false}
                  connectNulls={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  )
}
