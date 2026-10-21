import { useEffect, useMemo, useState } from 'react'
import type { PMUWithMeta } from '../../types/dashboard'
import { round } from '../../utils/format'
import {
  availabilityOf,
  displayFps,
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
  onBulkDisconnect: (names: string[]) => void | Promise<void>
  onBulkConnect: (names: string[]) => void | Promise<void>
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
  onBulkDisconnect,
  onBulkConnect,
  deletingPMUName,
  connectingPMUName,
}: Props) {
  const showDisconnected = inventoryTab === 'disconnected'
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const bulkBusy = deletingPMUName === '__bulk__' || connectingPMUName === '__bulk__'

  const visibleNames = useMemo(
    () => filteredDevices.map((pmu) => pmu.name),
    [filteredDevices],
  )

  useEffect(() => {
    setSelected(new Set())
  }, [inventoryTab, deviceSearch, regionFilter, statusFilter])

  useEffect(() => {
    const visible = new Set(visibleNames)
    setSelected((current) => {
      let changed = false
      const next = new Set<string>()
      current.forEach((name) => {
        if (visible.has(name)) next.add(name)
        else changed = true
      })
      return changed ? next : current
    })
  }, [visibleNames])

  const allVisibleSelected =
    visibleNames.length > 0 && visibleNames.every((name) => selected.has(name))
  const someVisibleSelected = visibleNames.some((name) => selected.has(name))
  const selectedNames = useMemo(
    () => visibleNames.filter((name) => selected.has(name)),
    [visibleNames, selected],
  )

  const toggleOne = (name: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const toggleAllVisible = () => {
    setSelected((current) => {
      if (allVisibleSelected) {
        const next = new Set(current)
        visibleNames.forEach((name) => next.delete(name))
        return next
      }
      const next = new Set(current)
      visibleNames.forEach((name) => next.add(name))
      return next
    })
  }

  const runBulk = async () => {
    if (!selectedNames.length || bulkBusy) return
    if (showDisconnected) await onBulkConnect(selectedNames)
    else await onBulkDisconnect(selectedNames)
    setSelected(new Set())
  }

  return (
    <section className="panel device-table-panel">
      <div className="panel-head">
        <div>
          <h3>Registered PMUs</h3>
          <p className="panel-sub">
            {filteredDevices.length} {showDisconnected ? 'disconnected' : 'enabled'} · {pmus.length}{' '}
            registered · click a row for details · checkbox to multi-select
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

      {selectedNames.length > 0 && (
        <div className="device-bulk-bar" role="region" aria-label="Bulk device actions">
          <span className="device-bulk-count">
            {selectedNames.length} selected
          </span>
          <button
            type="button"
            className="btn ghost"
            disabled={bulkBusy}
            onClick={() => setSelected(new Set())}
          >
            Clear
          </button>
          {showDisconnected ? (
            <button
              type="button"
              className="btn primary"
              disabled={bulkBusy}
              onClick={() => void runBulk()}
            >
              {bulkBusy ? 'Connecting…' : `Connect ${selectedNames.length}`}
            </button>
          ) : (
            <button
              type="button"
              className="btn ghost device-disconnect"
              disabled={bulkBusy}
              onClick={() => void runBulk()}
            >
              {bulkBusy ? 'Disconnecting…' : `Disconnect ${selectedNames.length}`}
            </button>
          )}
        </div>
      )}

      <div className="table-wrap scrollx rows-5">
        <table className="device-table">
          <thead>
            <tr>
              <th className="device-select-col">
                <input
                  type="checkbox"
                  checked={allVisibleSelected}
                  ref={(el) => {
                    if (el) el.indeterminate = someVisibleSelected && !allVisibleSelected
                  }}
                  disabled={!visibleNames.length || bulkBusy}
                  onChange={toggleAllVisible}
                  aria-label="Select all visible devices"
                />
              </th>
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
                <td colSpan={10} className="device-empty">
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
                const fps = displayFps(pmu, pmu.meta.targetFps)
                const target = resolveTargetFps(pmu, pmu.meta.targetFps)
                const isSelected = selected.has(pmu.name)
                const rowBusy =
                  deletingPMUName === pmu.name ||
                  connectingPMUName === pmu.name ||
                  bulkBusy
                return (
                  <tr
                    key={pmu.name}
                    className={`row-click${isSelected ? ' device-row-selected' : ''}`}
                    onClick={() => onRowClick(pmu.name)}
                  >
                    <td className="device-select-col" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={isSelected}
                        disabled={bulkBusy}
                        onChange={() => toggleOne(pmu.name)}
                        aria-label={`Select ${displayName(pmu)}`}
                      />
                    </td>
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
                        <span className={fps >= target * 0.95 ? 'fps-ok' : 'fps-warn'}>
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
                            disabled={rowBusy}
                            onClick={(e) => onConnect(pmu.name, e)}
                          >
                            {connectingPMUName === pmu.name || bulkBusy ? 'Connecting…' : 'Connect'}
                          </button>
                        ) : (
                          <button
                            type="button"
                            className="btn ghost device-disconnect"
                            disabled={rowBusy}
                            onClick={(e) => onDisconnect(pmu.name, e)}
                          >
                            {deletingPMUName === pmu.name || bulkBusy ? 'Disconnecting…' : 'Disconnect'}
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
