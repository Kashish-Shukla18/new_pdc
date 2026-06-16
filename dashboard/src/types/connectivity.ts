export type ConnectivityKpi = {
  label: string
  value: string
  subtext: string
  tone: 'ok' | 'warn' | 'bad' | 'accent' | 'neutral'
}

export type ConnectivityRec = {
  sev: 'bad' | 'warn' | 'info'
  title: string
  desc: string
}

export type RttHistoryPoint = {
  slot: number
  label: string
  [streamKey: string]: number | string
}

export type RttStream = {
  key: string
  name: string
  color: string
  latency: number
}
