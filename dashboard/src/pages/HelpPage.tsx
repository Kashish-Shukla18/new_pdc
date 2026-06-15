import { FAQ_ITEMS } from '../constants'
import { useDashboardContext } from '../context/DashboardContext'

export function HelpPage() {
  const { helpQuery, setHelpQuery, filteredHelp, openFaq, setOpenFaq } = useDashboardContext()

  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h3>Help & Support</h3>
          <p>Updated for dynamic live React dashboard workflow.</p>
        </div>
      </div>
      <div className="device-filter-row">
        <input
          value={helpQuery}
          onChange={(event) => setHelpQuery(event.target.value)}
          placeholder="Search help topics"
        />
      </div>
      <div className="help-list-react">
        {filteredHelp.map((item) => (
          <article key={item.title} className="help-card-react">
            <h4>{item.title}</h4>
            <p>{item.content}</p>
          </article>
        ))}
      </div>

      <div className="faq-list-react">
        {FAQ_ITEMS.map((item, idx) => (
          <article key={item.q} className="faq-item-react">
            <button type="button" onClick={() => setOpenFaq((current) => (current === idx ? null : idx))}>
              {item.q}
            </button>
            {openFaq === idx && <p>{item.a}</p>}
          </article>
        ))}
      </div>
    </section>
  )
}
