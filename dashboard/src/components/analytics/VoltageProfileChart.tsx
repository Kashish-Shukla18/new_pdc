import { Bar, BarChart, CartesianGrid, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { VoltageBus } from '../../types/analytics'

type Props = {
  buses: VoltageBus[]
}

function barColor(tone: VoltageBus['tone']) {
  if (tone === 'bad') return '#ff5d6c'
  if (tone === 'warn') return '#f4b740'
  return '#27d3a2'
}

export function VoltageProfileChart({ buses }: Props) {
  return (
    <div className="panel chart-panel">
      <div className="panel-head">
        <div>
          <h3>Voltage Profile (Bus pu)</h3>
          <p className="panel-sub">Positive-sequence magnitude</p>
        </div>
      </div>
      <div className="chart-wrap small">
        <ResponsiveContainer width="99%" height={320} minWidth={1} minHeight={1}>
          <BarChart data={buses}>
            <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
            <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 10 }} />
            <YAxis
              domain={[0.92, 1.06]}
              tick={{ fill: '#9eb0c5', fontSize: 11 }}
              tickFormatter={(value) => `${value.toFixed(2)} pu`}
            />
            <Tooltip formatter={(value) => [`${Number(value ?? 0).toFixed(3)} pu`, 'Voltage']} />
            <Bar dataKey="pu" radius={[6, 6, 0, 0]}>
              {buses.map((bus) => (
                <Cell key={bus.name} fill={barColor(bus.tone)} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
