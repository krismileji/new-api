import type { ChannelMonitorAnalyticsSummary } from '../types-analytics'
import { formatChannelMonitorCost } from './format'

export function formatProfitMoney(value: number | undefined) {
  return formatChannelMonitorCost(
    value == null ? undefined : value / 1_000_000_000
  )
}

export function formatProfitRate(
  summary: ChannelMonitorAnalyticsSummary | undefined
) {
  const value = summary?.profit_confirmed === true ? summary.profit_rate : null
  return value == null || !Number.isFinite(value)
    ? '—'
    : `${(value * 100).toFixed(1)}%`
}
