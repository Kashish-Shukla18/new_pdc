import type { DashboardState, NewPMUForm } from '../types/dashboard'

export const INDIA_CENTER: [number, number] = [22.5, 80.5]

export const CHART_COLORS = ['#4de0ff', '#7dff93', '#ffd166', '#ff719a', '#ab82ff', '#ffa84d']

export const emptyDashboardState: DashboardState = {
  nowUtc: new Date().toISOString(),
  pmus: [],
  eventCount: 0,
}

export const defaultNewPMU: NewPMUForm = {
  name: '',
  ip: '',
  port: 4712,
  idcode: 1,
  region: 'FIELD',
  protocol: 'tcp',
  lat: 0,
  lon: 0,
}

export const HELP_SECTIONS = [
  {
    title: 'Getting Started',
    content: 'This dashboard shows live telemetry from registered field PMUs via the PDC backend.',
  },
  {
    title: 'Data Frames',
    content: 'Use Data Frames to inspect per-PMU SOC/FRACSEC, phasor values, and frequency in near real-time.',
  },
  {
    title: 'Connectivity',
    content: 'Connectivity metrics are computed from live frame rates, quality rejects, and trend variance.',
  },
]

export const FAQ_ITEMS = [
  {
    q: 'Why are some PMUs offline?',
    a: 'This means they are configured in the database, but no live data is currently streaming.',
  },
  {
    q: 'Where does data come from?',
    a: 'The dashboard polls /conversation/state and subscribes to /conversation/events (SSE) exposed by the Go backend.',
  },
  {
    q: 'How do I pause updates?',
    a: 'Use Pause in the header. It pauses both polling and SSE consumption in this React dashboard.',
  },
]
