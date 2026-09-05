import { useQuery } from '@tanstack/react-query'

import { getChannelMonitorAnalytics } from '../api'
import type { ChannelMonitorAnalyticsQuery } from '../types-analytics'

export function useChannelMonitorAnalytics(
  request: ChannelMonitorAnalyticsQuery,
  enabled = true
) {
  return useQuery({
    queryKey: ['channel-monitor', 'analytics', request],
    queryFn: () => getChannelMonitorAnalytics(request),
    enabled,
    staleTime: 15_000,
  })
}
