export function round(v: number, digits = 2) {
  return Number(v.toFixed(digits))
}

export function formatTS(ts: number) {
  return new Date(ts).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function ageText(iso: string) {
  if (!iso) return '--'
  const deltaMs = Date.now() - new Date(iso).getTime()
  if (deltaMs < 2000) return `${Math.max(1, Math.round(deltaMs))} ms ago`
  if (deltaMs < 60000) return `${round(deltaMs / 1000, 1)} s ago`
  return `${Math.floor(deltaMs / 60000)} min ago`
}
