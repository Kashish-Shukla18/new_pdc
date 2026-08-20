export type DocSection = {
  heading: string
  items: string[]
}

export type PageDoc = {
  id: string
  title: string
  purpose: string
  dataSources: string[]
  sections: DocSection[]
  notes?: string[]
}

export const DOCS_INTRO = {
  title: 'PDC Operator Dashboard',
  summary:
    'Live console for IEEE C37.118 PMUs through the PDC. ' +
    'CFG-2 and DATA frames are decoded in the backend; this UI shows that state for monitoring.',
}

export const PAGE_DOCS: PageDoc[] = [
  {
    id: 'overview',
    title: 'Overview',
    purpose:
      'Fleet view: stream health KPIs, frequency and ROCOF charts, map, alerts, and regional roll-up.',
    dataSources: [
      'Live PMU stream state — connection, FPS, frequency / Δf / ROCOF trends, STAT quality',
      'Registered PMU locations — region and coordinates for map markers',
      'Handshake and stream events for the alert feed',
    ],
    sections: [
      {
        heading: 'KPI cards',
        items: [
          'Total PMUs — registered devices',
          'Offline — no frames in the last ~3 s',
          'Frames / sec — sum of approxFps',
          'Worst Δf — largest |frequency − CFG FNOM| among online PMUs (now)',
          'Max |ROCOF| — largest |ROCOF| among online PMUs (now)',
          'STAT data errors — online PMUs whose latest STAT bits 15–14 ≠ 00',
        ],
      },
      {
        heading: 'Live streams',
        items: [
          'Chips select which PMUs appear on frequency and ROCOF charts',
          'At least one stream must stay selected',
        ],
      },
      {
        heading: 'System frequency',
        items: [
          'Absolute — Hz with FNOM reference band from CFG-2',
          'Δf — frequency − FNOM',
          'About the last 120 trend points per PMU',
        ],
      },
      {
        heading: 'ROCOF',
        items: [
          'Hz/s for the same selected streams',
          'Zero reference line at 0 Hz/s',
          'Main ROCOF time series for the fleet (Data Frames shows ROCOF only as a scalar on the live line)',
        ],
      },
      {
        heading: 'Map, alerts, regions',
        items: [
          'Map markers use lat/lon from registration; All / Issues filters',
          'Issues = offline or quality-reject rate > 1%',
          'Alerts — offline/degraded streams and recent stream events',
          'Regional health — connected/total and availability % by region',
        ],
      },
    ],
    notes: [
      'With one online PMU, Worst Δf and Max |ROCOF| are that device’s latest values.',
      'Register non-zero lat/lon or the marker will not sit on the India map.',
    ],
  },
  {
    id: 'devices',
    title: 'Device Inventory',
    purpose:
      'Register and edit PMUs — IP, IDCODE, region, coordinates — and see live connection status.',
    dataSources: [
      'Saved PMU registration (name, IP, port, IDCODE, region, coordinates)',
      'Live connection status, frame counts, and quality rejects',
    ],
    sections: [
      {
        heading: 'Inventory',
        items: [
          'Search by name / IP; filter by region and status',
          'Healthy / Degraded / Offline from connection and loss',
          'Open a row for the detail drawer; edit from Devices',
        ],
      },
      {
        heading: 'Register / Edit',
        items: [
          'Name, IP, port, IDCODE, protocol (tcp/udp), region',
          'Latitude / longitude for Overview map',
          'Changes are saved to registration; PDC must reconnect/reload to apply network settings',
        ],
      },
    ],
    notes: [
      'Connectivity latency is measured pipeline time (receive → dashboard) when hop samples exist.',
    ],
  },
  {
    id: 'dataframes',
    title: 'Data Frames',
    purpose:
      'Per-PMU frame inspector: CFG-2 profile, live DATA summary, channel cards, and frequency trend.',
    dataSources: [
      'CFG-2 profile and latest DATA frame (SOC, FRACSEC, STAT, DIG, channels)',
      'Frequency trend for the selected PMU',
      'New scroll lines when SOC / FRACSEC / frame count change',
    ],
    sections: [
      {
        heading: 'CFG-2 panel',
        items: [
          'SYNC, IDCODE, station, FNOM, DATA_RATE, FORMAT, CFG_CNT',
          'Phasor / analog channel names and digital word count from handshake',
          'Live FPS from received frames; HDR text if the device sent a header frame',
        ],
      },
      {
        heading: 'Live DATA panel',
        items: [
          'Summary: STAT (and flags), DIG, TQ, Δf',
          'Scrolling lines: SOC, FRACSEC, F, ROCOF, STAT, DIG',
          'Pause stream stops appending lines',
        ],
      },
      {
        heading: 'Phasors, analogs, digital',
        items: [
          'Cards sorted VA–VC then IA–IC; CFG name under the label (e.g. PZR.AV)',
          'Analog values by CFG name',
          'Digital bits 0–15 with CFG bit names',
        ],
      },
      {
        heading: 'Frequency chart',
        items: [
          'Selected PMU frequency trend with FNOM line',
          'ROCOF plot is on Overview; here ROCOF appears only in the live line',
        ],
      },
    ],
    notes: [
      'Float FREQ on wire is absolute Hz; integer FREQ is mHz offset from FNOM.',
      'Requires a PDC build that publishes CFG summary, last frame, and named channels.',
    ],
  },
  {
    id: 'connectivity',
    title: 'Connectivity',
    purpose:
      'Per-stream quality plus measured pipeline hop times so you can see which function is slow.',
    dataSources: [
      'Live frame counts, quality rejects, FPS',
      'Per-stage wall clocks: TCP dial, handshake, TCP read, Kafka publish/lag, parse, quality, readings publish, dashboard record, Redis/Influx, /conversation/state JSON, browser poll',
    ],
    sections: [
      {
        heading: 'Pipeline hop timing',
        items: [
          'Slowest function is the hop with the highest average among ingest/process/dashboard/sink',
          'Connection cards: TCP dial and CFG-2 handshake (once per session)',
          'Bars: last / avg / p95 over the last ~256 samples per stage',
          'E2E receive → dashboard is time from TCP receive stamp to RecordReading',
          'UI refresh is the browser fetch of /conversation/state (plus JSON parse)',
        ],
      },
      {
        heading: 'KPIs and chart',
        items: [
          'Healthy / Degraded / Offline counts',
          'Average latency now uses E2E receive→dashboard when hop data is present',
        ],
      },
      {
        heading: 'Matrix',
        items: [
          'Per PMU: status, loss %, latency, jitter, availability, tip',
          'Click a row for last hop times on that PMU',
        ],
      },
    ],
  },
  {
    id: 'analytics',
    title: 'Analytics',
    purpose:
      'Derived views from live DATA and registration: fleet KPIs, V/I magnitude trends, phasor diagram, inter-PMU angle Δ, advisories.',
    dataSources: [
      'Live trends (frequency, ROCOF, V/I magnitudes), latest channels, FNOM, STAT, connection',
      'Recent stream errors / rejects / timeouts for advisories',
      'Registration — name and region for pair labels',
    ],
    sections: [
      {
        heading: 'KPI grid',
        items: [
          'Max VA angle Δ (needs ≥2 online PMUs; otherwise —)',
          'Avg frequency, worst Δf vs FNOM, max |ROCOF|, online/total, advisory count',
        ],
      },
      {
        heading: 'Phasor focus',
        items: [
          'Dropdown selects which PMU feeds V/I charts and the phasor diagrams',
        ],
      },
      {
        heading: 'Voltage / current magnitude',
        items: [
          'Line charts for VA–VC and IA–IC magnitudes',
          'Last 120 samples from the same trend buffer as frequency',
        ],
      },
      {
        heading: 'Phasor diagrams and angle Δ',
        items: [
          'Separate voltage and current polar diagrams (A yellow, B red, C blue)',
          'Inter-PMU VA angle Δ chart — empty until a second stream is online',
        ],
      },
      {
        heading: 'Advisories',
        items: [
          'Offline / quality rejects, STAT data error, large |Δf| or |ROCOF|, large angle Δ, recent error events',
          'Empty state when nothing is triggering',
        ],
      },
    ],
    notes: [
      'Values come from CFG-2 / DATA / registration (or simple client math such as angle Δ).',
      'Angle Δ needs ≥2 online PMUs; V/I trends and the diagram work with one PMU.',
      'Restart the PDC after upgrades that add V/I magnitude samples to trends.',
    ],
  },
  {
    id: 'help',
    title: 'Help & Support',
    purpose: 'Short topics and FAQ. Full detail is on this Documentation page.',
    dataSources: ['Static help and FAQ in the dashboard'],
    sections: [
      {
        heading: 'Topics & FAQ',
        items: [
          'Getting started, page summaries, offline PMUs, pause, map coords, Worst Δf, STAT errors',
        ],
      },
    ],
  },
]
