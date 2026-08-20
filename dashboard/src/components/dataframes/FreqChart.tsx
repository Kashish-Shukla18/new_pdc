import { memo } from 'react'
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { TrendPoint } from '../../types/dashboard'
import { formatTS, round } from '../../utils/format'

type Props = {
  data: TrendPoint[]
  fnomHz?: number
}

export const FreqChart = memo(function FreqChart({ data, fnomHz = 60 }: Props) {
  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Frequency</h3>
          <p className="panel-sub">Hz · live trend samples · FNOM {fnomHz} Hz</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={320}>
          <LineChart data={data}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 9 }} interval={8} />
            <YAxis
              domain={['auto', 'auto']}
              tick={{ fill: '#4de0ff', fontSize: 9 }}
              tickFormatter={(v) => `${round(Number(v), 3)}`}
              width={56}
            />
            <Tooltip
              labelFormatter={(value) => formatTS(Number(value))}
              formatter={(value) => [`${round(Number(value ?? 0), 4)} Hz`, 'Frequency']}
            />
            <Legend wrapperStyle={{ fontSize: 10 }} />
            <ReferenceLine y={fnomHz} stroke="#7dd3a7" strokeDasharray="4 4" label={`FNOM ${fnomHz}`} />
            <Line
              type="linear"
              dataKey="frequency"
              name="Freq (Hz)"
              stroke="#4de0ff"
              dot={false}
              strokeWidth={1.75}
              isAnimationActive={false}
              connectNulls
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})
