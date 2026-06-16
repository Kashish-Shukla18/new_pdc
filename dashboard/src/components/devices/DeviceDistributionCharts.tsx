import { memo, useMemo } from 'react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  PolarAngleAxis,
  PolarGrid,
  PolarRadiusAxis,
  Radar,
  RadarChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { PMUWithMeta } from '../../types/dashboard'
import { regionChartData, vendorChartData, voltageChartData } from '../../utils/devices'

type Props = {
  pmus: PMUWithMeta[]
}

export const DeviceDistributionCharts = memo(function DeviceDistributionCharts({ pmus }: Props) {
  const regions = useMemo(() => regionChartData(pmus), [pmus])
  const voltages = useMemo(() => voltageChartData(pmus), [pmus])
  const vendors = useMemo(() => vendorChartData(pmus), [pmus])

  return (
    <section className="device-charts-grid">
      <article className="panel chart-panel">
        <div className="panel-head">
          <div>
            <h3>By Region</h3>
            <p className="panel-sub">PMU count per RLDC</p>
          </div>
        </div>
        <div className="chart-wrap small">
          <ResponsiveContainer width="99%" height={280} minWidth={1} minHeight={280}>
            <PieChart>
              <Pie
                data={regions}
                dataKey="value"
                nameKey="name"
                innerRadius={52}
                outerRadius={88}
                paddingAngle={2}
                isAnimationActive={false}
              >
                {regions.map((entry) => (
                  <Cell key={entry.name} fill={entry.color} stroke="transparent" />
                ))}
              </Pie>
              <Tooltip formatter={(value, name) => [`${value} PMUs`, name]} />
              <Legend wrapperStyle={{ fontSize: 10 }} />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </article>

      <article className="panel chart-panel">
        <div className="panel-head">
          <div>
            <h3>By Voltage Class</h3>
            <p className="panel-sub">Substation voltage level</p>
          </div>
        </div>
        <div className="chart-wrap small">
          <ResponsiveContainer width="99%" height={280} minWidth={1} minHeight={280}>
            <BarChart data={voltages}>
              <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" vertical={false} />
              <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 10 }} />
              <YAxis allowDecimals={false} tick={{ fill: '#9eb0c5', fontSize: 10 }} />
              <Tooltip formatter={(value) => [`${value} PMUs`, 'Count']} />
              <Bar dataKey="count" radius={[6, 6, 0, 0]} isAnimationActive={false}>
                {voltages.map((entry) => (
                  <Cell key={entry.name} fill={entry.color} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </article>

      <article className="panel chart-panel">
        <div className="panel-head">
          <div>
            <h3>By Vendor</h3>
            <p className="panel-sub">Equipment manufacturer mix</p>
          </div>
        </div>
        <div className="chart-wrap small">
          <ResponsiveContainer width="99%" height={280} minWidth={1} minHeight={280}>
            <RadarChart data={vendors} outerRadius="72%">
              <PolarGrid stroke="rgba(255,255,255,0.1)" />
              <PolarAngleAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 9 }} />
              <PolarRadiusAxis tick={false} axisLine={false} />
              <Radar
                name="PMUs"
                dataKey="count"
                stroke="#4dc8f0"
                fill="rgba(77, 200, 240, 0.35)"
                strokeWidth={1.5}
                isAnimationActive={false}
              />
              <Tooltip formatter={(value) => [`${value} PMUs`, 'Count']} />
            </RadarChart>
          </ResponsiveContainer>
        </div>
      </article>
    </section>
  )
})
