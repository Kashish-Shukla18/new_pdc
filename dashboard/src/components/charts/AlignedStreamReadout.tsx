import { formatTSMs, formatWireFrameTime } from '../../utils/format'

export type AlignedStreamRow = {
  name: string
  color: string
  /** Formatted live value, e.g. "50.012 Hz" */
  value: string
  /** Chart / aligned axis timestamp (ms since epoch) */
  chartTs: number
  wireSoc?: number
  wireFracCount?: number
}

type Props = {
  rows: AlignedStreamRow[]
  /** Optional one-line status from PDC timeAlign */
  alignHint?: string
}

/** Live per-stream value + frame timestamps for multi-PMU alignment checks. */
export function AlignedStreamReadout({ rows, alignHint }: Props) {
  if (!rows.length) return null

  const tsCounts = new Map<number, number>()
  for (const row of rows) {
    if (row.chartTs > 0) tsCounts.set(row.chartTs, (tsCounts.get(row.chartTs) ?? 0) + 1)
  }
  const majorityTs =
    [...tsCounts.entries()].sort((a, b) => b[1] - a[1] || b[0] - a[0])[0]?.[0] ?? 0

  return (
    <div className="align-readout">
      {alignHint ? <p className="align-readout-hint">{alignHint}</p> : null}
      <div className="align-readout-grid">
        <div className="align-readout-head-row">
          <div className="align-readout-head">Stream</div>
          <div className="align-readout-head">Value</div>
          <div className="align-readout-head">Aligned ts</div>
          <div className="align-readout-head">Wire SOC.FRAC</div>
          <div className="align-readout-head">Match</div>
        </div>
        {rows.map((row) => {
          const matched = majorityTs > 0 && row.chartTs === majorityTs && (tsCounts.get(majorityTs) ?? 0) >= 2
          const alone = majorityTs > 0 && row.chartTs !== majorityTs
          return (
            <div key={row.name} className="align-readout-entry">
              <div className="align-readout-name">
                <i style={{ background: row.color }} />
                {row.name}
              </div>
              <div className="align-readout-mono">{row.value}</div>
              <div className="align-readout-mono">{formatTSMs(row.chartTs)}</div>
              <div className="align-readout-mono">
                {formatWireFrameTime(row.wireSoc, row.wireFracCount)}
              </div>
              <div className={`align-readout-match ${matched ? 'ok' : alone ? 'warn' : ''}`}>
                {matched ? 'same tick' : alone ? 'offset' : '—'}
              </div>
            </div>
          )
        })}
      </div>
      <p className="align-readout-foot">
        Alignment check: <strong>Aligned ts</strong> should match across streams on a complete set.
        Wire SOC may differ when using corrected/receive bucketing.
      </p>
    </div>
  )
}
