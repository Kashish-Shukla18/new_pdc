import { memo } from 'react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { TrendPoint } from '../../types/dashboard'
import {
  CHART_AXIS_TICK,
  CHART_GRID_STROKE,
  ChartSeriesTooltip,
} from '../../utils/chartTooltip'
import { formatTS, formatTSMs, round } from '../../utils/format'

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
          <p className="panel-sub">Hz · time-aligned ticks · FNOM {fnomHz} Hz</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height="100%" minWidth={1} minHeight={1}>
          <LineChart data={data} margin={{ top: 8, right: 12, left: 4, bottom: 4 }}>
            <CartesianGrid stroke={CHART_GRID_STROKE} strokeDasharray="3 3" />
            <XAxis
              dataKey="ts"
              tickFormatter={formatTS}
              tick={CHART_AXIS_TICK}
              interval={8}
              stroke="#94a3b8"
            />
            <YAxis
              domain={['auto', 'auto']}
              tick={CHART_AXIS_TICK}
              tickFormatter={(v) => `${round(Number(v), 3)}`}
              width={56}
              stroke="#94a3b8"
            />
            <Tooltip
              content={(props) => (
                <ChartSeriesTooltip
                  {...props}
                  labelFormatter={(value) => formatTSMs(Number(value))}
                  formatter={(value, name) => [
                    `${round(Number(value ?? 0), 4)} Hz`,
                    String(name ?? 'Freq (Hz)'),
                  ]}
                />
              )}
            />
            <ReferenceLine y={fnomHz} stroke="#15803d" strokeDasharray="4 4" label={`FNOM ${fnomHz}`} />
            <Line
              type="linear"
              dataKey="frequency"
              name="Freq (Hz)"
              stroke="#2563eb"
              dot={false}
              strokeWidth={1.75}
              isAnimationActive={false}
              connectNulls={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})
