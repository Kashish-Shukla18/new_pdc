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
  ts?: number
  [streamKey: string]: number | string | undefined
}

export type RttStream = {
  key: string
  name: string
  color: string
  latency: number
}
