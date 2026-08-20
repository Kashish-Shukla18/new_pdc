import type { ConnectivityRow } from '../../types/dashboard'
import { ageText, round } from '../../utils/format'

type Props = {
  rows: ConnectivityRow[]
  onRowClick: (name: string) => void
}

function statusChip(row: ConnectivityRow) {
  if (row.statusLabel === 'Offline') return 'bad'
  if (row.statusLabel === 'Degraded') return 'warn'
  return 'ok'
}

export function ConnectivityMatrix({ rows, onRowClick }: Props) {
  return (
    <section className="panel table-panel">
      <div className="panel-head">
        <div>
          <h3>Per-PMU Connectivity Matrix</h3>
          <p className="panel-sub">Sorted worst-first · click a row to inspect</p>
        </div>
      </div>
      <div className="table-wrap scrollx">
        <table>
          <thead>
            <tr>
              <th>PMU</th>
              <th>Region</th>
              <th>Link</th>
              <th>Latency (ms)</th>
              <th>Jitter (ms)</th>
              <th>Pkt Loss %</th>
              <th>Avail %</th>
              <th>Last Frame</th>
              <th>Status</th>
              <th>Recommendation</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.name} className="row-click" onClick={() => onRowClick(row.name)}>
                <td><strong>{row.name}</strong></td>
                <td>{row.meta.region}</td>
                <td>{row.link}</td>
                <td>
                  {row.connected && Number.isFinite(row.latency) && row.latency < 900
                    ? round(row.latency, 2)
                    : '—'}
                </td>
                <td>{row.connected ? round(row.jitter, 1) : '—'}</td>
                <td>{row.connected ? round(row.loss, 2) : '—'}</td>
                <td>{row.connected ? round(row.avail, 1) : '0.0'}</td>
                <td>{row.connected ? ageText(row.lastFrameTime) : '—'}</td>
                <td>
                  <span className={`status-chip ${statusChip(row)}`}>{row.statusLabel}</span>
                </td>
                <td className="conn-rec-cell">{row.tableRecommendation}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
