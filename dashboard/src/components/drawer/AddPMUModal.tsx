import { X } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'

export function AddPMUModal() {
  const { showAddPMU, setShowAddPMU, newPMU, setNewPMU, handleAddPMU } = useDashboardContext()

  if (!showAddPMU) return null

  return (
    <div className="drawer-overlay" onClick={() => setShowAddPMU(false)}>
      <aside className="drawer-react" onClick={(event) => event.stopPropagation()} style={{ width: '400px' }}>
        <div className="drawer-head-react">
          <h3>Add PMU Connection</h3>
          <button type="button" onClick={() => setShowAddPMU(false)}>
            <X size={18} />
          </button>
        </div>
        <div className="drawer-body-react">
          <form onSubmit={handleAddPMU} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Name</label>
              <input
                required
                value={newPMU.name}
                onChange={(e) => setNewPMU({ ...newPMU, name: e.target.value })}
                style={{ width: '100%', padding: '8px' }}
                placeholder="PMU-1"
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>IP Address</label>
              <input
                required
                value={newPMU.ip}
                onChange={(e) => setNewPMU({ ...newPMU, ip: e.target.value })}
                style={{ width: '100%', padding: '8px' }}
                placeholder="127.0.0.1"
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Port</label>
              <input
                type="number"
                required
                value={newPMU.port}
                onChange={(e) => setNewPMU({ ...newPMU, port: parseInt(e.target.value) })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>ID Code</label>
              <input
                type="number"
                required
                value={newPMU.idcode}
                onChange={(e) => setNewPMU({ ...newPMU, idcode: parseInt(e.target.value) })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Region</label>
              <input
                value={newPMU.region}
                onChange={(e) => setNewPMU({ ...newPMU, region: e.target.value })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Latitude</label>
              <input
                type="number"
                step="any"
                required
                value={newPMU.lat}
                onChange={(e) => setNewPMU({ ...newPMU, lat: parseFloat(e.target.value) || 0 })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Longitude</label>
              <input
                type="number"
                step="any"
                required
                value={newPMU.lon}
                onChange={(e) => setNewPMU({ ...newPMU, lon: parseFloat(e.target.value) || 0 })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Protocol</label>
              <select
                value={newPMU.protocol}
                onChange={(e) => setNewPMU({ ...newPMU, protocol: e.target.value })}
                style={{ width: '100%', padding: '8px' }}
              >
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </div>
            <button type="submit" className="btn primary" style={{ marginTop: '10px' }}>
              Connect PMU
            </button>
          </form>
        </div>
      </aside>
    </div>
  )
}
