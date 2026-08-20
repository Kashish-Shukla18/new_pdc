import type { DisplayPhasor } from '../../utils/phasorLabels'
import { PHASOR_I_COLORS, PHASOR_V_COLORS } from '../../utils/analyticsColors'
import { round } from '../../utils/format'

type Kind = 'voltage' | 'current'

type Props = {
  phasors: DisplayPhasor[]
  pmuName: string
  kind: Kind
}

function toXY(mag: number, angleDeg: number, scale: number) {
  const rad = (angleDeg * Math.PI) / 180
  return { x: mag * scale * Math.cos(rad), y: mag * scale * Math.sin(rad) }
}

const PHASE_FALLBACK = '#9eb0c5'

function colorFor(label: string, kind: Kind) {
  if (kind === 'voltage') return PHASOR_V_COLORS[label] ?? PHASE_FALLBACK
  return PHASOR_I_COLORS[label] ?? PHASE_FALLBACK
}

export function PhasorDiagram({ phasors, pmuName, kind }: Props) {
  const size = 300
  const cx = size / 2
  const cy = size / 2
  const plotR = 118

  const vectors = phasors.filter((p) =>
    kind === 'voltage' ? p.label.startsWith('V') : p.label.startsWith('I'),
  )

  const maxMag = Math.max(...vectors.map((p) => p.magnitude), 1e-9)
  const scale = plotR / maxMag
  const ticks = [0.33, 0.66, 1]
  const title = kind === 'voltage' ? 'Voltage Phasor Diagram' : 'Current Phasor Diagram'

  return (
    <div className="panel chart-panel analytics-diagram-panel">
      <div className="panel-head">
        <div>
          <h3>{title}</h3>
          <p className="panel-sub">{vectors.length ? pmuName : 'Waiting for phasor channels'}</p>
        </div>
      </div>
      {!vectors.length ? (
        <div className="frame-box-react" style={{ margin: 12 }}>
          No {kind} phasor channels yet for this PMU.
        </div>
      ) : (
        <div className="phasor-diagram-wrap">
          <svg viewBox={`0 0 ${size} ${size}`} className="phasor-diagram-svg" role="img">
            {ticks.map((t) => (
              <circle
                key={t}
                cx={cx}
                cy={cy}
                r={plotR * t}
                fill="none"
                stroke="rgba(158, 176, 197, 0.12)"
                strokeWidth={1}
              />
            ))}
            <line x1={cx - plotR} y1={cy} x2={cx + plotR} y2={cy} stroke="rgba(158, 176, 197, 0.2)" />
            <line x1={cx} y1={cy - plotR} x2={cx} y2={cy + plotR} stroke="rgba(158, 176, 197, 0.2)" />

            {vectors.map((p) => {
              const { x, y } = toXY(p.magnitude, p.angleDeg, scale)
              const x2 = cx + x
              const y2 = cy - y
              const color = colorFor(p.label, kind)
              return (
                <g key={`${p.label}-${p.cfgName}`}>
                  <title>{`${p.label}${p.cfgName !== p.label ? ` (${p.cfgName})` : ''}: ${round(p.magnitude, 2)} ∠ ${round(p.angleDeg, 1)}°`}</title>
                  <line
                    x1={cx}
                    y1={cy}
                    x2={x2}
                    y2={y2}
                    stroke={color}
                    strokeWidth={2.1}
                    strokeOpacity={0.9}
                  />
                  <circle cx={x2} cy={y2} r={2.8} fill={color} />
                  <text
                    x={x2 + (x >= 0 ? 5 : -5)}
                    y={y2 + (y >= 0 ? -5 : 11)}
                    fill={color}
                    fontSize={10}
                    fontWeight={600}
                    textAnchor={x >= 0 ? 'start' : 'end'}
                  >
                    {p.label}
                  </text>
                </g>
              )
            })}
            <circle cx={cx} cy={cy} r={2.2} fill="#8796a6" />
          </svg>

          <div className="phasor-diagram-legend">
            <div className="phasor-legend-group">
              {vectors.map((p) => (
                <div key={p.label} className="phasor-legend-chip" title={p.cfgName !== p.label ? p.cfgName : undefined}>
                  <i style={{ background: colorFor(p.label, kind) }} />
                  <span>
                    <b>{p.label}</b> {round(p.magnitude, 1)}∠{round(p.angleDeg, 0)}°
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}