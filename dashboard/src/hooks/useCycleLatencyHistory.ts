import { useEffect, useRef, useState } from 'react'
import type { PMUWithMeta } from '../types/dashboard'
import { averageCycleLatency } from '../utils/pmu'

export type CycleHistoryPoint = {
  ts: number
  label: string
  pmuWait: number
  parse: number
  pipelineRest: number
  pipeline: number
  total: number
}

const WINDOW = 60

export function useCycleLatencyHistory(pmus: PMUWithMeta[], tick: string, isPaused: boolean) {
  const historyRef = useRef<CycleHistoryPoint[]>([])
  const [history, setHistory] = useState<CycleHistoryPoint[]>([])
  const [latest, setLatest] = useState(() => averageCycleLatency(pmus))

  useEffect(() => {
    const cycle = averageCycleLatency(pmus)
    setLatest(cycle)
    if (isPaused || !cycle) return

    const now = Date.now()
    const point: CycleHistoryPoint = {
      ts: now,
      label: new Date(now).toLocaleTimeString(),
      pmuWait: cycle.pmuWait,
      parse: cycle.parse,
      pipelineRest: cycle.pipelineRest,
      pipeline: cycle.pipeline,
      total: cycle.total,
    }
    const next = [...historyRef.current, point].slice(-WINDOW)
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, pmus])

  return { cycleHistory: history, cycleLatest: latest }
}
