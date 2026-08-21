import { FreqChart } from '../components/dataframes/FreqChart'
import { useDashboardContext } from '../context/DashboardContext'
import { round } from '../utils/format'
import { displayPhasors, formatPhasorCfgList } from '../utils/phasorLabels'

function hexWord(n: number | undefined, width = 4) {
  return `0x${((n ?? 0) >>> 0).toString(16).toUpperCase().padStart(width, '0')}`
}

function formatList(items: string[] | undefined, fallback: string) {
  if (!items || items.length === 0) return fallback
  return items.join(', ')
}

function statFlags(detail: {
  dataError?: boolean
  data_error?: boolean
  cfgChange?: boolean
  cfg_change?: boolean
  triggerDetected?: boolean
  trigger_detected?: boolean
  pmuSyncStatus?: boolean
  pmu_sync_status?: boolean
} | undefined) {
  if (!detail) return []
  const flags: string[] = []
  if (detail.dataError || detail.data_error) flags.push('DATA_ERR')
  if (detail.cfgChange || detail.cfg_change) flags.push('CFG_CHG')
  if (detail.triggerDetected || detail.trigger_detected) flags.push('TRIG')
  if (detail.pmuSyncStatus || detail.pmu_sync_status) flags.push('UNLOCKED')
  return flags
}

export function DataFramesPage() {
  const {
    pmus,
    selectedFramePMU,
    selectedFramePMUName,
    setSelectedFramePMUName,
    isPaused,
    setIsPaused,
    frameLines,
    frameTrendHistory,
  } = useDashboardContext()

  if (!selectedFramePMU) return null

  const cfg = selectedFramePMU.cfg
  const channels = selectedFramePMU.lastChannels
  const frame = selectedFramePMU.lastFrame
  const fnom = selectedFramePMU.fnomHz && selectedFramePMU.fnomHz > 0
    ? selectedFramePMU.fnomHz
    : cfg?.fnomHz || 60
  const flags = statFlags(frame?.statDetail)
  const headerNote = cfg?.headerText
    ? ' · HDR present'
    : ' · HDR not provided by device'

  const phasors = displayPhasors(
    channels?.phasors?.length
      ? channels.phasors
      : [
          { name: 'VA', ...(selectedFramePMU.lastPhasor?.va ?? { magnitude: 0, angleDeg: 0 }) },
          { name: 'VB', ...(selectedFramePMU.lastPhasor?.vb ?? { magnitude: 0, angleDeg: 0 }) },
          { name: 'VC', ...(selectedFramePMU.lastPhasor?.vc ?? { magnitude: 0, angleDeg: 0 }) },
          { name: 'IA', ...(selectedFramePMU.lastPhasor?.ia ?? { magnitude: 0, angleDeg: 0 }) },
        ],
  )

  return (
    <>
      <section className="panel">
        <div className="panel-head">
          <div>
            <h3>Data Frame Information</h3>
            <p className="panel-sub">
              Frame inspector — CFG-2 profile and live DATA quantities{headerNote}
            </p>
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
              {!cfg?.available && <div>Waiting for CFG-2 profile…</div>}
              {cfg?.available && (
                <>
                  <div>SYNC: {hexWord(cfg.syncWord)}</div>
                  <div>IDCODE: {cfg.idCode}</div>
                  <div>STATION: {cfg.station || '—'}</div>
                  <div>FNOM: {cfg.fnomHz || '—'} Hz</div>
                  <div>DATA_RATE: {cfg.dataRate || '—'} fps</div>
                  <div>FORMAT: {hexWord(cfg.format)} ({cfg.polar ? 'polar' : 'rect'} · {cfg.phFloat ? 'float' : 'int'} ph)</div>
                  <div>CFG_CNT: {cfg.cfgCnt}</div>
                  <div>PHASORS ({cfg.phasors.length}): {formatPhasorCfgList(cfg.phasors)}</div>
                  <div>ANALOG ({cfg.analogs.length}): {formatList(cfg.analogs, '—')}</div>
                  <div>DIGITAL words: {cfg.digitalWords}</div>
                  {cfg.headerText ? <div>HDR: {cfg.headerText}</div> : null}
                  <div>Live FPS: {Math.round(selectedFramePMU.approxFps)}</div>
                </>
              )}
            </div>
          </div>

          <div className="panel frame-panel">
            <div className="panel-head">
              <h3>Live Data Frame</h3>
              <p className="panel-sub"><span className="live-dot" /> streaming · wire SOC / FRACSEC / STAT / DIG</p>
            </div>
            <div className="frame-box-react">
              {frame && frame.soc > 0 && (
                <div className="frame-stat-summary">
                  STAT {hexWord(frame.stat)}
                  {flags.length ? ` · ${flags.join(' ')}` : ' · OK'}
                  {' · '}DIG {hexWord(frame.digital ?? 0)}
                  {' · '}TQ {hexWord(frame.timeQuality, 2)}
                  {' · '}Δf {round((selectedFramePMU.lastReading?.frequencyDev ?? 0), 4)} Hz
                </div>
              )}
              {frameLines.length === 0 && <div>Waiting for live frames...</div>}
              {frameLines.map((line, idx) => (
                <div key={`${line}-${idx}`}>{line}</div>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section className="charts-grid compact-gap">
        <FreqChart data={frameTrendHistory} fnomHz={fnom} />

        <article className="panel">
          <div className="panel-head">
            <h3>Phasor Quantities</h3>
            <p className="panel-sub">VA–VC, IA–IC · CFG name under label</p>
          </div>
          <div className="phasor-grid-react">
            {phasors.map((item) => (
              <article key={`${item.label}-${item.cfgName}`} className="phasor-react-card">
                <p>{item.label}</p>
                {item.cfgName !== item.label && <span className="phasor-cfg-name">{item.cfgName}</span>}
                <strong>{round(item.magnitude, 2)}</strong>
                <span>{round(item.angleDeg, 2)}°</span>
              </article>
            ))}
          </div>
        </article>
      </section>

      <section className="main-grid dashboard-grid compact-gap">
        <article className="panel">
          <div className="panel-head">
            <h3>Analog Channels</h3>
            <p className="panel-sub">CFG-2 analog names · latest DATA values</p>
          </div>
          <div className="phasor-grid-react">
            {(channels?.analogs?.length ?? 0) === 0 && (
              <div className="frame-box-react">No analog channels in CFG-2</div>
            )}
            {channels?.analogs?.map((a) => (
              <article key={a.name} className="phasor-react-card">
                <p>{a.name}</p>
                <strong>{round(a.value, 4)}</strong>
              </article>
            ))}
          </div>
        </article>

        <article className="panel">
          <div className="panel-head">
            <h3>Digital Status</h3>
            <p className="panel-sub">
              Word {hexWord(frame?.digital ?? 0)} · CFG bit names
            </p>
          </div>
          <div className="digital-bit-grid">
            {(channels?.digitalBits?.length ?? 0) === 0 && (
              <div className="frame-box-react">No digital word</div>
            )}
            {channels?.digitalBits?.map((bit) => (
              <div
                key={`${bit.bit}-${bit.name}`}
                className={`digital-bit ${bit.set ? 'on' : 'off'}`}
                title={`bit ${bit.bit}`}
              >
                <span className="bit-idx">{bit.bit}</span>
                <span className="bit-name">{bit.name.replace(/^DIG\./, '')}</span>
                <span className="bit-val">{bit.set ? '1' : '0'}</span>
              </div>
            ))}
          </div>
        </article>
      </section>
    </>
  )
}
