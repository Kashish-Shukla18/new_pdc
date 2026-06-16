import { useEffect, useMemo, useRef, useState } from 'react'
import type { RttHistoryPoint, RttStream } from '../types/connectivity'
import type { ConnectivityRow } from '../types/dashboard'
import { appendRttPoint, RTT_WINDOW, seedRttHistory, topRttStreams } from '../utils/connectivity'

export function useRttHistory(connectivityRows: ConnectivityRow[], tick: string, isPaused: boolean) {
  const historyRef = useRef<RttHistoryPoint[]>([])
  const streamsRef = useRef<RttStream[]>([])
  const [history, setHistory] = useState<RttHistoryPoint[]>([])
  const [streams, setStreams] = useState<RttStream[]>([])

  const topStreams = useMemo(() => topRttStreams(connectivityRows), [connectivityRows])
  const streamSignature = topStreams.map((stream) => stream.name).join('|')

  useEffect(() => {
    const currentSignature = streamsRef.current.map((stream) => stream.name).join('|')
    if (currentSignature === streamSignature && historyRef.current.length === RTT_WINDOW) {
      return
    }

    streamsRef.current = topStreams
    const seeded = seedRttHistory(topStreams)
    historyRef.current = seeded
    setStreams(topStreams)
    setHistory(seeded)
  }, [streamSignature, topStreams])

  useEffect(() => {
    if (isPaused || !streamsRef.current.length || historyRef.current.length !== RTT_WINDOW) {
      return
    }

    const next = appendRttPoint(historyRef.current, streamsRef.current, connectivityRows)
    historyRef.current = next
    setHistory(next)
  }, [tick, isPaused, connectivityRows])

  return { rttHistory: history, rttStreams: streams }
}
