import { useEffect, useState } from 'react'

export function useClock() {
  const [clockLabel, setClockLabel] = useState('')

  useEffect(() => {
    const tickClock = () => {
      setClockLabel(
        new Date().toLocaleTimeString('en-IN', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        }),
      )
    }
    tickClock()
    const timer = window.setInterval(tickClock, 1000)
    return () => window.clearInterval(timer)
  }, [])

  return clockLabel
}
