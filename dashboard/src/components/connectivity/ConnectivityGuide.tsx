export function ConnectivityGuide() {
  return (
    <section className="panel connectivity-guide">
      <div className="panel-head">
        <div>
          <h3>What these numbers mean</h3>
          <p className="panel-sub">How to read the connectivity page</p>
        </div>
      </div>
      <div className="connectivity-guide-grid">
        <article>
          <h4>Stream latency (chart)</h4>
          <p>
            Time from when the PDC <strong>finishes receiving</strong> a C37.118 DATA frame until that
            reading is recorded for the dashboard. Typical healthy values are under a few milliseconds
            on this machine. This is <strong>not</strong> network ping RTT to the PMU, and it is{' '}
            <strong>not</strong> PMU clock skew (SOC vs wall clock).
          </p>
        </article>
        <article>
          <h4>Latency (matrix)</h4>
          <p>
            Same metric as the chart, latest sample per PMU. If a hop sample is briefly missing, the
            chart holds the previous value so the line stays continuous.
          </p>
        </article>
        <article>
          <h4>Jitter</h4>
          <p>
            Spread of recent frequency samples (a stability proxy), not classical network packet
            jitter. Higher values mean the frequency series is noisier.
          </p>
        </article>
        <article>
          <h4>Packet loss %</h4>
          <p>
            Share of frames rejected by the quality gate versus total frames seen. High values point
            to STAT/data-error issues or a bad path — not silent UDP drops the PDC never saw.
          </p>
        </article>
        <article>
          <h4>Availability %</h4>
          <p>
            How close the measured frame rate is to the expected rate for that stream. Low
            availability usually means reconnects, idle gaps, or the peer closing TCP.
          </p>
        </article>
        <article>
          <h4>Why a line used to disappear</h4>
          <p>
            The old chart dropped a point whenever a hop sample was missing or the “top streams”
            list reshuffled. The chart now uses a stable stream list and carries forward the last
            good latency so traces stay continuous.
          </p>
        </article>
      </div>
    </section>
  )
}
