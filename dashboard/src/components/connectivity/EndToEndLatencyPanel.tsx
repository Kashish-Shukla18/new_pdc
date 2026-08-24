import { memo } from 'react'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { CycleHistoryPoint } from '../../hooks/useCycleLatencyHistory'
import type { CycleLatency } from '../../utils/pmu'
import { formatTSMs, round } from '../../utils/format'
import { MultiStreamTooltip } from '../../utils/multiStreamTooltip'

type Props = {
  history: CycleHistoryPoint[]
  latest: CycleLatency | null
}

export const EndToEndLatencyPanel = memo(function EndToEndLatencyPanel({ history, latest }: Props) {
  const wait = latest?.pmuWait ?? 0
  const parse = latest?.parse ?? 0
  const rest = latest?.pipelineRest ?? 0
  const pipeline = latest?.pipeline ?? 0
  const total = latest?.total ?? 0
  const fps = latest?.fpsHint ? Math.round(latest.fpsHint) : '—'

  return (
    <section className="panel e2e-latency-panel">
      <div className="panel-head">
        <div>
          <h3>End-to-end cycle latency</h3>
          <p className="panel-sub">
            PMU inter-frame wait + parse + rest of pipeline · fleet average of online streams
          </p>
        </div>
        {latest && (
          <div className="e2e-total-badge">
            <span>Total cycle</span>
            <strong>{round(total, 2)} ms</strong>
            <em>~{fps} FPS equivalent wait</em>
          </div>
        )}
      </div>

      <div className="e2e-latency-layout">
        <div className="e2e-diagram" aria-label="Latency breakdown diagram">
          <div className="e2e-flow">
            <div className="e2e-block wait">
              <span className="e2e-block-label">PMU wait</span>
              <strong>{round(wait, 1)} ms</strong>
              <em>next frame (~20 ms @ 50 FPS)</em>
            </div>
            <span className="e2e-plus">+</span>
            <div className="e2e-block parse">
              <span className="e2e-block-label">Parse</span>
              <strong>{round(parse, 2)} ms</strong>
              <em>DATA decode</em>
            </div>
            <span className="e2e-plus">+</span>
            <div className="e2e-block rest">
              <span className="e2e-block-label">Other</span>
              <strong>{round(rest, 2)} ms</strong>
              <em>quality + dashboard</em>
            </div>
            <span className="e2e-eq">=</span>
            <div className="e2e-block total">
              <span className="e2e-block-label">E2E cycle</span>
              <strong>{round(total, 2)} ms</strong>
              <em>wait + pipeline</em>
            </div>
          </div>
          <p className="e2e-diagram-note">
            <strong>{round(wait, 1)} ms</strong> is the PMU frame interval (idle wait for the next sample),{' '}
            <strong>not</strong> PDC processing. Pipeline work is{' '}
            <strong>{round(pipeline, 2)} ms</strong> (parse {round(parse, 2)} ms + other {round(rest, 2)} ms).
          </p>
          <div className="e2e-bar" title="Relative sizes">
            <div className="e2e-bar-seg wait" style={{ flexGrow: Math.max(wait, 0.01) }}>
              wait
            </div>
            <div className="e2e-bar-seg parse" style={{ flexGrow: Math.max(parse, 0.01) }}>
              parse
            </div>
            <div className="e2e-bar-seg rest" style={{ flexGrow: Math.max(rest, 0.01) }}>
              other
            </div>
          </div>
        </div>

        <div className="e2e-chart-wrap">
          {!history.length ? (
            <div className="chart-empty">Waiting for hop samples… keep streams online.</div>
          ) : (
            <ResponsiveContainer width="99%" height={300} minWidth={1} minHeight={300}>
              <ComposedChart data={history} margin={{ top: 8, right: 12, left: 4, bottom: 0 }}>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis
                  dataKey="ts"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={(v) => formatTSMs(Number(v))}
                  tick={{ fill: '#9eb0c5', fontSize: 10 }}
                  minTickGap={36}
                />
                <YAxis
                  tick={{ fill: '#9eb0c5', fontSize: 11 }}
                  domain={[0, (max: number) => Math.max(25, Math.ceil((max || 1) * 1.1))]}
                  width={52}
                  tickFormatter={(v) => `${round(Number(v), 0)}`}
                  unit=" ms"
                />
                <Tooltip content={<MultiStreamTooltip tickLabel="Sample" valueSuffix=" ms" digits={2} />} />
                <Legend wrapperStyle={{ fontSize: 11 }} />
                <Area
                  type="monotone"
                  dataKey="pmuWait"
                  name="PMU wait (interval)"
                  stackId="cycle"
                  stroke="#f4b740"
                  fill="#f4b740"
                  fillOpacity={0.35}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="parse"
                  name="Parse"
                  stackId="cycle"
                  stroke="#4de0ff"
                  fill="#4de0ff"
                  fillOpacity={0.55}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="pipelineRest"
                  name="Other pipeline"
                  stackId="cycle"
                  stroke="#7dff93"
                  fill="#7dff93"
                  fillOpacity={0.45}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="total"
                  name="Total cycle"
                  stroke="#ff719a"
                  strokeWidth={2.25}
                  dot={false}
                  isAnimationActive={false}
                />
              </ComposedChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>
    </section>
  )
})
