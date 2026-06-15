import { createContext, useContext, type ReactNode } from 'react'
import { useDashboard, type DashboardContextValue } from '../hooks/useDashboard'

const DashboardContext = createContext<DashboardContextValue | null>(null)

export function DashboardProvider({ children }: { children: ReactNode }) {
  const value = useDashboard()
  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>
}

export function useDashboardContext() {
  const context = useContext(DashboardContext)
  if (!context) {
    throw new Error('useDashboardContext must be used within DashboardProvider')
  }
  return context
}
