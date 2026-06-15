import { AnalyticsKpiGrid } from '../components/analytics/AnalyticsKpiGrid'
import { AngleDiffChart } from '../components/analytics/AngleDiffChart'
import { IslandingRiskList } from '../components/analytics/IslandingRiskList'
import { OperatorRecommendations } from '../components/analytics/OperatorRecommendations'
import { OscillationChart } from '../components/analytics/OscillationChart'
import { VoltageProfileChart } from '../components/analytics/VoltageProfileChart'
import { useDashboardContext } from '../context/DashboardContext'

export function AnalyticsPage() {
  const {
    analyticsKpis,
    chartAnglePairs,
    angleHistory,
    oscillationModes,
    islandingRows,
    voltageProfile,
    analyticsRecs,
  } = useDashboardContext()

  return (
    <>
      <AnalyticsKpiGrid items={analyticsKpis} />

      <section className="analytics-grid-2">
        <AngleDiffChart history={angleHistory} pairs={chartAnglePairs} />
        <OscillationChart modes={oscillationModes} />
      </section>

      <section className="analytics-grid-2">
        <IslandingRiskList rows={islandingRows} />
        <VoltageProfileChart buses={voltageProfile} />
      </section>

      <OperatorRecommendations recommendations={analyticsRecs} />
    </>
  )
}
