import { useEffect, useMemo, useState } from 'react'
import { DOCS_INTRO, PAGE_DOCS, type PageDoc } from '../constants/docs'

function DocBlock({ doc }: { doc: PageDoc }) {
  return (
    <article id={`doc-${doc.id}`} className="docs-section panel">
      <header className="docs-section-head">
        <p className="docs-kicker">{doc.id}</p>
        <h3>{doc.title}</h3>
        <p className="docs-purpose">{doc.purpose}</p>
      </header>

      <div className="docs-block">
        <h4>Inputs</h4>
        <ul className="docs-list">
          {doc.dataSources.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </div>

      <div className="docs-section-grid">
        {doc.sections.map((section) => (
          <div key={section.heading} className="docs-block">
            <h4>{section.heading}</h4>
            <ul className="docs-list">
              {section.items.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
        ))}
      </div>

      {doc.notes && doc.notes.length > 0 && (
        <div className="docs-block docs-notes">
          <h4>Notes</h4>
          <ul className="docs-list">
            {doc.notes.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </div>
      )}
    </article>
  )
}

export function DocsPage() {
  const [query, setQuery] = useState('')
  const [activeId, setActiveId] = useState(PAGE_DOCS[0]?.id ?? 'overview')

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return PAGE_DOCS
    return PAGE_DOCS.filter((doc) => {
      const blob = [
        doc.title,
        doc.purpose,
        ...doc.dataSources,
        ...doc.sections.flatMap((s) => [s.heading, ...s.items]),
        ...(doc.notes ?? []),
      ]
        .join(' ')
        .toLowerCase()
      return blob.includes(q)
    })
  }, [query])

  useEffect(() => {
    if (!filtered.some((d) => d.id === activeId) && filtered[0]) {
      setActiveId(filtered[0].id)
    }
  }, [filtered, activeId])

  const scrollTo = (id: string) => {
    setActiveId(id)
    document.getElementById(`doc-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <section className="docs-page">
      <header className="panel docs-intro">
        <div className="docs-intro-copy">
          <p className="docs-kicker">Resources</p>
          <h2>{DOCS_INTRO.title}</h2>
          <p className="docs-intro-summary">{DOCS_INTRO.summary}</p>
        </div>
      </header>

      <div className="docs-layout">
        <aside className="panel docs-nav">
          <div className="docs-nav-head">
            <h3>On this page</h3>
            <p>{filtered.length} of {PAGE_DOCS.length} sections</p>
          </div>
          <label className="docs-search-wrap">
            <span className="sr-only">Search documentation</span>
            <input
              className="docs-search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              aria-label="Search documentation"
            />
          </label>
          <nav className="docs-nav-list" aria-label="Documentation sections">
            {filtered.map((doc) => (
              <button
                key={doc.id}
                type="button"
                className={`docs-nav-item ${activeId === doc.id ? 'active' : ''}`}
                onClick={() => scrollTo(doc.id)}
              >
                <span className="docs-nav-title">{doc.title}</span>
                <span className="docs-nav-sub">{doc.purpose}</span>
              </button>
            ))}
            {filtered.length === 0 && (
              <p className="docs-nav-empty">No matches</p>
            )}
          </nav>
        </aside>

        <div className="docs-content">
          {filtered.length === 0 ? (
            <div className="panel docs-empty">
              <h3>No matches</h3>
              <p>Try another search term, or clear the filter to see all pages.</p>
              <button type="button" className="btn" onClick={() => setQuery('')}>
                Clear search
              </button>
            </div>
          ) : (
            filtered.map((doc) => <DocBlock key={doc.id} doc={doc} />)
          )}
        </div>
      </div>
    </section>
  )
}
