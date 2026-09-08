import { useQuery } from '@tanstack/react-query'

import { getChannelMonitorAnalytics } from '../api'
import { CHANNEL_MONITOR_LIVE_REFRESH_INTERVAL_MS } from '../lib/query-options'
import type { ChannelMonitorAnalyticsQuery } from '../types-analytics'

export function useChannelMonitorAnalytics(
  request: ChannelMonitorAnalyticsQuery,
  enabled = true
) {
  return useQuery({
    queryKey: ['channel-monitor', 'analytics', request],
    queryFn: () => getChannelMonitorAnalytics(request),
    enabled,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchIntervalInBackground: false,
    refetchInterval: () => {
      const today = new Date(Date.now() + 8 * 60 * 60 * 1000)
        .toISOString()
        .slice(0, 10)
      const includesToday =
        (!request.from || request.from <= today) &&
        (!request.to || request.to > today)
      return includesToday ? CHANNEL_MONITOR_LIVE_REFRESH_INTERVAL_MS : false
    },
  })
}
