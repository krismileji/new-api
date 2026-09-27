import { formatChannelMonitorCost } from './format'

export function formatProfitMoney(value: number | undefined) {
  return formatChannelMonitorCost(
    value == null ? undefined : value / 1_000_000_000
  )
}

export function formatProfitRate(value: number | null | undefined) {
  return value == null || !Number.isFinite(value)
    ? '—'
    : `${(value * 100).toFixed(1)}%`
}
