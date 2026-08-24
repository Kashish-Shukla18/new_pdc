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
import { formatTSMs, round } from '../../utils/format'
import { MultiStreamTooltip } from '../../utils/multiStreamTooltip'

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
            <XAxis dataKey="ts" tickFormatter={formatTSMs} tick={{ fill: '#9eb0c5', fontSize: 9 }} interval={8} />
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
              content={
                <MultiStreamTooltip
                  tickLabel="Frame ts"
                  digits={4}
                  unitForName={(name) =>
                    String(name).toLowerCase().includes('rocof') ? ' Hz/s' : ' Hz'
                  }
                />
              }
            />
            <Legend wrapperStyle={{ fontSize: 10 }} />
            <Line
              yAxisId="left"
              type="linear"
              dataKey="frequency"
              name="Freq (Hz)"
              stroke="#4de0ff"
              dot={false}
              strokeWidth={1.75}
              isAnimationActive={false}
              connectNulls
            />
            <Line
              yAxisId="right"
              type="linear"
              dataKey="rocof"
              name="ROCOF (Hz/s)"
              stroke="#ffd166"
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
