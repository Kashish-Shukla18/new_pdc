import { Clock3, Radio } from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'

const PAGE_COPY: Record<string, { title: string; subtitle: string }> = {
  overview: {
    title: 'Overview',
    subtitle: 'Live fleet health, frequency, ROCOF, locations, and operational events.',
  },
  devices: {
    title: 'Device Inventory',
    subtitle: 'Registered PMU connection settings and current stream status.',
  },
  dataframes: {
    title: 'Data Frames',
    subtitle: 'Decoded CFG-2 and DATA fields from the selected PMU.',
  },
  analytics: {
    title: 'Analytics & Grid Recommendations',
    subtitle: 'Live phasor magnitudes, diagrams, angle differences, and operator advisories.',
  },
  connectivity: {
    title: 'Connectivity & pipeline latency',
    subtitle: 'Measured connection, receive, parse, storage, and dashboard timing.',
  },
  help: {
    title: 'Help & Support',
    subtitle: 'Practical guidance for operating and troubleshooting the PDC.',
  },
  docs: {
    title: 'Documentation',
    subtitle: 'Reference for dashboard pages, metrics, and data sources.',
  },
}

export function PageHeader() {
  const { activeTab, dashboard, streamOnline, isPaused } = useDashboardContext()
  const copy = PAGE_COPY[activeTab]
  const title = copy?.title ?? 'PDC Dashboard'
  const subtitle = copy?.subtitle ?? 'Live PMU monitoring.'

  return (
    <div className="page-title">
      <div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      <div className="page-actions">
        <span className="header-pill">
          <Clock3 size={14} /> {new Date(dashboard.nowUtc).toLocaleTimeString()}
        </span>
        <span className={`header-pill ${streamOnline && !isPaused ? 'live' : ''}`}>
          <Radio size={14} /> {streamOnline && !isPaused ? 'Live stream' : 'Standby'}
        </span>
      </div>
    </div>
  )
}
