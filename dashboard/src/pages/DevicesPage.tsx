import { Upload } from 'lucide-react'
import { useMemo } from 'react'
import { DeviceDistributionCharts } from '../components/devices/DeviceDistributionCharts'
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
            Registered field PMUs — connection details, vendor, region, IP, and live operational status
          </p>
        </div>
        <div className="page-actions">
          <button type="button" className="btn ghost">
            <Upload size={14} /> Import CSV
          </button>
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
      />

      <DeviceDistributionCharts pmus={pmus} />
    </>
  )
}
