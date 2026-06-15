import type { AnalyticsRecommendation } from '../../types/analytics'

type Props = {
  recommendations: AnalyticsRecommendation[]
}

function iconFor(sev: AnalyticsRecommendation['sev']) {
  if (sev === 'bad') return '!'
  if (sev === 'warn') return '⚠'
  return 'i'
}

export function OperatorRecommendations({ recommendations }: Props) {
  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h3>Operator Recommendations</h3>
          <p className="panel-sub">Generated from analytics engine — prioritized</p>
        </div>
      </div>
      <div className="rec-list-react analytics-rec-list">
        {recommendations.map((rec, idx) => (
          <article key={`${rec.title}-${idx}`} className={`rec-react analytics-rec ${rec.sev}`}>
            <div className="rec-icon">{iconFor(rec.sev)}</div>
            <div className="rec-body">
              <strong>{rec.title}</strong>
              <p>{rec.desc}</p>
            </div>
            <div className="rec-actions">
              <button type="button" className="btn ghost">Details</button>
              <button type="button" className="btn primary">Acknowledge</button>
            </div>
          </article>
        ))}
      </div>
    </section>
  )
}
