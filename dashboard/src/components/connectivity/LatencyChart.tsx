import { memo, useMemo } from 'react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { RttHistoryPoint, RttStream } from '../../types/connectivity'
import { ExternalChartLegend, useSeriesVisibility } from '../../utils/chartLegend'
import {
  CHART_AXIS_TICK,
  CHART_GRID_STROKE,
  ChartSeriesTooltip,
} from '../../utils/chartTooltip'
import { formatTS, round } from '../../utils/format'

type Props = {
  history: RttHistoryPoint[]
  streams: RttStream[]
}

export const LatencyChart = memo(function LatencyChart({ history, streams }: Props) {
  const seriesKeys = useMemo(() => streams.map((s) => s.key), [streams])
  const { hidden, toggle, isHidden } = useSeriesVisibility(seriesKeys)

  const filled = useMemo(() => {
    if (!history.length || !streams.length) return history
    const last = new Map<string, number>()
    return history.map((point) => {
      const next: RttHistoryPoint = { ...point }
      for (const stream of streams) {
        const raw = point[stream.key]
        if (typeof raw === 'number' && Number.isFinite(raw)) {
          last.set(stream.key, raw)
          next[stream.key] = raw
        } else if (last.has(stream.key)) {
          next[stream.key] = last.get(stream.key)!
        }
      }
      return next
    })
  }, [history, streams])

  if (!streams.length) {
    return (
      <div className="panel chart-panel">
        <div className="panel-head">
          <div>
            <h3>Stream latency</h3>
            <p className="panel-sub">ms · frame received → dashboard · live samples</p>
          </div>
        </div>
        <div className="chart-empty">No connected streams for latency chart.</div>
      </div>
    )
  }

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Stream latency</h3>
          <p className="panel-sub">
            How long after a frame arrives at the PDC until the dashboard records it ·{' '}
            {streams.length} streams · {filled.length} samples (~1/s)
          </p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height="100%" minWidth={1} minHeight={1}>
          <LineChart data={filled} margin={{ top: 8, right: 12, left: 4, bottom: 4 }}>
            <CartesianGrid stroke={CHART_GRID_STROKE} strokeDasharray="3 3" />
            <XAxis
              dataKey="ts"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickFormatter={(v) => formatTS(Number(v))}
              tick={CHART_AXIS_TICK}
              minTickGap={36}
              interval="preserveStartEnd"
              stroke="#94a3b8"
            />
            <YAxis
              tick={CHART_AXIS_TICK}
              domain={[0, (max: number) => Math.max(2, Math.ceil((max || 1) * 1.15))]}
              width={56}
              tickFormatter={(value) => `${round(Number(value), 1)}`}
              unit=" ms"
              stroke="#94a3b8"
            />
            <Tooltip
              content={(props) => (
                <ChartSeriesTooltip
                  {...props}
                  labelFormatter={(value) => formatTS(Number(value))}
                  formatter={(value, name) => [`${round(Number(value ?? 0), 2)} ms`, String(name)]}
                />
              )}
            />
            {streams.map((stream) =>
              isHidden(stream.key) ? null : (
                <Line
                  key={stream.key}
                  type="monotone"
                  dataKey={stream.key}
                  name={stream.name}
                  stroke={stream.color}
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ),
            )}
          </LineChart>
        </ResponsiveContainer>
      </div>
      <ExternalChartLegend
        items={streams.map((s) => ({ key: s.key, name: s.name, color: s.color }))}
        hidden={hidden}
        onToggle={toggle}
      />
    </div>
  )
})
