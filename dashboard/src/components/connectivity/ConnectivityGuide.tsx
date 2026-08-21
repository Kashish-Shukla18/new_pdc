export function ConnectivityGuide() {
  return (
    <section className="panel connectivity-guide">
      <div className="panel-head">
        <div>
          <h3>What these numbers mean</h3>
          <p className="panel-sub">How to read wait time vs real PDC processing</p>
        </div>
      </div>
      <div className="connectivity-guide-grid">
        <article>
          <h4>End-to-end cycle (new chart)</h4>
          <p>
            Plots <strong>PMU wait + parse + other pipeline</strong>. At 50 FPS the wait is ~20 ms
            (time until the next frame arrives). That 20 ms is the PMU sample interval — not PDC
            parse time. Parse is usually ~0.5 ms; dashboard/quality add a little more.
          </p>
        </article>
        <article>
          <h4>PMU wait (~20 ms @ 50 FPS)</h4>
          <p>
            Measured as <code>tcp_wait</code> / inter-arrival. The PDC is idle on the socket waiting
            for the next DATA frame. Formula: 1000 ms ÷ FPS ≈ interval.
          </p>
        </article>
        <article>
          <h4>Pipeline (e2e recv → dashboard)</h4>
          <p>
            Time after the frame is fully received until it is recorded for the UI. This is the real
            processing path (parse + quality + dashboard record), typically ~1 ms or less.
          </p>
        </article>
        <article>
          <h4>Stream latency chart</h4>
          <p>
            Per-PMU plot of receive→dashboard only (no PMU wait). Use it to compare streams; use the
            cycle chart to see wait + processing together.
          </p>
        </article>
        <article>
          <h4>Packet loss %</h4>
          <p>
            Quality rejects ÷ total frames. High values mean STAT/data-error issues, not the 20 ms
            wait.
          </p>
        </article>
        <article>
          <h4>Availability %</h4>
          <p>
            Measured frame rate vs expected rate. Drops when the peer reconnects or stops sending.
          </p>
        </article>
      </div>
    </section>
  )
}
