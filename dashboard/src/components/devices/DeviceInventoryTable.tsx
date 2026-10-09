import type { PMUWithMeta } from '../../types/dashboard'
import { round } from '../../utils/format'
import {
  availabilityOf,
  effectiveFps,
  latencyOf,
  resolveTargetFps,
  statusLabel,
  toneFromStatus,
  packetLossOf,
} from '../../utils/devices'
import { displayName } from '../../utils/pmu'

type InventoryTab = 'connected' | 'disconnected'

type Props = {
  pmus: PMUWithMeta[]
  filteredDevices: PMUWithMeta[]
  deviceSearch: string
  setDeviceSearch: (value: string) => void
  regionFilter: string
  setRegionFilter: (value: string) => void
  statusFilter: string
  setStatusFilter: (value: string) => void
  inventoryTab: InventoryTab
  setInventoryTab: (value: InventoryTab) => void
  tabCounts: { connected: number; disconnected: number }
  onRowClick: (name: string) => void
  onEdit: (name: string) => void
  onDisconnect: (name: string, e: React.MouseEvent) => void
  onConnect: (name: string, e: React.MouseEvent) => void
  deletingPMUName: string
  connectingPMUName: string
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
  inventoryTab,
  setInventoryTab,
  tabCounts,
  onRowClick,
  onEdit,
  onDisconnect,
  onConnect,
  deletingPMUName,
  connectingPMUName,
}: Props) {
  const showDisconnected = inventoryTab === 'disconnected'

  return (
    <section className="panel device-table-panel">
      <div className="panel-head">
        <div>
          <h3>Registered PMUs</h3>
          <p className="panel-sub">
            {filteredDevices.length} {showDisconnected ? 'disconnected' : 'enabled'} · {pmus.length}{' '}
            registered · click a row for details
          </p>
        </div>
      </div>

      <div className="tabbar device-inventory-tabs" role="tablist" aria-label="Device connection state">
        <button
          type="button"
          role="tab"
          aria-selected={inventoryTab === 'connected'}
          className={`tab-button ${inventoryTab === 'connected' ? 'active' : ''}`}
          onClick={() => setInventoryTab('connected')}
        >
          Connected ({tabCounts.connected})
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={inventoryTab === 'disconnected'}
          className={`tab-button ${inventoryTab === 'disconnected' ? 'active' : ''}`}
          onClick={() => setInventoryTab('disconnected')}
        >
          Disconnected ({tabCounts.disconnected})
        </button>
      </div>

      <div className="device-filters">
        <input
          value={deviceSearch}
          onChange={(event) => setDeviceSearch(event.target.value)}
          aria-label="Search devices"
        />
        <select
          value={regionFilter}
          onChange={(event) => setRegionFilter(event.target.value)}
        >
          <option value="ALL">All Regions</option>
          {Array.from(new Set(pmus.map((pmu) => pmu.meta.region))).map((region) => (
            <option key={region} value={region}>
              {region}
            </option>
          ))}
        </select>
        {!showDisconnected && (
          <select
            value={statusFilter}
            onChange={(event) => setStatusFilter(event.target.value)}
          >
            <option value="ALL">All Status</option>
            <option value="OK">Healthy</option>
            <option value="WARN">Degraded</option>
            <option value="BAD">Offline</option>
          </select>
        )}
      </div>

      <div className="table-wrap scrollx rows-5">
        <table className="device-table">
          <thead>
            <tr>
              <th>Station</th>
              <th>Endpoint</th>
              <th>Region</th>
              <th>IP Address</th>
              <th>FPS</th>
              <th>Status</th>
              <th>Data Avail.</th>
              <th>Latency</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {filteredDevices.length === 0 ? (
              <tr>
                <td colSpan={9} className="device-empty">
                  {showDisconnected
                    ? 'No disconnected PMUs. Disconnect an enabled stream to park it here.'
                    : 'No enabled PMUs in this filter. Check Disconnected or register a new device.'}
                </td>
              </tr>
            ) : (
              filteredDevices.map((pmu) => {
                const loss = packetLossOf(pmu)
                const tone = showDisconnected ? 'bad' : toneFromStatus(pmu.connected, loss)
                const label = showDisconnected ? 'Disconnected' : statusLabel(pmu)
                const fps = effectiveFps(pmu)
                const target = resolveTargetFps(pmu, pmu.meta.targetFps)
                return (
                  <tr key={pmu.name} className="row-click" onClick={() => onRowClick(pmu.name)}>
                    <td>
                      <strong className="device-id">{displayName(pmu)}</strong>
                    </td>
                    <td>
                      <code className="mono-ip">{pmu.name}</code>
                    </td>
                    <td>{pmu.meta.region}</td>
                    <td><code className="mono-ip">{pmu.meta.primaryIp}</code></td>
                    <td>
                      {showDisconnected ? (
                        <span className="device-muted">—</span>
                      ) : (
                        <span className={fps >= target * 0.7 ? 'fps-ok' : 'fps-warn'}>
                          {Math.round(fps)}
                          <span className="device-muted"> / {Math.round(target)}</span>
                        </span>
                      )}
                    </td>
                    <td>
                      <span className={`status-chip ${tone}`}>{label}</span>
                    </td>
                    <td>{!showDisconnected && pmu.connected ? `${round(availabilityOf(pmu, pmu.meta.targetFps), 1)}%` : '—'}</td>
                    <td>{!showDisconnected && pmu.connected ? `${round(latencyOf(pmu), 0)} ms` : '—'}</td>
                    <td>
                      <div className="device-row-actions">
                        <button
                          type="button"
                          className="btn ghost device-edit-btn"
                          onClick={(e) => {
                            e.stopPropagation()
                            onEdit(pmu.name)
                          }}
                        >
                          Edit
                        </button>
                        {showDisconnected ? (
                          <button
                            type="button"
                            className="btn primary device-connect"
                            disabled={connectingPMUName === pmu.name}
                            onClick={(e) => onConnect(pmu.name, e)}
                          >
                            {connectingPMUName === pmu.name ? 'Connecting…' : 'Connect'}
                          </button>
                        ) : (
                          <button
                            type="button"
                            className="btn ghost device-disconnect"
                            disabled={deletingPMUName === pmu.name}
                            onClick={(e) => onDisconnect(pmu.name, e)}
                          >
                            {deletingPMUName === pmu.name ? 'Disconnecting…' : 'Disconnect'}
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
