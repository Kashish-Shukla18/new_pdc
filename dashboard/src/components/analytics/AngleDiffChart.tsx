import { useMemo } from 'react'
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
import { AlignedStreamReadout } from '../charts/AlignedStreamReadout'
import { CHART_COLORS } from '../../constants'
import type { AngleHistoryPoint, AnglePair } from '../../types/analytics'
import type { PMUWithMeta } from '../../types/dashboard'
import { anglePairColors } from '../../utils/analytics'
import { formatTSMs, round } from '../../utils/format'
import { MultiStreamTooltip } from '../../utils/multiStreamTooltip'
import { pmuKey } from '../../utils/pmu'

type Props = {
  history: AngleHistoryPoint[]
  pairs: AnglePair[]
  /** Online PMUs used to show live VA + frame timestamps for alignment checks. */
  pmus?: PMUWithMeta[]
  alignHint?: string
}

export function AngleDiffChart({ history, pairs, pmus = [], alignHint }: Props) {
  const coloredPairs = anglePairColors(pairs)

  const streamRows = useMemo(() => {
    const keysInPairs = new Set<string>()
    for (const pair of pairs) {
      for (const part of pair.key.split('__')) {
        if (part) keysInPairs.add(part)
      }
    }
    const list = pmus.filter((p) => keysInPairs.has(pmuKey(p.name)))
    const fallback = list.length ? list : pmus.filter((p) => p.connected).slice(0, 6)
    return fallback.map((pmu, idx) => {
      const colorIdx = pmus.findIndex((p) => p.name === pmu.name)
      return {
        name: pmu.name,
        color: CHART_COLORS[(colorIdx >= 0 ? colorIdx : idx) % CHART_COLORS.length],
        value: `${round(pmu.lastPhasor?.va?.angleDeg ?? 0, 2)}° VA`,
        chartTs: pmu.lastPhasor?.ts ?? pmu.lastReading?.ts ?? 0,
        wireSoc: pmu.lastFrame?.soc,
        wireFracCount: pmu.lastFrame?.fracSecCount,
      }
    })
  }, [pmus, pairs])

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Inter-PMU Angle Δ (VA)</h3>
          <p className="panel-sub">
            {pairs.length
              ? 'Time-aligned VA phase angles · compare Aligned ts rows below'
              : 'Add a second online PMU to compare angles'}
          </p>
        </div>
      </div>
      <div className="chart-wrap small">
        {!pairs.length ? (
          <div className="frame-box-react" style={{ margin: 12 }}>
            No inter-PMU pairs yet — angle Δ is only defined between two streams.
          </div>
        ) : (
          <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={1}>
            <LineChart data={history}>
              <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
              <XAxis
                dataKey="ts"
                tickFormatter={formatTSMs}
                tick={{ fill: '#8a9aab', fontSize: 11 }}
              />
              <YAxis
                tick={{ fill: '#8a9aab', fontSize: 11 }}
                domain={[0, 'auto']}
                tickFormatter={(value) => `${value}°`}
              />
              <Tooltip content={<MultiStreamTooltip tickLabel="Aligned tick" valueSuffix="°" digits={2} />} />
              <Legend wrapperStyle={{ color: '#8a9aab', fontSize: 11 }} />
              {coloredPairs.map((pair) => (
                <Line
                  key={pair.key}
                  type="linear"
                  dataKey={pair.key}
                  name={pair.name}
                  stroke={pair.color}
                  strokeWidth={1.6}
                  strokeOpacity={0.9}
                  dot={false}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
      {!!coloredPairs.length && (
        <div className="legend-row">
          {coloredPairs.map((pair) => (
            <span key={pair.key}>
              <i style={{ background: pair.color }} />
              {pair.name}
            </span>
          ))}
        </div>
      )}
      <AlignedStreamReadout rows={streamRows} alignHint={alignHint} />
    </div>
  )
}
