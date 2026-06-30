import { useEffect, useRef } from 'react'
import { Clock3, Menu, Pause, Play, Radio } from 'lucide-react'
import { useClock } from '../../hooks/useClock'
import { useDashboardContext } from '../../context/DashboardContext'
import { round } from '../../utils/format'
import { Logo } from './Logo'

interface HeaderProps {
  onMenuToggle?: () => void
  sidebarOpen?: boolean
}

export function Header({ onMenuToggle, sidebarOpen = false }: HeaderProps) {
  const headerRef = useRef<HTMLElement>(null)
  const clockLabel = useClock()
  const { frameRate, systemTone, systemMessage, isPaused, setIsPaused, streamOnline, pmus } = useDashboardContext()

  useEffect(() => {
    const header = headerRef.current
    if (!header) return

    const syncHeaderHeight = () => {
      document.documentElement.style.setProperty('--header-height', `${header.offsetHeight}px`)
    }

    syncHeaderHeight()
    const observer = new ResizeObserver(syncHeaderHeight)
    observer.observe(header)
    window.addEventListener('resize', syncHeaderHeight)

    return () => {
      observer.disconnect()
      window.removeEventListener('resize', syncHeaderHeight)
    }
  }, [])

  return (
    <header ref={headerRef} className="app-header">
      <div className="brand">
        <button
          type="button"
          className="btn sidebar-toggle"
          aria-label="Open navigation menu"
          aria-expanded={sidebarOpen}
          onClick={onMenuToggle}
        >
          <Menu size={18} />
        </button>
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
