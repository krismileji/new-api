import type {
  ChannelMonitorFailureCategory,
  ChannelMonitorSuccessMode,
} from './types'

export type ChannelMonitorAnalyticsMetric = 'success' | 'cost'
export type ChannelMonitorAnalyticsSort =
  | 'samples'
  | 'success'
  | 'failure'
  | 'success_rate'
  | 'cache_tokens'
  | 'cache_utilization'
  | 'cache_write'
  | 'cost'
  | 'settled'
  | 'unresolved'
  | 'resolution_rate'
export type ChannelMonitorAnalyticsChannel = {
  name: string
  remark?: string | null
}

export type ChannelMonitorAnalyticsGroupBy =
  | 'day'
  | 'channel'
  | 'user'
  | 'api_key'
  | 'model'
  | 'channel_model'
  | 'api_key_channel_model'

export type ChannelMonitorAnalyticsQuery = {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
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
  page?: number
  pageSize?: number
}

export type ChannelMonitorAnalyticsSummary = {
  actual_success_count: number
  actual_failure_count: number
  actual_sample_count: number
  actual_success_rate: number
  final_success_count: number
  final_failure_count: number
  final_sample_count: number
  final_success_rate: number
  cache_hit_count: number
  cache_sample_count: number
  cache_hit_rate: number
  cache_read_tokens: number
  input_tokens: number
  cache_utilization_rate: number
  cache_write_request_count: number
  cost_nano_cny?: number
  settled_count?: number
  unresolved_count?: number
}

export type ChannelMonitorAnalyticsItem = ChannelMonitorAnalyticsSummary & {
  key: string
  day_start?: number
  channel_id?: number
  user_id?: number
  user_name?: string
  user_display_name?: string
  user_attribution?: string
  api_key_id?: number
  api_key_key?: string
  api_key_name?: string
  model_key?: string
  model_name?: string
  group_by?: ChannelMonitorAnalyticsGroupBy
}

export type ChannelMonitorAnalyticsResponse = {
  snapshot_revision?: number
  processed_at?: number
  generated_at?: number
  source:
    | 'database_daily'
    | 'redis_daily'
    | 'redis_and_database_daily'
    | 'redis_minutes'
  range_minutes?: number
  window_start?: number
  window_end?: number
  failure_categories?: ChannelMonitorFailureCategory[]
  failure_categories_truncated?: boolean
  group_by: ChannelMonitorAnalyticsGroupBy
  coverage: {
    status: 'complete' | 'partial' | 'unavailable'
    covered_from: number
    covered_through: number
    reasons: string[]
  }
  summary: ChannelMonitorAnalyticsSummary
  scope_summary: ChannelMonitorAnalyticsSummary
  items: ChannelMonitorAnalyticsItem[]
  page: number
  page_size: number
  total: number
}
