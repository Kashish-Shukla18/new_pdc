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
          <h3>Advisories</h3>
          <p className="panel-sub">From live frequency, ROCOF, STAT, angle Δ, and stream health</p>
        </div>
      </div>
      <div className="rec-list-react analytics-rec-list">
        {!recommendations.length && (
          <article className="rec-react analytics-rec info">
            <div className="rec-icon">{iconFor('info')}</div>
            <div className="rec-body">
              <strong>No advisories</strong>
              <p>No offline streams, STAT errors, or threshold breaches on live telemetry.</p>
            </div>
          </article>
        )}
        {recommendations.map((rec, idx) => (
          <article key={`${rec.title}-${idx}`} className={`rec-react analytics-rec ${rec.sev}`}>
            <div className="rec-icon">{iconFor(rec.sev)}</div>
            <div className="rec-body">
              <strong>{rec.title}</strong>
              <p>{rec.desc}</p>
            </div>
          </article>
        ))}
      </div>
    </section>
  )
}
