import type {
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsMetric,
  ChannelMonitorAnalyticsSort,
} from '../types-analytics'

export type ChannelMonitorAnalyticsExpansionTab = 'channels' | 'api_keys'

export type ChannelMonitorAnalyticsExpansionContext = {
  tab: ChannelMonitorAnalyticsExpansionTab
  metric: ChannelMonitorAnalyticsMetric
  from?: string
  to?: string
  channelId?: number
  userId?: number
  apiKeyId?: number
  model?: string
  search?: string
  sort?: ChannelMonitorAnalyticsSort
  direction?: 'asc' | 'desc'
}

export function getChannelMonitorAnalyticsChildGroupBy(
  tab: ChannelMonitorAnalyticsExpansionTab,
  groupBy: ChannelMonitorAnalyticsGroupBy
): ChannelMonitorAnalyticsGroupBy | null {
  if (tab === 'api_keys') {
    if (groupBy === 'api_key') return 'model'
    if (groupBy === 'model') return 'channel'
    return null
  }
  if (groupBy === 'channel') return 'model'
  if (groupBy === 'model' || groupBy === 'channel_model') return 'user'
  if (groupBy === 'user') return 'api_key'
  return null
}
