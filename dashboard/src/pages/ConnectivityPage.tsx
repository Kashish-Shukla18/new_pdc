import { ConnectivityKpiGrid } from '../components/connectivity/ConnectivityKpiGrid'
import { ConnectivityMatrix } from '../components/connectivity/ConnectivityMatrix'
import { ConnectivityRecommendations } from '../components/connectivity/ConnectivityRecommendations'
import { PipelineLatencyPanel } from '../components/connectivity/PipelineLatencyPanel'
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
    dashboard,
    uiTiming,
    pmus,
  } = useDashboardContext()

  return (
    <>
      <ConnectivityKpiGrid items={connectivityKpis} />

      <PipelineLatencyPanel latency={dashboard.latency} uiTiming={uiTiming} pmus={pmus} />

      <section className="connectivity-grid-2">
        <ConnectivityRecommendations recommendations={connectivityRecs} />
        <RttChart history={rttHistory} streams={rttStreams} />
      </section>

      <ConnectivityMatrix rows={connectivityRows} onRowClick={setDrawerPMUName} />
    </>
  )
}
