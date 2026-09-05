import type { ChannelMonitorAnalyticsResponse } from '../types-analytics'

export function isChannelMonitorAnalyticsCoverageIncomplete(
  coverage: ChannelMonitorAnalyticsResponse['coverage'] | undefined
) {
  return coverage != null && coverage.status !== 'complete'
}
