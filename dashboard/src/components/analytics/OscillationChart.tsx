import {
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { OscillationMode } from '../../types/analytics'

type Props = {
  modes: OscillationMode[]
}

function dampingColor(damping: number) {
  if (damping < 5) return '#ff5d6c'
  if (damping < 10) return '#f4b740'
  return '#27d3a2'
}

export function OscillationChart({ modes }: Props) {
  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Oscillation Detection (Prony / Modal)</h3>
          <p className="panel-sub">Damping ratio vs mode frequency</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={1}>
          <ScatterChart>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis
              type="number"
              dataKey="freq"
              name="Frequency"
              unit=" Hz"
              domain={[0, 'auto']}
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              label={{ value: 'Frequency (Hz)', position: 'insideBottom', offset: -2, fill: '#8fa0c8' }}
            />
            <YAxis
              type="number"
              dataKey="damping"
              name="Damping"
              unit="%"
              domain={[0, 16]}
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              label={{ value: 'Damping ζ (%)', angle: -90, position: 'insideLeft', fill: '#8fa0c8' }}
            />
            <Tooltip
              cursor={{ strokeDasharray: '3 3' }}
              formatter={(value, _name, item) => {
                const payload = item.payload as OscillationMode
                return [Number(value ?? 0), payload.pmu]
              }}
              labelFormatter={(_, payload) => {
                const point = payload?.[0]?.payload as OscillationMode | undefined
                return point?.label ?? ''
              }}
            />
            <Scatter name="Detected modes" data={modes}>
              {modes.map((mode) => (
                <Cell key={mode.pmu} fill={dampingColor(mode.damping)} />
              ))}
            </Scatter>
          </ScatterChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
