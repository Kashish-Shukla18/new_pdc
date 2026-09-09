type Props = {
  size?: number
}

export function Logo({ size = 42 }: Props) {
  return (
    <div className="app-logo" style={{ width: size, height: size }} aria-hidden="true">
      <svg viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id="pdc-ring" x1="8" y1="4" x2="40" y2="44" gradientUnits="userSpaceOnUse">
            <stop stopColor="#2563eb" />
            <stop offset="0.45" stopColor="#15803d" />
            <stop offset="1" stopColor="#6d28d9" />
          </linearGradient>
          <linearGradient id="pdc-wave" x1="10" y1="24" x2="38" y2="24" gradientUnits="userSpaceOnUse">
            <stop stopColor="#60a5fa" />
            <stop offset="1" stopColor="#2563eb" />
          </linearGradient>
        </defs>
        <rect x="2" y="2" width="44" height="44" rx="12" fill="#071018" stroke="url(#pdc-ring)" strokeWidth="1.5" />
        <circle cx="24" cy="24" r="14" stroke="rgba(77, 200, 240, 0.25)" strokeWidth="1" />
        <path
          d="M10 26 C14 18, 18 30, 22 22 S30 28, 34 20 S40 26, 38 26"
          stroke="url(#pdc-wave)"
          strokeWidth="2.2"
          strokeLinecap="round"
          fill="none"
        />
        <circle cx="24" cy="24" r="3.5" fill="#15803d" />
        <path d="M24 10 V14 M24 34 V38 M10 24 H14 M34 24 H38" stroke="rgba(93, 232, 170, 0.45)" strokeWidth="1.2" strokeLinecap="round" />
      </svg>
    </div>
  )
}
