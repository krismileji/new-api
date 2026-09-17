export type ChannelPassivePeriod = {
  source: 'redis_business'
  period_start: number
  period_end: number
  resolution: 'period' | 'hour'
  coverage: 'complete' | 'partial' | 'collecting' | 'unavailable'
  reason?: string
  success: number
  failure: number
  local_responses: number
  first_token_samples: number
  duration_samples: number
  tps_samples: number
  avg_first_token_ms: number | null
  avg_duration_ms: number | null
  avg_tps: number | null
  success_rate: number | null
  processed_at: number
  data_cutoff_at: number
  version: number
  sample_window_start?: number
  sample_window_end?: number
}

export type ChannelPassiveView = {
  target: {
    id: string
    scope: 'status' | 'group_member' | 'group_final'
    channel_id?: number
    group_name?: string
    model_name: string
    interval_seconds: number
    config_revision: number
    policy_revision?: number
    effective_at: number
  }
  periods: ChannelPassivePeriod[]
}

export type ChannelPassiveOverview = {
  items: ChannelPassiveView[]
  total: number
  page: number
  unavailable_reason?: string
  server_now: number
}
