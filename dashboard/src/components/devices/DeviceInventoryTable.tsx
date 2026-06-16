import type { PMUWithMeta } from '../../types/dashboard'
import { round } from '../../utils/format'
import {
  availabilityOf,
  latencyOf,
  statusLabel,
  toneFromStatus,
  vendorFor,
  voltageClassFor,
  packetLossOf,
} from '../../utils/devices'

type Props = {
  pmus: PMUWithMeta[]
  filteredDevices: PMUWithMeta[]
  deviceSearch: string
  setDeviceSearch: (value: string) => void
  regionFilter: string
  setRegionFilter: (value: string) => void
  statusFilter: string
  setStatusFilter: (value: string) => void
  onRowClick: (name: string) => void
  onDelete: (name: string, e: React.MouseEvent) => void
}

export function DeviceInventoryTable({
  pmus,
  filteredDevices,
  deviceSearch,
  setDeviceSearch,
  regionFilter,
  setRegionFilter,
  statusFilter,
  setStatusFilter,
  onRowClick,
  onDelete,
}: Props) {
  return (
    <section className="panel device-table-panel">
      <div className="panel-head">
        <div>
          <h3>Registered PMUs</h3>
          <p className="panel-sub">
            {filteredDevices.length} of {pmus.length} devices shown · click a row for full detail
          </p>
        </div>
      </div>

      <div className="device-filters">
        <input
          value={deviceSearch}
          onChange={(event) => setDeviceSearch(event.target.value)}
          placeholder="Search substation, ID, IP, vendor…"
        />
        <select value={regionFilter} onChange={(event) => setRegionFilter(event.target.value)}>
          <option value="ALL">All Regions</option>
          {Array.from(new Set(pmus.map((pmu) => pmu.meta.region))).map((region) => (
            <option key={region} value={region}>
              {region}
            </option>
          ))}
        </select>
        <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
          <option value="ALL">All Status</option>
          <option value="OK">Healthy</option>
          <option value="WARN">Degraded</option>
          <option value="BAD">Offline</option>
        </select>
      </div>

      <div className="table-wrap scrollx">
        <table className="device-table">
          <thead>
            <tr>
              <th>PMU ID</th>
              <th>Substation</th>
              <th>Region</th>
              <th>State</th>
              <th>Voltage</th>
              <th>Vendor / Model</th>
              <th>IP Address</th>
              <th>FPS</th>
              <th>Status</th>
              <th>Data Avail.</th>
              <th>Latency</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {filteredDevices.map((pmu) => {
              const loss = packetLossOf(pmu)
              const tone = toneFromStatus(pmu.connected, loss)
              const label = statusLabel(pmu)
              return (
                <tr key={pmu.name} className="row-click" onClick={() => onRowClick(pmu.name)}>
                  <td>
                    <strong className="device-id">{pmu.name}</strong>
                  </td>
                  <td>
                    <span className="device-primary">{pmu.meta.substation}</span>
                    <span className="device-muted">{pmu.meta.region} corridor</span>
                  </td>
                  <td>{pmu.meta.region}</td>
                  <td>{pmu.meta.state}</td>
                  <td><span className="voltage-tag">{voltageClassFor(pmu)}</span></td>
                  <td>
                    <span className="device-primary">{vendorFor(pmu)}</span>
                    <span className="device-muted">RES670</span>
                  </td>
                  <td><code className="mono-ip">{pmu.meta.primaryIp}</code></td>
                  <td>
                    <span className={pmu.approxFps >= pmu.meta.targetFps * 0.9 ? 'fps-ok' : 'fps-warn'}>
                      {round(pmu.approxFps, 1)}
                    </span>
                  </td>
                  <td>
                    <span className={`status-chip ${tone}`}>{label}</span>
                  </td>
                  <td>{pmu.connected ? `${round(availabilityOf(pmu, pmu.meta.targetFps), 1)}%` : '—'}</td>
                  <td>{pmu.connected ? `${round(latencyOf(pmu), 0)} ms` : '—'}</td>
                  <td>
                    <button
                      type="button"
                      className="btn ghost device-disconnect"
                      onClick={(e) => onDelete(pmu.name, e)}
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
    </section>
  )
}
