import { useEffect, useRef, useState } from 'react'

/** Bounds come from aligner/buffer.go via GET /conversation/aligner-buffer-capacity. */
type CapacityMeta = {
  capacity: number
  default: number
  min: number
  max: number
}

type Props = {
  /** Live value from conversation state (aligner.maxOpen) — display sync only. */
  liveCapacity?: number
  onApplied?: (capacity: number) => void
  onError?: (message: string) => void
}

export function BufferCapacityControl({ liveCapacity, onApplied, onError }: Props) {
  const [meta, setMeta] = useState<CapacityMeta | null>(null)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  /** Ignore stale conversation polls until they catch up to this applied value. */
  const pendingRef = useRef<number | null>(null)
  const focusedRef = useRef(false)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const res = await fetch('/conversation/aligner-buffer-capacity')
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const body = (await res.json()) as CapacityMeta
        if (cancelled) return
        setMeta(body)
        if (!focusedRef.current) {
          setDraft(String(body.capacity))
        }
      } catch (err) {
        if (!cancelled) {
          onError?.(err instanceof Error ? err.message : 'Failed to load buffer capacity')
        }
      }
    })()
    return () => {
      cancelled = true
    }
    // intentionally once on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Sync from live poll only when not editing and not waiting on a stale poll.
  useEffect(() => {
    if (typeof liveCapacity !== 'number' || liveCapacity <= 0 || !meta) return
    if (saving || focusedRef.current) return

    if (pendingRef.current != null) {
      if (liveCapacity !== pendingRef.current) return
      pendingRef.current = null
    }

    setMeta((prev) => (prev ? { ...prev, capacity: liveCapacity } : prev))
    setDraft(String(liveCapacity))
  }, [liveCapacity, saving, meta])

  const apply = async () => {
    const n = Number(draft)
    if (!Number.isFinite(n) || !Number.isInteger(n)) {
      onError?.('Enter a whole number for buffer capacity')
      return
    }
    setSaving(true)
    try {
      const res = await fetch('/conversation/aligner-buffer-capacity', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ capacity: n }),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `HTTP ${res.status}`)
      }
      const body = (await res.json()) as CapacityMeta
      pendingRef.current = body.capacity
      setMeta(body)
      setDraft(String(body.capacity))
      onApplied?.(body.capacity)
    } catch (err) {
      onError?.(err instanceof Error ? err.message : 'Failed to update buffer capacity')
    } finally {
      setSaving(false)
    }
  }

  const hint = meta
    ? `default ${meta.default} · ${meta.min}–${meta.max}`
    : 'loading…'

  return (
    <div className="buffer-capacity-control" title="Timestamps kept per PMU in the aligner RAM buffer">
      <label htmlFor="align-buffer-capacity">
        Buffer capacity
        <span className="buffer-capacity-hint">{hint}</span>
      </label>
      <div className="buffer-capacity-row">
        <input
          id="align-buffer-capacity"
          type="number"
          min={meta?.min}
          max={meta?.max}
          step={1}
          value={draft}
          disabled={saving || !meta}
          onFocus={() => {
            focusedRef.current = true
          }}
          onBlur={() => {
            focusedRef.current = false
          }}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              void apply()
            }
          }}
        />
        <button type="button" className="btn ghost" disabled={saving || !meta} onClick={() => void apply()}>
          {saving ? 'Saving…' : 'Apply'}
        </button>
      </div>
    </div>
  )
}
