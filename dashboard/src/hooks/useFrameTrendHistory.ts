import { useEffect, useRef, useState } from 'react'
import type { PMUWithMeta, TrendPoint } from '../types/dashboard'
import { appendFrameTrendPoint, FRAME_TREND_WINDOW, seedFrameTrendHistory } from '../utils/frameChart'

export function useFrameTrendHistory(
  pmu: PMUWithMeta | undefined,
  pmuName: string,
  tick: string,
  isPaused: boolean,
) {
  const historyRef = useRef<TrendPoint[]>([])
  const [history, setHistory] = useState<TrendPoint[]>([])

  const seededNameRef = useRef('')

  useEffect(() => {
    if (!pmu) return
    if (seededNameRef.current === pmuName && historyRef.current.length === FRAME_TREND_WINDOW) {
      return
    }
    seededNameRef.current = pmuName
    const seeded = seedFrameTrendHistory(pmu)
    historyRef.current = seeded
    setHistory(seeded)
  }, [pmu, pmuName])

  useEffect(() => {
    if (isPaused || !pmu || historyRef.current.length !== FRAME_TREND_WINDOW) return

    const next = appendFrameTrendPoint(historyRef.current, pmu)
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, pmu])

  return history
}
