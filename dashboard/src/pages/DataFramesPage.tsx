import { FreqRocofChart } from '../components/dataframes/FreqRocofChart'
import { useDashboardContext } from '../context/DashboardContext'
import { round } from '../utils/format'

export function DataFramesPage() {
  const {
    pmus,
    selectedFramePMU,
    selectedFramePMUName,
    setSelectedFramePMUName,
    isPaused,
    setIsPaused,
    frameLines,
    phasorItems,
    frameTrendHistory,
  } = useDashboardContext()

  if (!selectedFramePMU) return null

  return (
    <>
      <section className="panel">
        <div className="panel-head">
          <div>
            <h3>Data Frame Information</h3>
            <p className="panel-sub">Live decoded synchrophasor frames — CFG-2, header, and data quantities</p>
          </div>
          <div className="panel-tools-inline">
            <select
              value={selectedFramePMUName}
              onChange={(event) => setSelectedFramePMUName(event.target.value)}
            >
              {pmus.map((pmu) => (
                <option key={pmu.name} value={pmu.name}>
                  {pmu.name}
                </option>
              ))}
            </select>
            <button type="button" className="btn" onClick={() => setIsPaused((current) => !current)}>
              {isPaused ? 'Resume stream' : 'Pause stream'}
            </button>
          </div>
        </div>

        <div className="main-grid dashboard-grid">
          <div className="panel frame-panel">
            <div className="panel-head"><h3>Configuration Frame (CFG-2)</h3></div>
            <div className="frame-box-react">
              <div>SYNC: 0xAA31</div>
              <div>IDCODE: {selectedFramePMU.name}</div>
              <div>STATION: {selectedFramePMU.meta.substation}</div>
              <div>FPS: {round(selectedFramePMU.approxFps, 2)}</div>
              <div>PHASORS: VA, VB, VC, IA</div>
              <div>ANALOG: MW, MVAR</div>
              <div>DIGITAL: quality/status flags</div>
            </div>
          </div>

          <div className="panel frame-panel">
            <div className="panel-head">
              <h3>Live Data Frame</h3>
              <p className="panel-sub"><span className="live-dot" /> streaming · SOC/FRACSEC live</p>
            </div>
            <div className="frame-box-react">
              {frameLines.length === 0 && <div>Waiting for live frames...</div>}
              {frameLines.map((line, idx) => (
                <div key={`${line}-${idx}`}>{line}</div>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section className="charts-grid compact-gap">
        <FreqRocofChart data={frameTrendHistory} />

        <article className="panel">
          <div className="panel-head">
            <h3>Phasor Quantities</h3>
            <p className="panel-sub">Magnitude and angle referenced to GPS time</p>
          </div>
          <div className="phasor-grid-react">
            {phasorItems.map((item) => (
              <article key={item.label} className="phasor-react-card">
                <p>{item.label}</p>
                <strong>{item.value ? round(item.value.magnitude, 2) : '--'}</strong>
                <span>{item.value ? `${round(item.value.angleDeg, 2)}°` : '--'}</span>
              </article>
            ))}
          </div>
        </article>
      </section>
    </>
  )
}
