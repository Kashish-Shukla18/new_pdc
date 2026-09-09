import type { ConnectivityRec } from '../../types/connectivity'

type Props = {
  recommendations: ConnectivityRec[]
}

function recIcon(sev: ConnectivityRec['sev']) {
  if (sev === 'bad') return '!'
  if (sev === 'warn') return '⚠'
  return 'i'
}

export function ConnectivityRecommendations({ recommendations }: Props) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h3>Connectivity Recommendations</h3>
          <p className="panel-sub">{recommendations.length} recommendations pending</p>
        </div>
      </div>
      <div className="conn-rec-list">
        {recommendations.map((rec) => (
          <article key={rec.title} className={`conn-rec ${rec.sev}`}>
            <div className="conn-rec-icon">{recIcon(rec.sev)}</div>
            <div className="conn-rec-body">
              <h4>{rec.title}</h4>
              <p>{rec.desc}</p>
            </div>
          </article>
        ))}
      </div>
    </div>
  )
}
