import { ArrowLeft, Save } from 'lucide-react'
import { useDashboardContext } from '../context/DashboardContext'

export function EditDevicePage() {
  const {
    editingPMU,
    editPMU,
    setEditPMU,
    closeEditPMU,
    handleUpdatePMU,
    updateSaving,
    updateError,
  } = useDashboardContext()

  if (!editingPMU || !editPMU) return null

  return (
    <>
      <div className="devices-page-head">
        <div>
          <button type="button" className="btn ghost device-edit-back" onClick={closeEditPMU}>
            <ArrowLeft size={14} /> Back to inventory
          </button>
          <h2 className="devices-title">Edit Device — {editingPMU.name}</h2>
          <p className="devices-subtitle">
            Update connection settings for this PMU. Changes are saved to the registry and the receiver is restarted.
          </p>
        </div>
      </div>

      <section className="panel device-edit-panel">
        <div className="panel-head">
          <div>
            <h3>Device configuration</h3>
            <p className="panel-sub">Device name cannot be changed — register a new PMU to use a different ID.</p>
          </div>
        </div>

        <form onSubmit={handleUpdatePMU} className="device-edit-form">
          <div className="form-grid">
            <label className="field field-wide">
              <span>PMU name</span>
              <input value={editPMU.name} readOnly disabled />
            </label>

            <label className="field">
              <span>IP address</span>
              <input
                required
                value={editPMU.ip}
                onChange={(e) => setEditPMU({ ...editPMU, ip: e.target.value })}
                placeholder="172.24.108.1"
              />
            </label>

            <label className="field">
              <span>Port</span>
              <input
                type="number"
                required
                min={1}
                max={65535}
                value={editPMU.port}
                onChange={(e) => setEditPMU({ ...editPMU, port: parseInt(e.target.value, 10) || 0 })}
              />
            </label>

            <label className="field">
              <span>ID code</span>
              <input
                type="number"
                required
                min={0}
                max={65535}
                value={editPMU.idcode}
                onChange={(e) => setEditPMU({ ...editPMU, idcode: parseInt(e.target.value, 10) || 0 })}
              />
            </label>

            <label className="field">
              <span>Region</span>
              <input
                required
                value={editPMU.region}
                onChange={(e) => setEditPMU({ ...editPMU, region: e.target.value })}
                placeholder="NRLDC"
              />
            </label>

            <label className="field">
              <span>Protocol</span>
              <select
                value={editPMU.protocol}
                onChange={(e) => setEditPMU({ ...editPMU, protocol: e.target.value })}
              >
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </label>

            <label className="field">
              <span>Timeout (seconds)</span>
              <input
                type="number"
                min={0}
                value={editPMU.timeout_sec ?? 0}
                onChange={(e) =>
                  setEditPMU({ ...editPMU, timeout_sec: parseInt(e.target.value, 10) || 0 })
                }
              />
            </label>

            <label className="field">
              <span>Reconnect interval (seconds)</span>
              <input
                type="number"
                min={0}
                value={editPMU.reconnect_sec ?? 0}
                onChange={(e) =>
                  setEditPMU({ ...editPMU, reconnect_sec: parseInt(e.target.value, 10) || 0 })
                }
              />
            </label>

            <label className="field">
              <span>Latitude</span>
              <input
                type="number"
                step="any"
                required
                value={editPMU.lat}
                onChange={(e) => setEditPMU({ ...editPMU, lat: parseFloat(e.target.value) || 0 })}
              />
            </label>

            <label className="field">
              <span>Longitude</span>
              <input
                type="number"
                step="any"
                required
                value={editPMU.lon}
                onChange={(e) => setEditPMU({ ...editPMU, lon: parseFloat(e.target.value) || 0 })}
              />
            </label>
          </div>

          {updateError && <p className="device-edit-error">{updateError}</p>}

          <div className="register-actions">
            <p>Live status: {editingPMU.connected ? 'Connected' : 'Offline'} · {editingPMU.totalFrames} frames received</p>
            <div className="device-edit-actions">
              <button type="button" className="btn ghost" onClick={closeEditPMU}>
                Cancel
              </button>
              <button type="submit" className="btn primary" disabled={updateSaving}>
                <Save size={14} /> {updateSaving ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          </div>
        </form>
      </section>
    </>
  )
}
