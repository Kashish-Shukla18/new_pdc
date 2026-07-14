import { useEffect, useRef, useState } from 'react'
import type { PMUWithMeta, TrendPoint } from '../types/dashboard'
import { appendFrameTrendPoint, seedFrameTrendHistory } from '../utils/frameChart'

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
    if (seededNameRef.current !== pmuName) {
      seededNameRef.current = pmuName
      const seeded = seedFrameTrendHistory(pmu)
      historyRef.current = seeded
      setHistory(seeded)
      return
    }
  }, [pmu, pmuName])

  useEffect(() => {
    if (isPaused || !pmu) return
    const next = appendFrameTrendPoint(historyRef.current, pmu)
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, pmu])

  return history
}
