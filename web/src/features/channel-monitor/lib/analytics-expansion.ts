import type {
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsMetric,
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
  search?: string
  sort?: 'samples' | 'success' | 'failure' | 'cache_tokens'
  direction?: 'asc' | 'desc'
}

export function getChannelMonitorAnalyticsChildGroupBy(
  tab: ChannelMonitorAnalyticsExpansionTab,
  groupBy: ChannelMonitorAnalyticsGroupBy
): ChannelMonitorAnalyticsGroupBy | null {
  if (tab === 'api_keys') {
    return groupBy === 'api_key' ? 'api_key_channel_model' : null
  }
  if (groupBy === 'channel' || groupBy === 'channel_model') return 'user'
  if (groupBy === 'user') return 'api_key'
  if (groupBy === 'api_key') return 'api_key_channel_model'
  return null
}
