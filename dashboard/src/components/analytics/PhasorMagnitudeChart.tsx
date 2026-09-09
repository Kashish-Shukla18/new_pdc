import { useState } from 'react'
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
import type { PMUWithMeta } from '../../types/dashboard'
import { CHART_TOOLTIP_STYLE } from '../../utils/chartTooltip'
import { formatTS, round } from '../../utils/format'
import { pmuKey } from '../../utils/pmu'

export const PHASOR_TREND_WINDOW = 90

type Props = {
  pmus: PMUWithMeta[]
  kind: 'voltage' | 'current'
}

const V_KEYS = [
  { key: 'va', label: 'VA' },
  { key: 'vb', label: 'VB' },
  { key: 'vc', label: 'VC' },
] as const

const I_KEYS = [
  { key: 'ia', label: 'IA' },
  { key: 'ib', label: 'IB' },
  { key: 'ic', label: 'IC' },
] as const

export function PhasorMagnitudeChart({ pmus, kind }: Props) {
  const phaseKeys = kind === 'voltage' ? V_KEYS : I_KEYS
  const [selectedPhaseKey, setSelectedPhaseKey] = useState<string>(
    kind === 'voltage' ? 'va' : 'ia',
  )
  const [excludedPMUNames, setExcludedPMUNames] = useState<string[]>([])
  const selectedPhase = phaseKeys.find((phase) => phase.key === selectedPhaseKey) ?? phaseKeys[0]
  const selectedPMUs = pmus.filter((pmu) => !excludedPMUNames.includes(pmu.name))
  const chartSeries = selectedPMUs.map((pmu) => ({
    dataKey: `${pmuKey(pmu.name)}__${selectedPhase.key}`,
    name: pmu.name,
    color: CHART_COLORS[pmus.findIndex((candidate) => candidate.name === pmu.name) % CHART_COLORS.length],
  }))
  const data = (() => {
    const rows = new Map<number, Record<string, number>>()
    for (const pmu of selectedPMUs) {
      for (const point of pmu.trends.slice(-PHASOR_TREND_WINDOW)) {
        const row = rows.get(point.ts) ?? { ts: point.ts }
        const value = point[selectedPhase.key]
        if (typeof value === 'number') {
          row[`${pmuKey(pmu.name)}__${selectedPhase.key}`] = value
        }
        rows.set(point.ts, row)
      }
    }
    return [...rows.values()]
      .sort((left, right) => left.ts - right.ts)
      .slice(-PHASOR_TREND_WINDOW)
  })()
  const hasSeries = data.some((point) =>
    chartSeries.some((series) => typeof point[series.dataKey] === 'number'),
  )
  const unit = kind === 'voltage' ? 'V' : 'A'

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
              ? `${selectedPhase.label} magnitude · ${selectedPMUs.length} of ${pmus.length} PMUs · PMU SOC/FRACSEC time`
              : `Waiting for ${kind} trend…`}
          </p>
        </div>
        <div className="phasor-chart-controls">
          <select
            value={selectedPhase.key}
            onChange={(event) => setSelectedPhaseKey(event.target.value)}
            aria-label={`Select ${kind} phasor`}
          >
            {phaseKeys.map((phase) => (
              <option key={phase.key} value={phase.key}>{phase.label}</option>
            ))}
          </select>
          <details className="pmu-multiselect">
            <summary>PMUs ({selectedPMUs.length}/{pmus.length})</summary>
            <div className="pmu-multiselect-menu">
              <button type="button" onClick={() => setExcludedPMUNames([])}>Select all</button>
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
        {!data.length || !hasSeries ? (
          <div className="frame-box-react" style={{ margin: 12 }}>
            Waiting for live {kind} phasor samples… Restart PDC if this stays empty.
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
                domain={[0, 'auto']}
                tickFormatter={(v) => `${round(Number(v), 1)}`}
                width={48}
              />
              <Tooltip
                {...CHART_TOOLTIP_STYLE}
                labelFormatter={(value) => `Matched timestamp: ${new Date(Number(value)).toISOString()}`}
                formatter={(value, name, item) => [
                  `${round(Number(value ?? 0), 3)} ${unit} · ${new Date(Number(item.payload.ts)).toISOString()}`,
                  String(name),
                ]}
              />
              <Legend wrapperStyle={{ color: '#8a9aab', fontSize: 11 }} />
              {chartSeries.map((series) => (
                <Line
                  key={series.dataKey}
                  type="linear"
                  dataKey={series.dataKey}
                  name={series.name}
                  stroke={series.color}
                  strokeWidth={1.75}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  )
}
