import type { TabId } from '../types/dashboard'
import { AnalyticsPage } from './AnalyticsPage'
import { ConnectivityPage } from './ConnectivityPage'
import { DataFramesPage } from './DataFramesPage'
import { DevicesPage } from './DevicesPage'
import { DocsPage } from './DocsPage'
import { HelpPage } from './HelpPage'
import { OverviewPage } from './OverviewPage'

const pageComponents: Record<TabId, () => React.ReactNode> = {
  overview: OverviewPage,
  devices: DevicesPage,
  dataframes: DataFramesPage,
  connectivity: ConnectivityPage,
  analytics: AnalyticsPage,
  help: HelpPage,
  docs: DocsPage,
}

export function PageRouter({ activeTab }: { activeTab: TabId }) {
  const Page = pageComponents[activeTab]
  return <Page />
}
