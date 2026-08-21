import { Pencil, X } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'
import { ageText, round } from '../../utils/format'

export function PMUDetailDrawer() {
  const { drawerPMU, setDrawerPMUName, openEditPMU } = useDashboardContext()

  if (!drawerPMU) return null

  return (
    <div className="drawer-overlay" onClick={() => setDrawerPMUName('')}>
      <aside className="drawer-react" onClick={(event) => event.stopPropagation()}>
        <div className="drawer-head-react">
          <h3>{drawerPMU.name}</h3>
          <button type="button" onClick={() => setDrawerPMUName('')}>
            <X size={18} />
          </button>
        </div>
        <div className="drawer-body-react">
          <p><strong>Substation:</strong> {drawerPMU.meta.substation}, {drawerPMU.meta.state}</p>
          <p><strong>Region:</strong> {drawerPMU.meta.region}</p>
          <p><strong>Voltage:</strong> {drawerPMU.meta.voltage}</p>
          <p><strong>Vendor:</strong> {drawerPMU.meta.vendor}</p>
          <p><strong>Primary IP:</strong> {drawerPMU.meta.primaryIp}</p>
          <p><strong>Redundant IP:</strong> {drawerPMU.meta.redundantIp}</p>
          <p><strong>Frames:</strong> {drawerPMU.totalFrames}</p>
          <p><strong>Approx FPS:</strong> {Math.round(drawerPMU.approxFps)}</p>
          <p><strong>Last event:</strong> {ageText(drawerPMU.lastEventTime)}</p>
          {drawerPMU.lastHops && Object.keys(drawerPMU.lastHops).length > 0 && (
            <div className="drawer-hops">
              <p><strong>Last hop times</strong></p>
              {Object.entries(drawerPMU.lastHops)
                .sort((a, b) => b[1] - a[1])
                .map(([stage, ms]) => (
                  <p key={stage} className="drawer-hop-row">
                    <span>{stage}</span>
                    <span className="mono">{round(ms, ms < 1 ? 2 : 1)} ms</span>
                  </p>
                ))}
            </div>
          )}
          {drawerPMU.lastError && <p><strong>Last error:</strong> {drawerPMU.lastError}</p>}
          <button
            type="button"
            className="btn primary"
            style={{ marginTop: '16px' }}
            onClick={() => openEditPMU(drawerPMU.name)}
          >
            <Pencil size={14} /> Edit device
          </button>
        </div>
      </aside>
    </div>
  )
}
