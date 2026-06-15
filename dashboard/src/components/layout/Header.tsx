import { Clock3 } from 'lucide-react'
import { useClock } from '../../hooks/useClock'
import { useDashboardContext } from '../../context/DashboardContext'
import { round } from '../../utils/format'

export function Header() {
  const clockLabel = useClock()
  const { frameRate, systemTone, systemMessage, isPaused, setIsPaused } = useDashboardContext()

  return (
    <header className="app-header">
      <div className="brand">
        <div className="logo">PDC</div>
        <div className="brand-text">
          <h1>National PMU Monitoring Console</h1>
          <p>3-simulator live streams mapped across all React dashboard tabs</p>
        </div>
      </div>

      <div className="header-meta">
        <div className="system-state">
          <span className={`dot ${systemTone === 'bad' ? 'bad' : systemTone === 'warn' ? 'warn' : ''}`} />
          <span>{systemMessage}</span>
        </div>
        <span className="utc-badge">{round(frameRate)} fps</span>
        <span className="clock">
          <Clock3 size={14} /> {clockLabel} IST
        </span>
        <button type="button" className="btn ghost" onClick={() => setIsPaused((current) => !current)}>
          {isPaused ? 'Resume' : 'Pause'}
        </button>
      </div>
    </header>
  )
}
