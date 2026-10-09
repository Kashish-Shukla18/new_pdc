import { ArrivalSpreadPanel } from '../components/connectivity/ArrivalSpreadPanel'
import { ConnectivityGuide } from '../components/connectivity/ConnectivityGuide'
import { ConnectivityKpiGrid } from '../components/connectivity/ConnectivityKpiGrid'
import { ConnectivityMatrix } from '../components/connectivity/ConnectivityMatrix'
import { ConnectivityRecommendations } from '../components/connectivity/ConnectivityRecommendations'
import { EndToEndLatencyPanel } from '../components/connectivity/EndToEndLatencyPanel'
import { LatencyChart } from '../components/connectivity/LatencyChart'
import { useDashboardContext } from '../context/DashboardContext'

export function ConnectivityPage() {
  const {
    enabledPmus,
    connectivityRows,
    connectivityKpis,
    connectivityRecs,
    rttHistory,
    rttStreams,
    cycleHistory,
    cycleLatest,
    spreadHistory,
    spreadLatest,
    setDrawerPMUName,
  } = useDashboardContext()

  return (
    <>
      <ConnectivityGuide />
      <ConnectivityKpiGrid items={connectivityKpis} />
      <EndToEndLatencyPanel history={cycleHistory} latest={cycleLatest} />
      <ArrivalSpreadPanel history={spreadHistory} latest={spreadLatest} pmus={enabledPmus} />
      <LatencyChart history={rttHistory} streams={rttStreams} />
      <ConnectivityRecommendations recommendations={connectivityRecs} />
      <ConnectivityMatrix rows={connectivityRows} onRowClick={setDrawerPMUName} />
    </>
  )
}
