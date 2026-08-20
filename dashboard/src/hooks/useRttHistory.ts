import { useEffect, useMemo, useRef, useState } from 'react'
import type { RttHistoryPoint, RttStream } from '../types/connectivity'
import type { ConnectivityRow } from '../types/dashboard'
import { appendRttPoint, topRttStreams } from '../utils/connectivity'

export function useRttHistory(connectivityRows: ConnectivityRow[], tick: string, isPaused: boolean) {
  const historyRef = useRef<RttHistoryPoint[]>([])
  const streamsRef = useRef<RttStream[]>([])
  const [history, setHistory] = useState<RttHistoryPoint[]>([])
  const [streams, setStreams] = useState<RttStream[]>([])

  const topStreams = useMemo(() => topRttStreams(connectivityRows), [connectivityRows])
  const streamSignature = topStreams.map((stream) => stream.name).join('|')

  useEffect(() => {
    // Keep history when the set of names is unchanged; only reset if membership changes.
    const currentSignature = streamsRef.current.map((stream) => stream.name).join('|')
    streamsRef.current = topStreams
    setStreams(topStreams)
    if (currentSignature && currentSignature !== streamSignature) {
      historyRef.current = []
      setHistory([])
    }
  }, [streamSignature, topStreams])

  useEffect(() => {
    if (isPaused || !streamsRef.current.length) return

    const next = appendRttPoint(historyRef.current, streamsRef.current, connectivityRows)
    if (next === historyRef.current) return
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, connectivityRows])

  return { rttHistory: history, rttStreams: streams }
}
