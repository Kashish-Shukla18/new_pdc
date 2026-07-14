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
import type { AngleHistoryPoint, AnglePair } from '../../types/analytics'
import { anglePairColors } from '../../utils/analytics'
import { CHART_TOOLTIP_STYLE } from '../../utils/chartTooltip'
import { formatTS } from '../../utils/format'

type Props = {
  history: AngleHistoryPoint[]
  pairs: AnglePair[]
}

export function AngleDiffChart({ history, pairs }: Props) {
  const coloredPairs = anglePairColors(pairs)

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Inter-PMU Angle Δ (VA)</h3>
          <p className="panel-sub">
            {pairs.length
              ? 'From live VA phase angles · needs ≥2 PMUs'
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
                tickFormatter={formatTS}
                tick={{ fill: '#8a9aab', fontSize: 11 }}
              />
              <YAxis
                tick={{ fill: '#8a9aab', fontSize: 11 }}
                domain={[0, 'auto']}
                tickFormatter={(value) => `${value}°`}
              />
              <Tooltip
                {...CHART_TOOLTIP_STYLE}
                labelFormatter={(value) => formatTS(Number(value))}
                formatter={(value) => [`${Number(value ?? 0).toFixed(2)}°`, '']}
              />
              <Legend wrapperStyle={{ color: '#8a9aab', fontSize: 11 }} />
              {coloredPairs.map((pair) => (
                <Line
                  key={pair.key}
                  type="monotone"
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
    </div>
  )
}
