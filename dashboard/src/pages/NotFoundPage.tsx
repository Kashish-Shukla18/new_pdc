export function NotFoundPage() {
  return (
    <main className="not-found-page">
      <section className="panel not-found-card">
        <p className="eyebrow">404</p>
        <h1>Page not found</h1>
        <p>The requested dashboard page does not exist.</p>
        <a className="btn primary" href="/">Return to dashboard</a>
      </section>
    </main>
  )
}
