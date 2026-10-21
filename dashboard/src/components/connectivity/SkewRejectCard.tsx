import type { PMUWithMeta } from '../../types/dashboard'

type Props = {
  pmus: PMUWithMeta[]
}

export function SkewRejectCard({ pmus }: Props) {
  const skewed = pmus
    .filter((p) => (p.skewRejects ?? 0) > 0)
    .sort((a, b) => (b.skewRejects ?? 0) - (a.skewRejects ?? 0))
  const total = skewed.reduce((sum, p) => sum + (p.skewRejects ?? 0), 0)

  return (
    <section className={`panel skew-reject-card ${total ? 'skew-reject-card--active' : ''}`}>
      <div className="skew-reject-card__head">
        <h3>Clock skew rejects</h3>
        <p className="skew-reject-card__value mono">{total}</p>
      </div>
      <p className="skew-reject-card__sub">
        Frames failed quality because measurement time was too far from now
        (see <code>MAX_CLOCK_SKEW</code>). Same counts already appear in logs.
      </p>
      {!skewed.length ? (
        <p className="skew-reject-card__empty">No skew rejects since PDC start.</p>
      ) : (
        <ul className="skew-reject-card__list">
          {skewed.map((p) => (
            <li key={p.name}>
              <span className="mono">{p.meta?.station || p.name}</span>
              <span className="mono">{p.skewRejects}</span>
              <span className="skew-reject-card__reason" title={p.lastQualityReject}>
                {p.lastQualityReject || 'timestamp skew'}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
