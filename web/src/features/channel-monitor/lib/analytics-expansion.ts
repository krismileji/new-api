import type { ChannelMonitorSuccessMode } from '../types'
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
  minutes?: number
  group?: string
  successMode?: ChannelMonitorSuccessMode
  channelId?: number
  userId?: number
  apiKeyId?: number
  apiKeyKey?: string
  model?: string
  modelKey?: string
  search?: string
  sort?: ChannelMonitorAnalyticsSort
  direction?: 'asc' | 'desc'
}

export function getChannelMonitorAnalyticsChildGroupBy(
  tab: ChannelMonitorAnalyticsExpansionTab,
  groupBy: ChannelMonitorAnalyticsGroupBy
): ChannelMonitorAnalyticsGroupBy | null {
  if (tab === 'api_keys') {
    if (groupBy === 'user') return 'api_key'
    if (groupBy === 'api_key') return 'model'
    if (groupBy === 'model') return 'channel'
    return null
  }
  if (groupBy === 'channel') return 'model'
  if (groupBy === 'model' || groupBy === 'channel_model') return 'user'
  if (groupBy === 'user') return 'api_key'
  return null
}
