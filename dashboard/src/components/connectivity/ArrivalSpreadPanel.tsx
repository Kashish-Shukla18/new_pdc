import { memo, useMemo } from 'react'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { DynamicRelativeDelayPoint } from '../../hooks/useArrivalSpreadHistory'
import type { PMUWithMeta } from '../../types/dashboard'
import { ExternalChartLegend, useSeriesVisibility } from '../../utils/chartLegend'
import {
  CHART_AXIS_TICK,
  CHART_GRID_STROKE,
  ChartSeriesTooltip,
} from '../../utils/chartTooltip'
import { formatTS, round } from '../../utils/format'
import { chartLabel, type DynamicRelativeDelay } from '../../utils/pmu'

type Props = {
  history: DynamicRelativeDelayPoint[]
  latest: DynamicRelativeDelay | null
  pmus: PMUWithMeta[]
}

const SERIES = [
  { key: 'spreadMs', name: 'Recv spread (max−min)', color: '#b45309' },
  { key: 'parseAvgMs', name: 'Avg parse', color: '#2563eb' },
  { key: 'totalMs', name: 'Dynamic relative delay', color: '#b91c1c' },
]

function labelFor(name: string, pmus: PMUWithMeta[]) {
  const pmu = pmus.find((p) => p.name === name)
  return pmu ? chartLabel(pmu) : name
}

export const ArrivalSpreadPanel = memo(function ArrivalSpreadPanel({
  history,
  latest,
  pmus,
}: Props) {
  const seriesKeys = useMemo(() => SERIES.map((s) => s.key), [])
  const { hidden, toggle, isHidden } = useSeriesVisibility(seriesKeys)

  const spread = latest?.spreadMs ?? 0
  const parseAvg = latest?.parseAvgMs ?? 0
  const total = latest?.totalMs ?? 0
  const count = latest?.count ?? 0

  return (
    <section className="panel e2e-latency-panel">
      <div className="panel-head">
        <div>
          <h3>Dynamic relative delay</h3>
          <p className="panel-sub">
            (max − min TCP recv of latest frames) + average parse across those streams
          </p>
        </div>
        {latest && (
          <div className="e2e-total-badge arrival-spread-badge">
            <span>Dynamic relative delay</span>
            <strong>{round(total, 2)} ms</strong>
            <em>{count} online streams</em>
          </div>
        )}
      </div>

      <div className="e2e-latency-layout">
        <div className="e2e-diagram" aria-label="Dynamic relative delay summary">
          {!latest ? (
            <p className="e2e-diagram-note">Need at least two online streams with receive times.</p>
          ) : (
            <>
              <div className="e2e-flow arrival-spread-flow">
                <div className="e2e-block wait">
                  <span className="e2e-block-label">Recv spread</span>
                  <strong>{round(spread, 2)} ms</strong>
                  <em>
                    {labelFor(latest.earliestName, pmus)} → {labelFor(latest.latestName, pmus)}
                  </em>
                </div>
                <span className="e2e-plus">+</span>
                <div className="e2e-block parse">
                  <span className="e2e-block-label">Avg parse</span>
                  <strong>{round(parseAvg, 2)} ms</strong>
                  <em>mean of live streams</em>
                </div>
                <span className="e2e-eq">=</span>
                <div className="e2e-block total">
                  <span className="e2e-block-label">Delay</span>
                  <strong>{round(total, 2)} ms</strong>
                  <em>spread + parse</em>
                </div>
              </div>
              <p className="e2e-diagram-note">
                Arrival unevenness across the fleet, plus typical decode cost. Small is good.
              </p>
            </>
          )}
        </div>

        <div className="e2e-chart-wrap">
          {!history.length ? (
            <div className="chart-empty">Waiting for delay samples…</div>
          ) : (
            <>
              <ResponsiveContainer width="99%" height={260} minWidth={1} minHeight={260}>
                <ComposedChart data={history} margin={{ top: 8, right: 12, left: 4, bottom: 4 }}>
                  <CartesianGrid stroke={CHART_GRID_STROKE} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="ts"
                    type="number"
                    domain={['dataMin', 'dataMax']}
                    tickFormatter={(v) => formatTS(Number(v))}
                    tick={CHART_AXIS_TICK}
                    minTickGap={36}
                    stroke="#94a3b8"
                  />
                  <YAxis
                    tick={CHART_AXIS_TICK}
                    domain={[0, (max: number) => Math.max(5, Math.ceil((max || 1) * 1.15))]}
                    width={52}
                    tickFormatter={(v) => `${round(Number(v), 0)}`}
                    unit=" ms"
                    stroke="#94a3b8"
                  />
                  <Tooltip
                    content={(props) => (
                      <ChartSeriesTooltip
                        {...props}
                        labelFormatter={(v) => formatTS(Number(v))}
                        formatter={(value, name) => [
                          `${round(Number(value ?? 0), 2)} ms`,
                          String(name),
                        ]}
                      />
                    )}
                  />
                  {!isHidden('spreadMs') && (
                    <Area
                      type="monotone"
                      dataKey="spreadMs"
                      name="Recv spread (max−min)"
                      stackId="drd"
                      stroke="#b45309"
                      fill="#b45309"
                      fillOpacity={0.35}
                      isAnimationActive={false}
                    />
                  )}
                  {!isHidden('parseAvgMs') && (
                    <Area
                      type="monotone"
                      dataKey="parseAvgMs"
                      name="Avg parse"
                      stackId="drd"
                      stroke="#2563eb"
                      fill="#2563eb"
                      fillOpacity={0.45}
                      isAnimationActive={false}
                    />
                  )}
                  {!isHidden('totalMs') && (
                    <Line
                      type="monotone"
                      dataKey="totalMs"
                      name="Dynamic relative delay"
                      stroke="#b91c1c"
                      strokeWidth={2.25}
                      dot={false}
                      isAnimationActive={false}
                    />
                  )}
                </ComposedChart>
              </ResponsiveContainer>
              <ExternalChartLegend
                items={SERIES.map((s) => ({ key: s.key, name: s.name, color: s.color }))}
                hidden={hidden}
                onToggle={toggle}
                maxHeight={56}
              />
            </>
          )}
        </div>
      </div>
    </section>
  )
})
