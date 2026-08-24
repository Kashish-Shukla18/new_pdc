import { memo, useMemo } from 'react'
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
import { formatTSMs, round } from '../../utils/format'
import { MultiStreamTooltip } from '../../utils/multiStreamTooltip'

type Props = {
  history: RttHistoryPoint[]
  streams: RttStream[]
}

export const LatencyChart = memo(function LatencyChart({ history, streams }: Props) {
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
            How long after a frame arrives at the PDC until the dashboard records it · top{' '}
            {streams.length} streams · {filled.length} samples (~1/s)
          </p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={340} minWidth={1} minHeight={340}>
          <LineChart data={filled} margin={{ top: 8, right: 12, left: 4, bottom: 0 }}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis
              dataKey="ts"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickFormatter={(v) => formatTSMs(Number(v))}
              tick={{ fill: '#9eb0c5', fontSize: 10 }}
              minTickGap={36}
              interval="preserveStartEnd"
            />
            <YAxis
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              domain={[0, (max: number) => Math.max(2, Math.ceil((max || 1) * 1.15))]}
              width={56}
              tickFormatter={(value) => `${round(Number(value), 1)}`}
              unit=" ms"
            />
            <Tooltip content={<MultiStreamTooltip tickLabel="Sample" valueSuffix=" ms" digits={2} />} />
            <Legend wrapperStyle={{ fontSize: 11 }} />
            {streams.map((stream) => (
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
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})
