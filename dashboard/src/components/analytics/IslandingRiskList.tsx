import type { IslandingRow } from '../../types/analytics'

type Props = {
  rows: IslandingRow[]
}

function riskClass(risk: IslandingRow['risk']) {
  if (risk === 'High') return 'bad'
  if (risk === 'Medium') return 'warn'
  return 'ok'
}

export function IslandingRiskList({ rows }: Props) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h3>Islanding Risk Indicators</h3>
          <p className="panel-sub">Per region / simulator corridor</p>
        </div>
      </div>
      <div className="island-list">
        {rows.map((row) => (
          <div key={row.name} className="island-row">
            <div>
              <strong>{row.name}</strong>
              <p>{row.detail}</p>
            </div>
            <span className={`risk-pill ${riskClass(row.risk)}`}>{row.risk}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
