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
import { PHASOR_I_COLORS, PHASOR_V_COLORS } from '../../utils/analyticsColors'
import { formatTSMs, round } from '../../utils/format'
import { MultiStreamTooltip } from '../../utils/multiStreamTooltip'

export const PHASOR_TREND_WINDOW = 90

type Props = {
  trends: TrendPoint[]
  kind: 'voltage' | 'current'
  pmuName: string
}

const V_KEYS = [
  { key: 'va', label: 'VA', color: PHASOR_V_COLORS.VA },
  { key: 'vb', label: 'VB', color: PHASOR_V_COLORS.VB },
  { key: 'vc', label: 'VC', color: PHASOR_V_COLORS.VC },
] as const

const I_KEYS = [
  { key: 'ia', label: 'IA', color: PHASOR_I_COLORS.IA },
  { key: 'ib', label: 'IB', color: PHASOR_I_COLORS.IB },
  { key: 'ic', label: 'IC', color: PHASOR_I_COLORS.IC },
] as const

export function PhasorMagnitudeChart({ trends, kind, pmuName }: Props) {
  const series = kind === 'voltage' ? V_KEYS : I_KEYS
  const data = trends.slice(-PHASOR_TREND_WINDOW)
  const hasSeries = data.some((point) =>
    series.some((s) => typeof point[s.key] === 'number'),
  )

  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>{kind === 'voltage' ? 'Voltage Phasors' : 'Current Phasors'}</h3>
          <p className="panel-sub">
            {data.length
              ? `${pmuName} · magnitude · last ${Math.min(data.length, PHASOR_TREND_WINDOW)} samples`
              : `Waiting for ${kind} trend…`}
          </p>
        </div>
      </div>
      <div className="chart-wrap small">
        {!data.length || !hasSeries ? (
          <div className="frame-box-react" style={{ margin: 12 }}>
            Waiting for live {kind} phasor samples… Restart PDC if this stays empty.
          </div>
        ) : (
          <ResponsiveContainer width="99%" height={280} minWidth={1} minHeight={1}>
            <LineChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid stroke="rgba(158, 176, 197, 0.1)" strokeDasharray="3 3" />
              <XAxis
                dataKey="ts"
                tickFormatter={formatTSMs}
                tick={{ fill: '#8a9aab', fontSize: 10 }}
                interval="preserveStartEnd"
                minTickGap={28}
              />
              <YAxis
                tick={{ fill: '#8a9aab', fontSize: 11 }}
                domain={[0, 'auto']}
                tickFormatter={(v) => `${round(Number(v), 1)}`}
                width={48}
              />
              <Tooltip content={<MultiStreamTooltip tickLabel="Frame ts" valueSuffix="" digits={3} />} />
              <Legend wrapperStyle={{ color: '#8a9aab', fontSize: 11 }} />
              {series.map((s) => (
                <Line
                  key={s.key}
                  type="linear"
                  dataKey={s.key}
                  name={s.label}
                  stroke={s.color}
                  strokeWidth={1.75}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  )
}
