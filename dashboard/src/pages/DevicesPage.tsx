import { useMemo } from 'react'
import { DeviceInventoryTable } from '../components/devices/DeviceInventoryTable'
import { DeviceSummaryGrid } from '../components/devices/DeviceSummaryGrid'
import { useDashboardContext } from '../context/DashboardContext'
import { computeDeviceSummary } from '../utils/devices'
import { EditDevicePage } from './EditDevicePage'

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
    deletingPMUName,
    editingPMUName,
    openEditPMU,
  } = useDashboardContext()

  const summary = useMemo(() => computeDeviceSummary(pmus), [pmus])

  if (editingPMUName) {
    return <EditDevicePage />
  }

  return (
    <>
      <div className="devices-page-head">
        <div>
          <h2 className="devices-title">Device Inventory & Locations</h2>
          <p className="devices-subtitle">
            Registered PMUs — connection details, region, IP, and live operational status
          </p>
        </div>
        <div className="page-actions">
          <button type="button" className="btn primary" onClick={() => setShowAddPMU(true)}>
            + Register New PMU
          </button>
        </div>
      </div>

      <DeviceSummaryGrid items={summary} />

      <DeviceInventoryTable
        pmus={pmus}
        filteredDevices={filteredDevices}
        deviceSearch={deviceSearch}
        setDeviceSearch={setDeviceSearch}
        regionFilter={regionFilter}
        setRegionFilter={setRegionFilter}
        statusFilter={statusFilter}
        setStatusFilter={setStatusFilter}
        onRowClick={setDrawerPMUName}
        onEdit={openEditPMU}
        onDelete={handleDeletePMU}
        deletingPMUName={deletingPMUName}
      />

    </>
  )
}
