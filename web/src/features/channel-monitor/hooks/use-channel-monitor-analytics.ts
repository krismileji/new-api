import { useQuery } from '@tanstack/react-query'

import { getChannelMonitorAnalytics } from '../api'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'
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
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
  })
}
