import { BookOpen, Radio, TimerReset } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'

const PAGE_COPY: Partial<Record<string, { title: string; subtitle: string }>> = {
  analytics: {
    title: 'Analytics & Grid Recommendations',
    subtitle:
      'WAMS analytics — voltage angle differences, oscillation detection, islanding risk, and operator advisories',
  },
}

export function PageHeader() {
  const { activeTab, dashboard, streamOnline, isPaused } = useDashboardContext()
  const copy = PAGE_COPY[activeTab]
  const title = copy?.title ?? activeTab[0].toUpperCase() + activeTab.slice(1)
  const subtitle =
    copy?.subtitle ?? 'Live telemetry mapped from the simulator streams across this section.'

  return (
    <div className="page-title">
      <div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
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
