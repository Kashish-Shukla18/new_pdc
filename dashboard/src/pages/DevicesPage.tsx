import { useMemo } from 'react'
import { BufferCapacityControl } from '../components/devices/BufferCapacityControl'
import { DeviceInventoryTable } from '../components/devices/DeviceInventoryTable'
import { DeviceSummaryGrid } from '../components/devices/DeviceSummaryGrid'
import { useDashboardContext } from '../context/DashboardContext'
import { computeDeviceSummary } from '../utils/devices'
import { EditDevicePage } from './EditDevicePage'

export function DevicesPage() {
  const {
    dashboard,
    pmus,
    enabledPmus,
    filteredDevices,
    deviceSearch,
    setDeviceSearch,
    regionFilter,
    setRegionFilter,
    statusFilter,
    setStatusFilter,
    setShowAddPMU,
    setDrawerPMUName,
    handleDisconnectPMU,
    handleConnectPMU,
    deletingPMUName,
    connectingPMUName,
    deviceInventoryTab,
    setDeviceInventoryTab,
    deviceTabCounts,
    editingPMUName,
    openEditPMU,
    setNotice,
  } = useDashboardContext()

  const summary = useMemo(() => computeDeviceSummary(enabledPmus), [enabledPmus])
  const liveCapacity = dashboard.aligner?.maxOpen

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
        <div className="page-actions devices-page-actions">
          <BufferCapacityControl
            liveCapacity={liveCapacity}
            onApplied={(capacity) =>
              setNotice({ type: 'success', message: `Aligner buffer capacity set to ${capacity} samples/PMU` })
            }
            onError={(message) => setNotice({ type: 'error', message })}
          />
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
        inventoryTab={deviceInventoryTab}
        setInventoryTab={setDeviceInventoryTab}
        tabCounts={deviceTabCounts}
        onRowClick={setDrawerPMUName}
        onEdit={openEditPMU}
        onDisconnect={handleDisconnectPMU}
        onConnect={handleConnectPMU}
        deletingPMUName={deletingPMUName}
        connectingPMUName={connectingPMUName}
      />

    </>
  )
}
