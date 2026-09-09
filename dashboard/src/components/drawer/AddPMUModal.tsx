import { X } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'

export function AddPMUModal() {
  const {
    showAddPMU,
    setShowAddPMU,
    newPMU,
    setNewPMU,
    handleAddPMU,
    addSaving,
    addError,
  } = useDashboardContext()

  if (!showAddPMU) return null

  return (
    <div className="drawer-overlay" onClick={() => setShowAddPMU(false)}>
      <aside className="drawer-react" onClick={(event) => event.stopPropagation()} style={{ width: '400px' }}>
        <div className="drawer-head-react">
          <h3>Add PMU Connection</h3>
          <button type="button" aria-label="Close add PMU form" onClick={() => setShowAddPMU(false)}>
            <X size={18} />
          </button>
        </div>
        <div className="drawer-body-react">
          <form onSubmit={handleAddPMU} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>Name</label>
              <input
                required
                aria-label="PMU name"
                value={newPMU.name}
                onChange={(e) => setNewPMU({ ...newPMU, name: e.target.value })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>IP Address</label>
              <input
                required
                aria-label="IP address"
                value={newPMU.ip}
                onChange={(e) => setNewPMU({ ...newPMU, ip: e.target.value })}
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
            <div>
              <label style={{ display: 'block', marginBottom: '4px' }}>
                {newPMU.protocol === 'udp' ? 'UDP listen port' : 'Port'}
              </label>
              <input
                type="number"
                required
                value={newPMU.port}
                onChange={(e) => setNewPMU({ ...newPMU, port: parseInt(e.target.value, 10) || 0 })}
                style={{ width: '100%', padding: '8px' }}
              />
            </div>
            {newPMU.protocol === 'udp' && (
              <div>
                <label style={{ display: 'block', marginBottom: '4px' }}>TCP control port</label>
                <input
                  type="number"
                  value={newPMU.tcp_port ?? 0}
                  onChange={(e) =>
                    setNewPMU({ ...newPMU, tcp_port: parseInt(e.target.value, 10) || 0 })
                  }
                  style={{ width: '100%', padding: '8px' }}
                />
              </div>
            )}
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
            {addError && <p className="form-message error" role="alert">{addError}</p>}
            <button type="submit" className="btn primary" style={{ marginTop: '10px' }} disabled={addSaving}>
              {addSaving ? 'Connecting…' : 'Connect PMU'}
            </button>
          </form>
        </div>
      </aside>
    </div>
  )
}
