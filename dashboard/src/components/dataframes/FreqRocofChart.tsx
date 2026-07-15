import { memo } from 'react'
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
import type { TrendPoint } from '../../types/dashboard'
import { formatTS, round } from '../../utils/format'

type Props = {
  data: TrendPoint[]
}

export const FreqRocofChart = memo(function FreqRocofChart({ data }: Props) {
  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Frequency & ROCOF</h3>
          <p className="panel-sub">Hz / Hz·s⁻¹ · last 60 s rolling</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={320}>
          <LineChart data={data}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 9 }} interval={8} />
            <YAxis
              yAxisId="left"
              domain={['auto', 'auto']}
              tick={{ fill: '#4de0ff', fontSize: 9 }}
              tickFormatter={(v) => `${round(Number(v), 3)}`}
            />
            <YAxis
              yAxisId="right"
              orientation="right"
              domain={['auto', 'auto']}
              tick={{ fill: '#ffd166', fontSize: 9 }}
              tickFormatter={(v) => `${round(Number(v), 3)}`}
            />
            <Tooltip
              labelFormatter={(value) => formatTS(Number(value))}
              formatter={(value, name) => [
                name === 'frequency' ? `${round(Number(value ?? 0), 4)} Hz` : `${round(Number(value ?? 0), 4)} Hz/s`,
                name === 'frequency' ? 'Frequency' : 'ROCOF',
              ]}
            />
            <Legend wrapperStyle={{ fontSize: 10 }} />
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="frequency"
              name="Freq (Hz)"
              stroke="#4de0ff"
              dot={false}
              strokeWidth={1.5}
              isAnimationActive={false}
            />
            <Line
              yAxisId="right"
              type="monotone"
              dataKey="rocof"
              name="ROCOF (Hz/s)"
              stroke="#ffd166"
              dot={false}
              strokeWidth={1.5}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})
