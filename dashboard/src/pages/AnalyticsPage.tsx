import { useMemo, useState } from 'react'
import { AnalyticsKpiGrid } from '../components/analytics/AnalyticsKpiGrid'
import { AngleDiffChart } from '../components/analytics/AngleDiffChart'
import { OperatorRecommendations } from '../components/analytics/OperatorRecommendations'
import { PhasorDiagram } from '../components/analytics/PhasorDiagram'
import { PhasorMagnitudeChart } from '../components/analytics/PhasorMagnitudeChart'
import { useDashboardContext } from '../context/DashboardContext'
import { displayPhasors } from '../utils/phasorLabels'

export function AnalyticsPage() {
  const {
    pmus,
    analyticsKpis,
    chartAnglePairs,
    angleHistory,
    analyticsRecs,
  } = useDashboardContext()

  const [analyticsPMUName, setAnalyticsPMUName] = useState('')
  const selectedName = analyticsPMUName && pmus.some((p) => p.name === analyticsPMUName)
    ? analyticsPMUName
    : pmus[0]?.name ?? ''
  const selected = pmus.find((p) => p.name === selectedName) ?? pmus[0]

  const displayPhasorRows = useMemo(() => {
    if (!selected) return []
    if (selected.lastChannels?.phasors?.length) {
      return displayPhasors(selected.lastChannels.phasors)
    }
    return displayPhasors([
      { name: 'VA', ...(selected.lastPhasor?.va ?? { magnitude: 0, angleDeg: 0 }) },
      { name: 'VB', ...(selected.lastPhasor?.vb ?? { magnitude: 0, angleDeg: 0 }) },
      { name: 'VC', ...(selected.lastPhasor?.vc ?? { magnitude: 0, angleDeg: 0 }) },
      { name: 'IA', ...(selected.lastPhasor?.ia ?? { magnitude: 0, angleDeg: 0 }) },
    ])
  }, [selected])

  return (
    <div className="analytics-page">
      <AnalyticsKpiGrid items={analyticsKpis} />

      <section className="panel analytics-toolbar">
        <div className="panel-head">
          <div>
            <h3>Phasor focus</h3>
            <p className="panel-sub">V/I charts and diagrams use the selected PMU</p>
          </div>
          <div className="panel-tools-inline">
            <select
              value={selectedName}
              onChange={(event) => setAnalyticsPMUName(event.target.value)}
              disabled={!pmus.length}
            >
              {pmus.map((pmu) => (
                <option key={pmu.name} value={pmu.name}>
                  {pmu.name}{pmu.connected ? '' : ' (offline)'}
                </option>
              ))}
            </select>
          </div>
        </div>
      </section>

      <section className="analytics-grid-2">
        <PhasorMagnitudeChart
          trends={selected?.trends ?? []}
          kind="voltage"
          pmuName={selectedName || '—'}
        />
        <PhasorMagnitudeChart
          trends={selected?.trends ?? []}
          kind="current"
          pmuName={selectedName || '—'}
        />
      </section>

      <section className="analytics-grid-2">
        <PhasorDiagram kind="voltage" phasors={displayPhasorRows} pmuName={selectedName || '—'} />
        <PhasorDiagram kind="current" phasors={displayPhasorRows} pmuName={selectedName || '—'} />
      </section>

      <section className="analytics-grid-angle">
        <AngleDiffChart history={angleHistory} pairs={chartAnglePairs} />
      </section>

      <OperatorRecommendations recommendations={analyticsRecs} />
    </div>
  )
}
