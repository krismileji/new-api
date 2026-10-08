import type {
  ChannelMonitorAnalyticsResponse,
  ChannelMonitorAnalyticsSummary,
} from '../types-analytics'
import { formatChannelMonitorCost } from './format'

export function formatProfitMoney(value: number | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  return formatChannelMonitorCost(value / 1_000_000_000)
}

export function getProfitDisplayStatus(
  summary: ChannelMonitorAnalyticsSummary | undefined,
  coverage?: ChannelMonitorAnalyticsResponse['coverage'],
  dayStart?: number
): 'confirmed' | 'estimated' | 'history_unavailable' | 'unavailable' {
  if (coverage?.status === 'unavailable') return 'unavailable'
  if (
    coverage?.reasons.includes('income_history_unavailable') &&
    (dayStart == null || dayStart < coverage.covered_from)
  ) {
    return 'history_unavailable'
  }
  if (
    summary?.profit_nano_cny == null ||
    !Number.isFinite(summary.profit_nano_cny)
  ) {
    return 'unavailable'
  }
  if (summary.profit_confirmed === true) return 'confirmed'
  if (coverage?.reasons.includes('profit_history_expired')) return 'unavailable'
  return 'estimated'
}

export function formatProfitRate(
  summary: ChannelMonitorAnalyticsSummary | undefined,
  coverage?: ChannelMonitorAnalyticsResponse['coverage'],
  dayStart?: number
) {
  const status = getProfitDisplayStatus(summary, coverage, dayStart)
  const value =
    status === 'confirmed' || status === 'estimated'
      ? summary?.profit_rate
      : null
  return value == null || !Number.isFinite(value)
    ? '—'
    : `${(value * 100).toFixed(1)}%`
}
