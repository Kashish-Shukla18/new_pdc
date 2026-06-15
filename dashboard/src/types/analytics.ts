export type AnalyticsSeverity = 'bad' | 'warn' | 'info'

export type AnglePair = {
  key: string
  name: string
  value: number
  regionA: string
  regionB: string
}

export type AnalyticsKpi = {
  label: string
  value: string
  sub: string
  tone: 'ok' | 'warn' | 'bad' | 'accent' | 'neutral'
}

export type OscillationMode = {
  freq: number
  damping: number
  pmu: string
  label: string
}

export type IslandingRow = {
  name: string
  risk: 'Low' | 'Medium' | 'High'
  detail: string
}

export type VoltageBus = {
  name: string
  pu: number
  tone: 'ok' | 'warn' | 'bad'
}

export type AnalyticsRecommendation = {
  sev: AnalyticsSeverity
  title: string
  desc: string
}

export type AngleHistoryPoint = {
  ts: number
  label: string
  [key: string]: number | string
}
