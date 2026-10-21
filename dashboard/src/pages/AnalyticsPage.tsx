import { useMemo, useState } from 'react'
import {
  Box,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Typography,
} from '@mui/material'
import Grid from '@mui/material/Grid'
import { AnalyticsKpiGrid } from '../components/analytics/AnalyticsKpiGrid'
import { AngleDiffChart } from '../components/analytics/AngleDiffChart'
import { OperatorRecommendations } from '../components/analytics/OperatorRecommendations'
import { PhasorDiagram, type DiagramVector } from '../components/analytics/PhasorDiagram'
import { PhasorMagnitudeChart } from '../components/analytics/PhasorMagnitudeChart'
import { CHART_COLORS } from '../constants'
import { useDashboardContext } from '../context/DashboardContext'
import type { AlignedBatch, PMUWithMeta } from '../types/dashboard'
import {
  latestAlignedBatch,
  phasorsFromAlignedPoint,
} from '../utils/alignedChart'
import { PHASOR_I_COLORS, PHASOR_V_COLORS } from '../utils/analyticsColors'
import { formatTSMs } from '../utils/format'
import { chartLabel } from '../utils/pmu'

type PhaseFilter = 'all' | 'VA' | 'VB' | 'VC' | 'IA' | 'IB' | 'IC'

const V_FILTERS: PhaseFilter[] = ['all', 'VA', 'VB', 'VC']
const I_FILTERS: PhaseFilter[] = ['all', 'IA', 'IB', 'IC']

function colorForVector(
  pmuIndex: number,
  label: string,
  kind: 'voltage' | 'current',
  multiPmu: boolean,
): string {
  if (!multiPmu) {
    if (kind === 'voltage') return PHASOR_V_COLORS[label] ?? CHART_COLORS[0]
    return PHASOR_I_COLORS[label] ?? CHART_COLORS[0]
  }
  return CHART_COLORS[pmuIndex % CHART_COLORS.length]
}

function buildVectors(
  selected: PMUWithMeta[],
  allPmus: PMUWithMeta[],
  kind: 'voltage' | 'current',
  phaseFilter: PhaseFilter,
  batches: AlignedBatch[] | undefined,
): DiagramVector[] {
  const live = selected.filter((p) => p.connected)
  const multiPmu = live.length > 1
  const prefix = kind === 'voltage' ? 'V' : 'I'
  const batch = latestAlignedBatch(
    batches,
    live.map((p) => p.name),
  )
  if (!batch) return []

  const out: DiagramVector[] = []
  for (const pmu of live) {
    if (!batch.points?.[pmu.name]) continue
    const pmuIndex = allPmus.findIndex((p) => p.name === pmu.name)
    const rows = phasorsFromAlignedPoint(batch.points[pmu.name]).filter((r) => {
      if (!r.label.startsWith(prefix)) return false
      if (phaseFilter === 'all') return true
      return r.label === phaseFilter
    })
    for (const r of rows) {
      out.push({
        id: `${pmu.name}::${r.label}::${r.cfgName}`,
        pmuName: pmu.name,
        pmuLabel: chartLabel(pmu),
        label: r.label,
        cfgName: r.cfgName,
        magnitude: r.magnitude,
        angleDeg: r.angleDeg,
        color: colorForVector(Math.max(pmuIndex, 0), r.label, kind, multiPmu),
      })
    }
  }
  return out
}

export function AnalyticsPage() {
  const {
    enabledPmus: pmus,
    dashboard,
    analyticsKpis,
    analyticsRecs,
  } = useDashboardContext()

  const onlinePMUs = useMemo(() => pmus.filter((p) => p.connected), [pmus])

  const [voltagePhase, setVoltagePhase] = useState<PhaseFilter>('all')
  const [currentPhase, setCurrentPhase] = useState<PhaseFilter>('all')

  const diagramBatch = useMemo(
    () =>
      latestAlignedBatch(
        dashboard.alignedBatches,
        onlinePMUs.map((p) => p.name),
      ),
    [dashboard.alignedBatches, onlinePMUs],
  )

  /** Online PMUs with no point on the latest aligned tick (chart-gap equivalent). */
  const diagramMissing = useMemo(() => {
    if (!diagramBatch) return []
    return onlinePMUs
      .filter((p) => !diagramBatch.points?.[p.name])
      .map((p) => ({ name: p.name, label: chartLabel(p) }))
  }, [diagramBatch, onlinePMUs])

  const voltageVectors = useMemo(
    () => buildVectors(onlinePMUs, pmus, 'voltage', voltagePhase, dashboard.alignedBatches),
    [onlinePMUs, pmus, voltagePhase, dashboard.alignedBatches],
  )
  const currentVectors = useMemo(
    () => buildVectors(onlinePMUs, pmus, 'current', currentPhase, dashboard.alignedBatches),
    [onlinePMUs, pmus, currentPhase, dashboard.alignedBatches],
  )

  const emptyHint = !onlinePMUs.length
    ? 'No online PMUs — connect a stream to plot live phasors.'
    : !diagramBatch
      ? 'Waiting for aligned samples…'
      : undefined

  return (
    <Box className="analytics-page" sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <AnalyticsKpiGrid items={analyticsKpis} />

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <PhasorMagnitudeChart pmus={pmus} kind="voltage" alignedBatches={dashboard.alignedBatches} />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <PhasorMagnitudeChart pmus={pmus} kind="current" alignedBatches={dashboard.alignedBatches} />
        </Grid>
      </Grid>

      <Box
        sx={{
          p: { xs: 1.5, sm: 2 },
          borderRadius: 2,
          border: '1px solid',
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        <Box sx={{ mb: 2 }}>
          <Typography variant="h6" component="h3">
            Phasor diagrams
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Same aligned tick as Overview / Analytics charts
            {diagramBatch ? ` · ${formatTSMs(diagramBatch.ts)}` : ''}
          </Typography>
        </Box>

        <Grid container spacing={2}>
          <Grid size={{ xs: 12, lg: 6 }}>
            <Stack spacing={1}>
              <FormControl size="small" sx={{ maxWidth: 220, alignSelf: { xs: 'stretch', sm: 'flex-end' } }}>
                <InputLabel id="voltage-phase-filter-label">Voltage channels</InputLabel>
                <Select
                  labelId="voltage-phase-filter-label"
                  label="Voltage channels"
                  value={voltagePhase}
                  onChange={(e) => setVoltagePhase(e.target.value as PhaseFilter)}
                >
                  {V_FILTERS.map((f) => (
                    <MenuItem key={f} value={f}>
                      {f === 'all' ? 'All V' : f}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
              <PhasorDiagram
                kind="voltage"
                vectors={voltageVectors}
                emptyMessage={emptyHint}
                sampleTs={diagramBatch?.ts}
                missing={diagramMissing}
              />
            </Stack>
          </Grid>
          <Grid size={{ xs: 12, lg: 6 }}>
            <Stack spacing={1}>
              <FormControl size="small" sx={{ maxWidth: 220, alignSelf: { xs: 'stretch', sm: 'flex-end' } }}>
                <InputLabel id="current-phase-filter-label">Current channels</InputLabel>
                <Select
                  labelId="current-phase-filter-label"
                  label="Current channels"
                  value={currentPhase}
                  onChange={(e) => setCurrentPhase(e.target.value as PhaseFilter)}
                >
                  {I_FILTERS.map((f) => (
                    <MenuItem key={f} value={f}>
                      {f === 'all' ? 'All I' : f}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
              <PhasorDiagram
                kind="current"
                vectors={currentVectors}
                emptyMessage={emptyHint}
                sampleTs={diagramBatch?.ts}
                missing={diagramMissing}
              />
            </Stack>
          </Grid>
        </Grid>
      </Box>

      <AngleDiffChart pmus={pmus} alignedBatches={dashboard.alignedBatches} />
      <OperatorRecommendations recommendations={analyticsRecs} />
    </Box>
  )
}
