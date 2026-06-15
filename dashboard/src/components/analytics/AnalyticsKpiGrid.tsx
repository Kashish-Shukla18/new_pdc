import type { AnalyticsKpi } from '../../types/analytics'

type Props = {
  items: AnalyticsKpi[]
}

export function AnalyticsKpiGrid({ items }: Props) {
  return (
    <section className="kpi-grid analytics-kpi-grid">
      {items.map((item) => (
        <article key={item.label} className={`kpi-card analytics-kpi ${item.tone}`}>
          <p className="kpi-meta">{item.label}</p>
          <h2 className="kpi-value">{item.value}</h2>
          <p className="kpi-sub">{item.sub}</p>
        </article>
      ))}
    </section>
  )
}
