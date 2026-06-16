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
import type { RttHistoryPoint, RttStream } from '../../types/connectivity'
import { round } from '../../utils/format'

type Props = {
  history: RttHistoryPoint[]
  streams: RttStream[]
}

export const RttChart = memo(function RttChart({ history, streams }: Props) {
  if (!streams.length) {
    return (
      <div className="panel chart-panel">
        <div className="panel-head">
          <div>
            <h3>Round-Trip Time — Top 6 Streams</h3>
            <p className="panel-sub">ms · last 5 min rolling</p>
          </div>
        </div>
        <div className="chart-empty">No connected streams available for RTT chart.</div>
      </div>
    )
  }

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Round-Trip Time — Top 6 Streams</h3>
          <p className="panel-sub">ms · last 5 min rolling</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={320}>
          <LineChart data={history}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis
              dataKey="label"
              tick={{ fill: '#9eb0c5', fontSize: 9 }}
              interval={4}
            />
            <YAxis
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              domain={['auto', 'auto']}
              tickFormatter={(value) => `${value} ms`}
            />
            <Tooltip formatter={(value) => [`${round(Number(value ?? 0), 1)} ms`, '']} />
            <Legend wrapperStyle={{ fontSize: 10 }} />
            {streams.map((stream) => (
              <Line
                key={stream.key}
                type="monotone"
                dataKey={stream.key}
                name={stream.name}
                stroke={stream.color}
                strokeWidth={1.4}
                dot={false}
                isAnimationActive={false}
                connectNulls
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})
