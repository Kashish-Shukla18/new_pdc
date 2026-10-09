import { useEffect, useRef, useState } from 'react'
import type { PMUWithMeta } from '../types/dashboard'
import { dynamicRelativeDelay, type DynamicRelativeDelay } from '../utils/pmu'

export type DynamicRelativeDelayPoint = {
  ts: number
  label: string
  spreadMs: number
  parseAvgMs: number
  totalMs: number
}

const WINDOW = 60

export function useArrivalSpreadHistory(pmus: PMUWithMeta[], tick: string, isPaused: boolean) {
  const historyRef = useRef<DynamicRelativeDelayPoint[]>([])
  const [history, setHistory] = useState<DynamicRelativeDelayPoint[]>([])
  const [latest, setLatest] = useState<DynamicRelativeDelay | null>(() => dynamicRelativeDelay(pmus))

  useEffect(() => {
    const sample = dynamicRelativeDelay(pmus)
    setLatest(sample)
    if (isPaused || !sample) return

    const now = Date.now()
    const point: DynamicRelativeDelayPoint = {
      ts: now,
      label: new Date(now).toLocaleTimeString(),
      spreadMs: sample.spreadMs,
      parseAvgMs: sample.parseAvgMs,
      totalMs: sample.totalMs,
    }
    const next = [...historyRef.current, point].slice(-WINDOW)
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, pmus])

  return { spreadHistory: history, spreadLatest: latest }
}
