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

export function formatHopMs(ms: number | undefined | null) {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—'
  if (ms === 0) return '0 ms'
  if (ms < 1) return `${ms.toFixed(2)} ms`
  if (ms < 100) return `${ms.toFixed(1)} ms`
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

export function hopTone(ms: number, group: string): 'ok' | 'warn' | 'bad' {
  if (!Number.isFinite(ms) || ms <= 0) return 'ok'
  if (group === 'idle') return 'ok'
  if (group === 'connection') {
    if (ms > 2000) return 'bad'
    if (ms > 500) return 'warn'
    return 'ok'
  }
  if (group === 'e2e' || group === 'dashboard') {
    if (ms > 150) return 'bad'
    if (ms > 50) return 'warn'
    return 'ok'
  }
  if (ms > 50) return 'bad'
  if (ms > 20) return 'warn'
  return 'ok'
}
