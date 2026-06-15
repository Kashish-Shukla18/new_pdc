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
          <h3>Voltage Angle Differences</h3>
          <p className="panel-sub">Across major inter-regional corridors</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={1}>
          <LineChart data={history}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis
              dataKey="ts"
              tickFormatter={formatTS}
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
            />
            <YAxis
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              domain={[0, 'auto']}
              tickFormatter={(value) => `${value}°`}
            />
            <Tooltip
              labelFormatter={(value) => formatTS(Number(value))}
              formatter={(value) => [`${Number(value ?? 0).toFixed(2)}°`, '']}
            />
            <Legend />
            {coloredPairs.map((pair) => (
              <Line
                key={pair.key}
                type="monotone"
                dataKey={pair.key}
                name={pair.name}
                stroke={pair.color}
                strokeWidth={1.5}
                dot={false}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
      <div className="legend-row">
        {coloredPairs.map((pair) => (
          <span key={pair.key}>
            <i style={{ background: pair.color }} />
            {pair.name}
          </span>
        ))}
      </div>
    </div>
  )
}
