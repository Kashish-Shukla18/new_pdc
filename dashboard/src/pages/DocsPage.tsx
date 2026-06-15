export function DocsPage() {
  return (
    <section className="panel help-card-react">
      <h3>Documentation</h3>
      <p>This React dashboard now maps fully dynamic data from InfluxDB across all sections.</p>
      <ul>
        <li>Live source: /conversation/state (poll 1s) and /conversation/events (SSE).</li>
        <li>Config source: /api/pmus mapped from InfluxDB.</li>
        <li>Overview: map, alerts, frequency charts from live trends.</li>
        <li>Data Frames: selected PMU frame stream, CFG and phasor values.</li>
        <li>Connectivity: latency/jitter/loss/availability derived per simulator PMU.</li>
        <li>Analytics: angle deltas and modal hints derived from live phasor and trend data.</li>
      </ul>
    </section>
  )
}
