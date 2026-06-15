import { BookOpen, Radio, TimerReset } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'

export function PageHeader() {
  const { activeTab, dashboard, streamOnline, isPaused } = useDashboardContext()

  return (
    <div className="page-title">
      <div>
        <h2>{activeTab[0].toUpperCase() + activeTab.slice(1)}</h2>
        <p>Live telemetry mapped from the three simulator streams across this section.</p>
      </div>
      <div className="page-actions">
        <button type="button" className="btn ghost">
          <BookOpen size={14} /> Runbook
        </button>
        <button type="button" className="btn">
          <TimerReset size={14} /> {new Date(dashboard.nowUtc).toLocaleTimeString()}
        </button>
        <button type="button" className="btn primary">
          <Radio size={14} /> {streamOnline && !isPaused ? 'Live stream' : 'Standby'}
        </button>
      </div>
    </div>
  )
}
