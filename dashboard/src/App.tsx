import { useEffect, useState } from 'react'
import { AddPMUModal } from './components/drawer/AddPMUModal'
import { PMUDetailDrawer } from './components/drawer/PMUDetailDrawer'
import { Header } from './components/layout/Header'
import { PageHeader } from './components/layout/PageHeader'
import { Sidebar } from './components/layout/Sidebar'
import { DashboardProvider, useDashboardContext } from './context/DashboardContext'
import { PageRouter } from './pages'

function DashboardShell() {
  const { activeTab } = useDashboardContext()
  const [sidebarOpen, setSidebarOpen] = useState(false)

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

        <main className="main">
          <div className="app-shell">
            <PageHeader />
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
  return (
    <DashboardProvider>
      <DashboardShell />
    </DashboardProvider>
  )
}

export default App
