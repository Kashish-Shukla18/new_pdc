import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useDashboardContext } from '../context/DashboardContext'
import { round } from '../utils/format'

export function AnalyticsPage() {
  const { pmus, anglePairs, analyticsRecs } = useDashboardContext()

  const maxAngle = round(Math.max(0, ...anglePairs.map((item) => item.value)), 2)
  const avgFrequency = round(
    pmus.reduce((sum, pmu) => sum + (pmu.lastReading?.frequency ?? 0), 0) / Math.max(1, pmus.length),
    4,
  )

  return (
    <>
      <section className="kpi-grid">
        <article className="kpi-card warn">
          <p className="kpi-meta">Max angle delta</p>
          <h2 className="kpi-value">{maxAngle}°</h2>
        </article>
        <article className="kpi-card ok">
          <p className="kpi-meta">Avg frequency</p>
          <h2 className="kpi-value">{avgFrequency} Hz</h2>
        </article>
        <article className="kpi-card neutral">
          <p className="kpi-meta">Advisories</p>
          <h2 className="kpi-value">{analyticsRecs.length}</h2>
        </article>
      </section>

      <section className="main-grid dashboard-grid">
        <div className="panel chart-panel">
          <div className="panel-head"><h3>Voltage Angle Differences</h3></div>
          <div className="chart-wrap small">
            <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
              <BarChart data={anglePairs}>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <Tooltip />
                <Bar dataKey="value" fill="#8b6cf5" radius={[8, 8, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="panel chart-panel">
          <div className="panel-head"><h3>Oscillation Detection</h3></div>
          <div className="chart-wrap small">
            <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
              <ScatterChart>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis
                  type="number"
                  dataKey="freq"
                  name="freq"
                  unit="Hz"
                  tick={{ fill: '#9eb0c5', fontSize: 11 }}
                />
                <YAxis
                  type="number"
                  dataKey="damping"
                  name="damping"
                  unit="%"
                  tick={{ fill: '#9eb0c5', fontSize: 11 }}
                />
                <Tooltip cursor={{ strokeDasharray: '3 3' }} />
                <Scatter
                  name="Modes"
                  data={pmus.map((pmu) => ({
                    freq: Math.abs(pmu.lastReading?.rocof ?? 0) * 10 + 0.2,
                    damping: Math.max(2, 14 - Math.abs(pmu.lastReading?.rocof ?? 0) * 80),
                    pmu: pmu.name,
                  }))}
                  fill="#4de0ff"
                />
              </ScatterChart>
            </ResponsiveContainer>
          </div>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h3>Operator Recommendations</h3></div>
        <div className="rec-list-react">
          {analyticsRecs.map((rec, idx) => (
            <article key={`${rec.title}-${idx}`} className={`rec-react ${rec.sev}`}>
              <strong>{rec.title}</strong>
              <p>{rec.desc}</p>
            </article>
          ))}
        </div>
      </section>
    </>
  )
}
