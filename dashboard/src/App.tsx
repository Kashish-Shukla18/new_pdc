import { useEffect, useState } from 'react'
import { AddPMUModal } from './components/drawer/AddPMUModal'
import { PMUDetailDrawer } from './components/drawer/PMUDetailDrawer'
import { Header } from './components/layout/Header'
import { PageHeader } from './components/layout/PageHeader'
import { Sidebar } from './components/layout/Sidebar'
import { DashboardProvider, useDashboardContext } from './context/DashboardContext'
import { PageRouter } from './pages'
import { NotFoundPage } from './pages/NotFoundPage'

function DashboardShell() {
  const { activeTab, notice, setNotice, loadError } = useDashboardContext()
  const [sidebarOpen, setSidebarOpen] = useState(false)

  useEffect(() => {
    const pageNames: Record<string, string> = {
      overview: 'Overview',
      devices: 'Device Inventory',
      dataframes: 'Data Frames',
      connectivity: 'Connectivity',
      analytics: 'Analytics',
      help: 'Help',
      docs: 'Documentation',
    }
    document.title = `${pageNames[activeTab] ?? 'Dashboard'} | PDC`
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute(
        'content',
        `PDC ${pageNames[activeTab] ?? 'dashboard'} for live PMU monitoring and synchrophasor data.`,
      )
  }, [activeTab])

  useEffect(() => {
    const onResize = () => {
      if (window.innerWidth > 960) setSidebarOpen(false)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  return (
    <div className="console-root">
      <Header
        sidebarOpen={sidebarOpen}
        onMenuToggle={() => setSidebarOpen((open) => !open)}
      />

      {sidebarOpen && (
        <button
          type="button"
          className="sidebar-backdrop"
          aria-label="Close navigation"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      <div className="console-shell">
        <Sidebar isOpen={sidebarOpen} onClose={() => setSidebarOpen(false)} />

        <main className={`main ${activeTab === 'docs' ? 'main--docs' : ''}`}>
          <div className={`app-shell ${activeTab === 'docs' ? 'app-shell--docs' : ''}`}>
            <PageHeader />
            {(notice || loadError) && (
              <div
                className={`app-message ${notice?.type ?? 'error'}`}
                role={notice?.type === 'success' ? 'status' : 'alert'}
              >
                <span>{notice?.message ?? loadError}</span>
                {notice && (
                  <button type="button" aria-label="Dismiss message" onClick={() => setNotice(null)}>
                    ×
                  </button>
                )}
              </div>
            )}
            <PageRouter activeTab={activeTab} />
          </div>
        </main>
      </div>

      <PMUDetailDrawer />
      <AddPMUModal />
    </div>
  )
}

function App() {
  if (window.location.pathname !== '/') {
    return <NotFoundPage />
  }

  return (
    <DashboardProvider>
      <DashboardShell />
    </DashboardProvider>
  )
}

export default App
