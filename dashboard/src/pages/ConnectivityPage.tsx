import { ConnectivityKpiGrid } from '../components/connectivity/ConnectivityKpiGrid'
import { ConnectivityMatrix } from '../components/connectivity/ConnectivityMatrix'
import { ConnectivityRecommendations } from '../components/connectivity/ConnectivityRecommendations'
import { RttChart } from '../components/connectivity/RttChart'
import { useDashboardContext } from '../context/DashboardContext'

export function ConnectivityPage() {
  const {
    connectivityRows,
    connectivityKpis,
    connectivityRecs,
    rttHistory,
    rttStreams,
    setDrawerPMUName,
  } = useDashboardContext()

  return (
    <>
      <ConnectivityKpiGrid items={connectivityKpis} />

      <section className="connectivity-grid-2">
        <ConnectivityRecommendations recommendations={connectivityRecs} />
        <RttChart history={rttHistory} streams={rttStreams} />
      </section>

      <ConnectivityMatrix rows={connectivityRows} onRowClick={setDrawerPMUName} />
    </>
  )
}
