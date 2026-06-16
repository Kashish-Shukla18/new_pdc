import { Clock3, Pause, Play, Radio } from 'lucide-react'
import { useClock } from '../../hooks/useClock'
import { useDashboardContext } from '../../context/DashboardContext'
import { round } from '../../utils/format'
import { Logo } from './Logo'

export function Header() {
  const clockLabel = useClock()
  const { frameRate, systemTone, systemMessage, isPaused, setIsPaused, streamOnline, pmus } = useDashboardContext()

  return (
    <header className="app-header">
      <div className="brand">
        <Logo size={44} />
        <div className="brand-text">
          <div className="brand-title-row">
            <h1>National PMU Monitoring Console</h1>
            <span className="brand-badge">WAMS</span>
          </div>
          <p className="brand-tagline">Phasor Data Concentrator · Real-time grid observability</p>
        </div>
      </div>

      <div className="header-meta">
        <div className={`header-pill system-state ${systemTone}`}>
          <span className={`dot ${systemTone === 'bad' ? 'bad' : systemTone === 'warn' ? 'warn' : ''}`} />
          <span>{systemMessage}</span>
        </div>
        <div className="header-pill">
          <Radio size={13} />
          <span>{pmus.length} PMUs</span>
        </div>
        <div className="header-pill accent">
          <span className="mono">{round(frameRate)} fps</span>
        </div>
        <div className={`header-pill ${streamOnline && !isPaused ? 'live' : ''}`}>
          <span className={`dot ${streamOnline && !isPaused ? '' : 'warn'}`} />
          <span>{streamOnline && !isPaused ? 'Live' : 'Standby'}</span>
        </div>
        <div className="header-pill clock-pill">
          <Clock3 size={14} />
          <span className="mono">{clockLabel}</span>
          <span className="tz">IST</span>
        </div>
        <button
          type="button"
          className="btn header-action"
          onClick={() => setIsPaused((current) => !current)}
        >
          {isPaused ? <Play size={14} /> : <Pause size={14} />}
          {isPaused ? 'Resume' : 'Pause'}
        </button>
      </div>
    </header>
  )
}
