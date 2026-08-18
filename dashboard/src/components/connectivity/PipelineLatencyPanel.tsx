import type { LatencyStage, PipelineLatency, PMUWithMeta, UiTiming } from '../../types/dashboard'
import { formatHopMs, hopTone } from '../../utils/format'

type Props = {
  latency?: PipelineLatency
  uiTiming: UiTiming
  pmus: PMUWithMeta[]
}

const FRAME_GROUPS = new Set(['ingest', 'process', 'dashboard', 'sink', 'e2e'])

const PMU_HOP_COLS: { id: string; label: string }[] = [
  { id: 'tcp_dial', label: 'Dial' },
  { id: 'handshake_hdr', label: 'HDR wait' },
  { id: 'handshake_total', label: 'Handshake' },
  { id: 'tcp_wait', label: 'TCP wait' },
  { id: 'tcp_copy', label: 'TCP copy' },
  { id: 'tcp_read', label: 'TCP total' },
  { id: 'raw_kafka_enqueue', label: 'K enqueue' },
  { id: 'raw_kafka_publish', label: 'K write' },
  { id: 'raw_kafka_lag', label: 'K lag' },
  { id: 'frame_to_parse', label: 'To parse' },
  { id: 'parse', label: 'Parse' },
  { id: 'quality', label: 'Quality' },
  { id: 'readings_publish', label: 'Readings pub' },
  { id: 'dashboard_record', label: 'Dash record' },
  { id: 'sink_store', label: 'Sink' },
  { id: 'e2e_recv_to_dashboard', label: 'E2E' },
]

function barWidth(value: number, max: number) {
  if (max <= 0 || value <= 0) return 0
  return Math.max(2, Math.min(100, (value / max) * 100))
}

export function PipelineLatencyPanel({ latency, uiTiming, pmus }: Props) {
  const stages = latency?.stages ?? []
  const connStages = stages.filter((s) => s.group === 'connection' && s.count > 0)
  const waitStages = stages.filter((s) => s.group === 'idle' && s.count > 0)
  const frameStages = stages.filter((s) => FRAME_GROUPS.has(s.group) && s.count > 0)

  const uiStage: LatencyStage = {
    id: 'ui_refresh',
    label: 'Dashboard UI refresh',
    group: 'dashboard',
    lastMs: uiTiming.totalMs,
    avgMs: uiTiming.totalMs,
    p95Ms: uiTiming.totalMs,
    maxMs: uiTiming.totalMs,
    count: uiTiming.totalMs > 0 ? 1 : 0,
  }

  const waterfall = [...frameStages, ...(uiStage.lastMs > 0 ? [uiStage] : [])]
  const maxAvg = Math.max(0.001, ...waterfall.map((s) => Math.max(s.avgMs, s.p95Ms, s.lastMs)))

  const slowest = latency?.slowestStage
    ? waterfall.find((s) => s.id === latency.slowestStage) ?? {
        id: latency.slowestStage,
        label: latency.slowestLabel || latency.slowestStage,
        avgMs: latency.slowestAvgMs,
      }
    : waterfall.reduce<(typeof waterfall)[0] | null>((best, s) => {
        if (!best || s.avgMs > best.avgMs) return s
        return best
      }, null)

  const connected = pmus.filter((p) => p.connected)

  return (
    <section className="panel pipeline-latency">
      <div className="panel-head">
        <div>
          <h3>Pipeline hop timing</h3>
          <p className="panel-sub">
            Wall time per function · last / avg / p95 over the last ~256 samples · slowest hop is the bottleneck
          </p>
        </div>
        {slowest && (
          <div className={`pipeline-slowest ${hopTone(slowest.avgMs, 'ingest')}`}>
            <span>Slowest function</span>
            <strong>{slowest.label}</strong>
            <em>{formatHopMs(slowest.avgMs)} avg</em>
          </div>
        )}
      </div>

      {waitStages.length > 0 && (
        <div className="pipeline-conn">
          {waitStages.map((stage) => (
            <article key={stage.id} className="pipeline-conn-card">
              <p>{stage.label}</p>
              <strong>{formatHopMs(stage.avgMs)}</strong>
              <span>avg wait for next PMU sample · not processing</span>
            </article>
          ))}
        </div>
      )}

      {connStages.length > 0 && (
        <div className="pipeline-conn">
          {connStages.map((stage) => (
            <article key={stage.id} className={`pipeline-conn-card ${hopTone(stage.lastMs, 'connection')}`}>
              <p>{stage.label}</p>
              <strong>{formatHopMs(stage.lastMs)}</strong>
              <span>last · p95 {formatHopMs(stage.p95Ms)}</span>
            </article>
          ))}
        </div>
      )}

      <div className="pipeline-waterfall">
        {waterfall.length === 0 && (
          <p className="pipeline-empty">Waiting for frames — hop times appear once data is flowing.</p>
        )}
        {waterfall.map((stage) => {
          const tone = hopTone(stage.avgMs, stage.group)
          const isSlowest = slowest?.id === stage.id
          return (
            <div key={stage.id} className={`pipeline-row ${tone} ${isSlowest ? 'slowest' : ''}`}>
              <div className="pipeline-row-meta">
                <span className="pipeline-row-label">{stage.label}</span>
                <span className="pipeline-row-group">{stage.group}</span>
              </div>
              <div className="pipeline-bars">
                <div className="pipeline-bar-track" title={`avg ${formatHopMs(stage.avgMs)} ms`}>
                  <div className="pipeline-bar avg" style={{ width: `${barWidth(stage.avgMs, maxAvg)}%` }} />
                </div>
                <div className="pipeline-bar-track p95" title={`p95 ${formatHopMs(stage.p95Ms)} ms`}>
                  <div className="pipeline-bar p95" style={{ width: `${barWidth(stage.p95Ms, maxAvg)}%` }} />
                </div>
              </div>
              <div className="pipeline-nums">
                <span><em>last</em> {formatHopMs(stage.lastMs)}</span>
                <span><em>avg</em> {formatHopMs(stage.avgMs)}</span>
                <span><em>p95</em> {formatHopMs(stage.p95Ms)}</span>
                <span><em>max</em> {formatHopMs(stage.maxMs)}</span>
              </div>
            </div>
          )
        })}
      </div>

      {uiTiming.totalMs > 0 && (
        <p className="pipeline-ui-note">
          UI poll (browser): network {formatHopMs(uiTiming.fetchMs)} · JSON parse {formatHopMs(uiTiming.jsonMs)} ·
          total {formatHopMs(uiTiming.totalMs)}. Display also waits up to 1 s between polls.
        </p>
      )}

      {connected.length > 0 && (
        <div className="table-wrap scrollx pipeline-pmu-table">
          <table>
            <thead>
              <tr>
                <th>PMU</th>
                {PMU_HOP_COLS.map((col) => (
                  <th key={col.id}>{col.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {connected.map((pmu) => (
                <tr key={pmu.name}>
                  <td><strong>{pmu.name}</strong></td>
                  {PMU_HOP_COLS.map((col) => {
                    const ms = pmu.lastHops?.[col.id]
                    const group =
                      col.id.startsWith('handshake') || col.id === 'tcp_dial'
                        ? 'connection'
                        : col.id === 'tcp_wait' || col.id === 'tcp_read' || col.id === 'tcp_interarrival'
                          ? 'idle'
                          : 'ingest'
                    return (
                      <td key={col.id} className={`hop-cell ${hopTone(ms ?? 0, group)}`}>
                        {formatHopMs(ms)}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
