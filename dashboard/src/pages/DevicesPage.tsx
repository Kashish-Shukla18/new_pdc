import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useDashboardContext } from '../context/DashboardContext'
import { round } from '../utils/format'
import { availabilityOf, packetLossOf, toneFromStatus } from '../utils/pmu'

export function DevicesPage() {
  const {
    pmus,
    filteredDevices,
    deviceSearch,
    setDeviceSearch,
    regionFilter,
    setRegionFilter,
    statusFilter,
    setStatusFilter,
    setShowAddPMU,
    setDrawerPMUName,
    handleDeletePMU,
  } = useDashboardContext()

  const pieData = [
    { name: 'Healthy', value: pmus.filter((pmu) => pmu.connected).length, color: '#5de8aa' },
    { name: 'Offline', value: pmus.filter((pmu) => !pmu.connected).length, color: '#f0706a' },
  ]

  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h3>Device Inventory</h3>
          <p>Manage and monitor PMU connections.</p>
        </div>
        <div className="panel-tools-inline">
          <button type="button" className="btn primary" onClick={() => setShowAddPMU(true)}>
            + Add PMU
          </button>
        </div>
      </div>

      <div className="device-filter-row">
        <input
          value={deviceSearch}
          onChange={(event) => setDeviceSearch(event.target.value)}
          placeholder="Search PMU / substation / IP"
        />
        <select value={regionFilter} onChange={(event) => setRegionFilter(event.target.value)}>
          <option value="ALL">All regions</option>
          {Array.from(new Set(pmus.map((pmu) => pmu.meta.region))).map((region) => (
            <option key={region} value={region}>
              {region}
            </option>
          ))}
        </select>
        <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
          <option value="ALL">All status</option>
          <option value="OK">Healthy</option>
          <option value="WARN">Degraded</option>
          <option value="BAD">Offline</option>
        </select>
      </div>

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>PMU</th>
              <th>Substation</th>
              <th>Region</th>
              <th>Vendor</th>
              <th>IP</th>
              <th>FPS</th>
              <th>Status</th>
              <th>Availability</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {filteredDevices.map((pmu) => {
              const loss = packetLossOf(pmu)
              const tone = toneFromStatus(pmu.connected, loss)
              return (
                <tr key={pmu.name} onClick={() => setDrawerPMUName(pmu.name)}>
                  <td><strong>{pmu.name}</strong></td>
                  <td>{pmu.meta.substation}</td>
                  <td>{pmu.meta.region}</td>
                  <td>{pmu.meta.vendor}</td>
                  <td>{pmu.meta.primaryIp}</td>
                  <td>{round(pmu.approxFps, 1)}</td>
                  <td>
                    <span className={`status-chip ${tone}`}>{pmu.connected ? 'Healthy' : 'Offline'}</span>
                  </td>
                  <td>{round(availabilityOf(pmu, pmu.meta.targetFps), 1)}%</td>
                  <td>
                    <button
                      type="button"
                      className="btn ghost"
                      style={{ color: 'var(--err)', padding: '2px 6px' }}
                      onClick={(e) => handleDeletePMU(pmu.name, e)}
                    >
                      Disconnect
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="charts-grid compact-gap">
        <article className="panel chart-panel">
          <div className="panel-head"><h3>Availability by PMU</h3></div>
          <div className="chart-wrap small">
            <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
              <BarChart
                data={pmus.map((pmu) => ({
                  name: pmu.name,
                  avail: round(availabilityOf(pmu, pmu.meta.targetFps), 2),
                }))}
              >
                <CartesianGrid stroke="rgba(255,255,255,0.08)" strokeDasharray="3 3" />
                <XAxis dataKey="name" tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <YAxis domain={[0, 100]} tick={{ fill: '#9eb0c5', fontSize: 11 }} />
                <Tooltip />
                <Bar dataKey="avail" radius={[8, 8, 0, 0]}>
                  {pmus.map((pmu) => (
                    <Cell key={pmu.name} fill={pmu.connected ? '#5de8aa' : '#f0706a'} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </article>

        <article className="panel chart-panel">
          <div className="panel-head"><h3>Status Distribution</h3></div>
          <div className="chart-wrap small">
            <ResponsiveContainer width="99%" height={360} minWidth={1} minHeight={1}>
              <PieChart>
                <Pie data={pieData} dataKey="value" nameKey="name" outerRadius={90}>
                  {pieData.map((entry) => (
                    <Cell key={entry.name} fill={entry.color} />
                  ))}
                </Pie>
                <Tooltip />
                <Legend />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </article>
      </div>
    </section>
  )
}
