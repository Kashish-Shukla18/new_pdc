import { useState } from 'react'
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
  onEdit: (name: string) => void
  onDelete: (name: string, e: React.MouseEvent) => void
  deletingPMUName: string
}

const PAGE_SIZE = 20

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
  onEdit,
  onDelete,
  deletingPMUName,
}: Props) {
  const [page, setPage] = useState(0)
  const pageCount = Math.max(1, Math.ceil(filteredDevices.length / PAGE_SIZE))
  const currentPage = Math.min(page, pageCount - 1)
  const pageStart = currentPage * PAGE_SIZE
  const visibleDevices = filteredDevices.slice(pageStart, pageStart + PAGE_SIZE)

  return (
    <section className="panel device-table-panel">
      <div className="panel-head">
        <div>
          <h3>Registered PMUs</h3>
          <p className="panel-sub">
            {filteredDevices.length
              ? `${pageStart + 1}–${Math.min(pageStart + PAGE_SIZE, filteredDevices.length)} of ${filteredDevices.length}`
              : '0'} filtered devices · {pmus.length} registered · click a row for details
          </p>
        </div>
      </div>

      <div className="device-filters">
        <input
          value={deviceSearch}
          onChange={(event) => {
            setPage(0)
            setDeviceSearch(event.target.value)
          }}
          aria-label="Search devices"
        />
        <select
          value={regionFilter}
          onChange={(event) => {
            setPage(0)
            setRegionFilter(event.target.value)
          }}
        >
          <option value="ALL">All Regions</option>
          {Array.from(new Set(pmus.map((pmu) => pmu.meta.region))).map((region) => (
            <option key={region} value={region}>
              {region}
            </option>
          ))}
        </select>
        <select
          value={statusFilter}
          onChange={(event) => {
            setPage(0)
            setStatusFilter(event.target.value)
          }}
        >
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
              <th>IP Address</th>
              <th>FPS</th>
              <th>Status</th>
              <th>Data Avail.</th>
              <th>Latency</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {visibleDevices.map((pmu) => {
              const loss = packetLossOf(pmu)
              const tone = toneFromStatus(pmu.connected, loss)
              const label = statusLabel(pmu)
              const fps = effectiveFps(pmu)
              const target = resolveTargetFps(pmu, pmu.meta.targetFps)
              return (
                <tr key={pmu.name} className="row-click" onClick={() => onRowClick(pmu.name)}>
                  <td>
                    <strong className="device-id">{pmu.name}</strong>
                  </td>
                  <td>
                    <span className="device-primary">{pmu.meta.substation}</span>
                  </td>
                  <td>{pmu.meta.region}</td>
                  <td><code className="mono-ip">{pmu.meta.primaryIp}</code></td>
                  <td>
                    <span className={fps >= target * 0.7 ? 'fps-ok' : 'fps-warn'}>
                      {Math.round(fps)}
                      <span className="device-muted"> / {Math.round(target)}</span>
                    </span>
                  </td>
                  <td>
                    <span className={`status-chip ${tone}`}>{label}</span>
                  </td>
                  <td>{pmu.connected ? `${round(availabilityOf(pmu, pmu.meta.targetFps), 1)}%` : '—'}</td>
                  <td>{pmu.connected ? `${round(latencyOf(pmu), 0)} ms` : '—'}</td>
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
                      <button
                        type="button"
                        className="btn ghost device-disconnect"
                        disabled={deletingPMUName === pmu.name}
                        onClick={(e) => onDelete(pmu.name, e)}
                      >
                        {deletingPMUName === pmu.name ? 'Removing…' : 'Disconnect'}
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {pageCount > 1 && (
        <div className="device-pagination" aria-label="Device table pagination">
          <button
            type="button"
            className="btn ghost"
            disabled={currentPage === 0}
            onClick={() => setPage((value) => Math.max(0, value - 1))}
          >
            Previous
          </button>
          <span>Page {currentPage + 1} of {pageCount}</span>
          <button
            type="button"
            className="btn ghost"
            disabled={currentPage >= pageCount - 1}
            onClick={() => setPage((value) => Math.min(pageCount - 1, value + 1))}
          >
            Next
          </button>
        </div>
      )}
    </section>
  )
}
