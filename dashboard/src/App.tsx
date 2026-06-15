import { AddPMUModal } from './components/drawer/AddPMUModal'
import { PMUDetailDrawer } from './components/drawer/PMUDetailDrawer'
import { Header } from './components/layout/Header'
import { PageHeader } from './components/layout/PageHeader'
import { Sidebar } from './components/layout/Sidebar'
import { DashboardProvider, useDashboardContext } from './context/DashboardContext'
import { PageRouter } from './pages'

function DashboardShell() {
  const { activeTab } = useDashboardContext()

  return (
    <div className="console-root">
      <Header />

      <div className="console-shell">
        <Sidebar />

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
