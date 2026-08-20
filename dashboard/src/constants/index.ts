import type { DashboardState, NewPMUForm } from '../types/dashboard'

export const INDIA_CENTER: [number, number] = [22.5, 80.5]

export const CHART_COLORS = ['#4de0ff', '#7dff93', '#ffd166', '#ff719a', '#ab82ff', '#ffa84d']

export const emptyDashboardState: DashboardState = {
  nowUtc: new Date().toISOString(),
  pmus: [],
  eventCount: 0,
  latency: { slowestStage: '', slowestLabel: '', slowestAvgMs: 0, stages: [] },
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
    content:
      'Register PMUs under Device Inventory, check Overview for fleet health, Data Frames for decode detail, Analytics for V/I trends and advisories.',
  },
  {
    title: 'Overview',
    content:
      'Fleet KPIs, frequency and ROCOF charts, map from registration lat/lon, regional health, and live alerts.',
  },
  {
    title: 'Device Inventory',
    content:
      'Add or edit PMUs (IP, IDCODE, region, coordinates). Offline means configured but not streaming.',
  },
  {
    title: 'Data Frames',
    content:
      'CFG-2 profile, live DATA lines (SOC/FRACSEC/STAT/DIG), VA–IC / analogs / digital bits, and a frequency trend for one PMU.',
  },
  {
    title: 'Connectivity',
    content:
      'Measured hop times (TCP dial, handshake, Kafka, parse, dashboard JSON, UI refresh) plus loss and availability. The slowest function is highlighted.',
  },
  {
    title: 'Analytics',
    content:
      'V/I magnitude trends, separate V and I phasor diagrams, inter-PMU VA angle Δ (≥2 PMUs), and advisories from live DATA.',
  },
  {
    title: 'Documentation',
    content:
      'Page-by-page reference under Resources → Documentation (data sources and what each panel shows).',
  },
]

export const FAQ_ITEMS = [
  {
    q: 'Why are some PMUs offline?',
    a: 'They are registered, but no DATA frames have arrived recently (~3 s).',
  },
  {
    q: 'Where does data come from?',
    a: 'IEEE C37.118 CFG-2 and DATA frames are decoded by the PDC; the dashboard shows that live stream state and registration.',
  },
  {
    q: 'How do I pause updates?',
    a: 'Use Pause in the header (and Pause stream on Data Frames). That freezes live updates in this UI.',
  },
  {
    q: 'Why is the map empty on Overview?',
    a: 'Set non-zero latitude and longitude when registering. (0, 0) is off the India view.',
  },
  {
    q: 'What does Worst Δf mean with one PMU?',
    a: 'That PMU’s current frequency minus CFG FNOM. With many PMUs it is the largest |Δf| online right now.',
  },
  {
    q: 'What is STAT data errors?',
    a: 'How many online PMUs have DATA STAT bits 15–14 ≠ 00 (error / test / invalid) on the latest frame.',
  },
  {
    q: 'Why is Analytics angle Δ empty?',
    a: 'Inter-PMU angle needs at least two online streams. V/I charts and the phasor diagrams still work with one PMU.',
  },
]
