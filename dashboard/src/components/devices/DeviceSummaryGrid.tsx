import type { DeviceSummary } from '../../utils/devices'

type Props = {
  items: DeviceSummary[]
}

export function DeviceSummaryGrid({ items }: Props) {
  return (
    <section className="kpi-grid device-summary-grid">
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
