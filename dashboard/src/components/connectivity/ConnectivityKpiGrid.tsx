import type { ConnectivityKpi } from '../../types/connectivity'

type Props = {
  items: ConnectivityKpi[]
}

export function ConnectivityKpiGrid({ items }: Props) {
  return (
    <section className="kpi-grid connectivity-kpi-grid">
      {items.map((item) => (
        <article key={item.label} className={`kpi-card ${item.tone}`}>
          <p className="kpi-meta">{item.label}</p>
          <h2 className="kpi-value">{item.value}</h2>
          <p className="kpi-sub">{item.subtext}</p>
        </article>
      ))}
    </section>
  )
}
