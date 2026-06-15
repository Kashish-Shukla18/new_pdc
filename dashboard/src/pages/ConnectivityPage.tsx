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
import { useDashboardContext } from '../context/DashboardContext'
import { ageText, formatTS, round } from '../utils/format'
import { pmuKey } from '../utils/pmu'

export function ConnectivityPage() {
  const { connectivityRows, mergedTrend, pmus, setDrawerPMUName } = useDashboardContext()

  const kpiItems = [
    {
      label: 'Active issues',
      value: connectivityRows.filter((row) => row.tone !== 'ok').length,
      sub: 'across simulator streams',
    },
    {
      label: 'Offline streams',
      value: connectivityRows.filter((row) => !row.connected).length,
      sub: 'no fresh frames',
    },
    {
      label: 'High loss (>1%)',
      value: connectivityRows.filter((row) => row.loss > 1).length,
      sub: 'quality rejects ratio',
    },
    {
      label: 'Avg latency',
      value: `${round(
        connectivityRows.reduce((sum, row) => sum + row.latency, 0) / Math.max(1, connectivityRows.length),
        1,
      )} ms`,
      sub: 'derived from fps + rocof drift',
    },
  ]

  return (
    <>
      <section className="kpi-grid">
        {kpiItems.map((item) => (
          <article key={item.label} className="kpi-card neutral">
            <p className="kpi-meta">{item.label}</p>
            <h2 className="kpi-value">{item.value}</h2>
            <p className="kpi-meta">{item.sub}</p>
          </article>
        ))}
      </section>

      <section className="main-grid dashboard-grid">
        <div className="panel">
          <div className="panel-head"><h3>Connectivity Recommendations</h3></div>
          <div className="rec-list-react">
            {connectivityRows.map((row) => (
              <article key={row.name} className={`rec-react ${row.tone}`}>
                <strong>{row.name}</strong>
                <p>{row.recommendation}</p>
              </article>
            ))}
          </div>
        </div>

        <div className="panel chart-panel">
          <div className="panel-head"><h3>Latency Trend by PMU</h3></div>
          <div className="chart-wrap small">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={mergedTrend}>
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis dataKey="ts" tickFormatter={formatTS} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <YAxis tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <Tooltip labelFormatter={(value) => formatTS(Number(value))} />
                <Legend />
                {pmus.map((pmu, idx) => (
                  <Line
                    key={`${pmu.name}-latency`}
                    type="monotone"
                    dataKey={`${pmuKey(pmu.name)}__rocof`}
                    name={pmu.name}
                    stroke={['#ff7b7b', '#ffd166', '#4de0ff'][idx % 3]}
                    dot={false}
                    strokeWidth={2}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>
      </section>

      <section className="panel table-panel">
        <div className="panel-head"><h3>Per-PMU Connectivity Matrix</h3></div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>PMU</th>
                <th>Region</th>
                <th>Latency (ms)</th>
                <th>Jitter (ms)</th>
                <th>Loss %</th>
                <th>Avail %</th>
                <th>Last frame</th>
                <th>Status</th>
                <th>Recommendation</th>
              </tr>
            </thead>
            <tbody>
              {connectivityRows.map((row) => (
                <tr key={row.name} onClick={() => setDrawerPMUName(row.name)}>
                  <td><strong>{row.name}</strong></td>
                  <td>{row.meta.region}</td>
                  <td>{round(row.latency, 1)}</td>
                  <td>{round(row.jitter, 2)}</td>
                  <td>{round(row.loss, 3)}</td>
                  <td>{round(row.avail, 1)}</td>
                  <td>{ageText(row.lastFrameTime)}</td>
                  <td><span className={`status-chip ${row.tone}`}>{row.tone}</span></td>
                  <td>{row.recommendation}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
