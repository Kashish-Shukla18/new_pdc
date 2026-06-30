import {
  CircleHelp,
  FileText,
  Gauge,
  LayoutDashboard,
  Signal,
  SlidersHorizontal,
  Wifi,
} from 'lucide-react'
import { useDashboardContext } from '../../context/DashboardContext'
import type { TabId } from '../../types/dashboard'

const navIcons: Partial<Record<TabId, React.ReactNode>> = {
  overview: <LayoutDashboard size={16} />,
  devices: <SlidersHorizontal size={16} />,
  dataframes: <Signal size={16} />,
  connectivity: <Wifi size={16} />,
  analytics: <Gauge size={16} />,
  help: <CircleHelp size={16} />,
  docs: <FileText size={16} />,
}

interface SidebarProps {
  isOpen?: boolean
  onClose?: () => void
}

export function Sidebar({ isOpen = false, onClose }: SidebarProps) {
  const { activeTab, setActiveTab, navItems, systemCounts, dashboard } = useDashboardContext()

  const selectTab = (id: Parameters<typeof setActiveTab>[0]) => {
    setActiveTab(id)
    onClose?.()
  }

  const monitoringItems = navItems.filter((item) => item.section === 'Monitoring')
  const resourceItems = navItems.filter((item) => item.section === 'Resources')

  return (
    <aside className={`sidebar${isOpen ? ' open' : ''}`}>
      <div className="nav-section">Monitoring</div>
      {monitoringItems.map((item) => (
        <button
          key={item.id}
          type="button"
          className={`nav-item ${activeTab === item.id ? 'active' : ''}`}
          onClick={() => selectTab(item.id)}
        >
          {navIcons[item.id]}
          {item.label}
          {item.badge && <span className={`nav-badge ${item.bad ? 'bad' : ''}`}>{item.badge}</span>}
        </button>
      ))}

      <div className="nav-section">Resources</div>
      {resourceItems.map((item) => (
        <button
          key={item.id}
          type="button"
          className={`nav-item ${activeTab === item.id ? 'active' : ''}`}
          onClick={() => selectTab(item.id)}
        >
          {navIcons[item.id]}
          {item.label}
        </button>
      ))}

      <div className="nav-section">Status</div>
      <div className="status-box">
        <div>
          <strong>{systemCounts.connected}</strong>
          <span>Connected PMUs</span>
        </div>
        <div>
          <strong>{dashboard.eventCount}</strong>
          <span>Events observed</span>
        </div>
        <div>
          <strong>{systemCounts.totalErrors}</strong>
          <span>Pipeline errors</span>
        </div>
      </div>
    </aside>
  )
}
