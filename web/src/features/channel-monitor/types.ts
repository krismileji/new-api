/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type ChannelMonitorItem = {
  id: number
  name: string
  type: number
  status: number
  status_reason?: string
  priority: number
  weight: number
  base_url: string
  models: string
  test_model: string | null
  groups: string[]
  ratio: number | null
  previous_ratio: number | null
  cost_ratio: number | null
  previous_cost_ratio: number | null
  conversion_factor: number | null
  remark: string
  channel_remark: string
  updated_time: number
  updated_by: number
  updated_by_username: string
  last_fetch_status: '' | 'succeeded' | 'failed'
  last_fetch_error: string
  last_fetch_time: number
  consecutive_failures: number
  upstream_balance: number | null
  last_balance_time: number
  last_balance_error: string
  today_cost_cny: number
  today_cost_configured: boolean
  today_cost_complete: boolean
  today_cost_unresolved_count: number
  concurrency_limit: number
  concurrency_active: number
  upstream: ChannelMonitorUpstreamConfig | null
}

export type ChannelMonitorConcurrencyStatus = {
  active: number
  limit: number
}

export type ChannelMonitorConcurrencyOverview = {
  channels: Record<string, ChannelMonitorConcurrencyStatus>
  generated_at: number
}

export type ChannelMonitorUpstreamType = 'new_api' | 'sub2api' | 'custom'

export type ChannelMonitorUpstreamAuthType =
  | 'public'
  | 'user'
  | 'api_key'
  | 'account'
  | 'token'
  | 'refresh_token'
  | 'custom'

export type ChannelMonitorCustomSource = 'fixed' | 'http'
export type ChannelMonitorCustomBodyType = 'none' | 'json' | 'form'
export type ChannelMonitorCustomResponseType = 'json' | 'text'

export type ChannelMonitorCustomKeyValue = {
  key: string
  value: string
  secret: boolean
  has_value: boolean
}

export type ChannelMonitorCustomRequestConfig = {
  method: 'GET' | 'POST'
  path: string
  query: ChannelMonitorCustomKeyValue[]
  headers: ChannelMonitorCustomKeyValue[]
  body_type: ChannelMonitorCustomBodyType
  body: string
  body_secret: boolean
  has_body: boolean
  form: ChannelMonitorCustomKeyValue[]
}

export type ChannelMonitorCustomResultConfig = {
  response_type: ChannelMonitorCustomResponseType
  value_path: string
  multiplier: number
}

export type ChannelMonitorCustomMetricConfig = {
  source: ChannelMonitorCustomSource
  fixed_value?: number
  request?: ChannelMonitorCustomRequestConfig
  result?: ChannelMonitorCustomResultConfig
}

export type ChannelMonitorCustomUpstreamConfig = {
  version: 1
  ratio: ChannelMonitorCustomMetricConfig
  balance: ChannelMonitorCustomMetricConfig
  balance_reuse_ratio_request: boolean
}

export type ChannelMonitorCostConversion =
  | { mode: 'none' }
  | {
      mode: 'recharge'
      paid_cny: number
      credited_usd: number
    }
  | {
      mode: 'subscription'
      subscription_period: 'day' | 'week' | 'month'
      subscription_price_cny: number
      subscription_daily_usd: number
    }

export type ChannelMonitorUpstreamConfig = {
  type: ChannelMonitorUpstreamType
  base_url: string
  group: string
  auth_type: ChannelMonitorUpstreamAuthType
  user_id: number
  has_access_token: boolean
  has_refresh_token?: boolean
  account: string
  has_password: boolean
  single_channel_action: ChannelMonitorPolicyAction
  multiple_channels_action: ChannelMonitorPolicyAction
  balance_warning_threshold: number | null
  balance_auto_disable_threshold: number | null
  ratio_sync_enabled: boolean
  balance_sync_enabled: boolean
  cost_conversion: ChannelMonitorCostConversion
  custom_config?: ChannelMonitorCustomUpstreamConfig
}

export type ChannelMonitorUpstreamRequest = {
  type: ChannelMonitorUpstreamType
  base_url: string
  group: string
  auth_type: ChannelMonitorUpstreamAuthType
  user_id: number
  access_token: string
  refresh_token?: string
  account: string
  password: string
  single_channel_action: ChannelMonitorPolicyAction
  multiple_channels_action: ChannelMonitorPolicyAction
  balance_warning_threshold: number | null
  balance_auto_disable_threshold: number | null
  ratio_sync_enabled: boolean
  balance_sync_enabled: boolean
  cost_conversion: ChannelMonitorCostConversion
  custom_config?: ChannelMonitorCustomUpstreamConfig
}

export type ChannelMonitorCustomRequestDebug = {
  status_code: number
  duration_ms: number
  response_preview?: string
}

export type ChannelMonitorUpstreamVersionResult = {
  version: string
  endpoint: string
}

export type NewAPIGroupRatioResult = {
  ratio: number
  cost_ratio: number
  conversion_factor: number
  endpoint: string
  balance: ChannelMonitorUpstreamBalanceResult
  debug?: ChannelMonitorCustomRequestDebug
}

export type ChannelMonitorUpstreamBalanceResult = {
  amount: number | null
  endpoint?: string
  error?: string
  debug?: ChannelMonitorCustomRequestDebug
}

export type ChannelMonitorUpstreamGroup = {
  id?: string
  name: string
  ratio: number
}

export type ChannelMonitorUpstreamGroupsResult = {
  groups: ChannelMonitorUpstreamGroup[]
  balance: ChannelMonitorUpstreamBalanceResult
  applied_group?: string
  applied_group_error?: string
}

export type ChannelMonitorFetchResult = {
  result: NewAPIGroupRatioResult
  monitor: {
    ratio: number
    previous_ratio: number | null
    updated_time: number
  }
  created: boolean
  changed: boolean
}

export type ChannelMonitorApplyGroupResult = ChannelMonitorFetchResult & {
  keys_updated: number
}

export type ChannelMonitorRealtimeMetadata = {
  generated_at?: number
  snapshot_age_seconds?: number
  stale?: boolean
  data_cutoff_at: number
  processed_at: number
  event_watermark: number
  queue_depth: number
  redis_status?: 'available' | 'unavailable'
  redis_available?: boolean
  redis_consumer_running?: boolean
  pending_count?: number
  redis_pool_stats?: Record<string, ChannelMonitorRedisPoolStats>
  writer_queue_depth?: number
  writer_queue_capacity?: number
  writer_queued_events?: number
  writer_dropped_events?: number
  writer_retry_events?: number
  writer_oldest_queued_at?: number
  writer_queue_age_seconds?: number
  cost_queue_pending_count?: number
  cost_stream_pending_count?: number
  cost_stream_unread_count?: number
  cost_outbox_pending_count?: number
  cost_outbox_oldest_pending_at?: number
  cost_outbox_retry_count?: number
  cost_ledger_failed_count?: number
  cost_publish_failed_count?: number
  cost_dead_letter_count?: number
  oldest_pending_at?: number
  consumer_lag_seconds?: number
  last_published_at?: number
  last_processed_at?: number
  retry_count?: number
  takeover_count?: number
  quarantine_count?: number
  last_quarantined_at?: number
  runtime_marker_failure_count?: number
  schedule_marker_failure_count?: number
  marker_release_failure_count?: number
  marker_release_failure_active?: boolean
  stream_trim_failure_count?: number
  stream_trim_failure_active?: boolean
  degraded_reasons?: ChannelMonitorRealtimeDegradedReason[]
  realtime_degraded: boolean
}

export type ChannelMonitorRedisPoolStats = {
  role?: string
  pool_size?: number
  total_conns?: number
  idle_conns?: number
  stale_conns?: number
  hits?: number
  misses?: number
  timeouts?: number
  command_count?: number
  command_error_count?: number
  command_latency_total_micros?: number
  command_latency_max_micros?: number
  unavailable?: boolean
}

export type ChannelMonitorRealtimeDegradedReason =
  | 'redis_unavailable'
  | 'consumer_stopped'
  | 'consumer_group_missing'
  | 'event_backlog'
  | 'publisher_unavailable'
  | 'marker_release_failure'
  | 'stream_trim_failure'
  | 'writer_queue_full'
  | 'cost_stream_backlog'
  | 'cost_outbox_backlog'
  | 'cost_publish_failure'
  | 'cost_dead_letter'

export type ChannelMonitorOverview = ChannelMonitorRealtimeMetadata & {
  generated_at: number
  channels: ChannelMonitorItem[]
  channel_order: number[]
  group_ratios: Record<string, number>
  group_coefficients: Record<string, number>
  settings: ChannelMonitorSettings
}

export type ChannelMonitorCostDay = {
  date: string
  start_at: number
  cost_cny: number
  probe_cost_cny?: number
  group_probe_cost_cny?: number
  model_detection_cost_cny?: number
  settled_count: number
  unresolved_count: number
}

export type ChannelMonitorCostChannel = {
  channel_id: number
  channel_name: string
  channel_remark: string
  status: number
  cost_ratio: number | null
  cost_cny: number
  probe_cost_cny?: number
  group_probe_cost_cny?: number
  model_detection_cost_cny?: number
  settled_count: number
  unresolved_count: number
}

export type ChannelMonitorCostAPIKey = {
  id: number
  api_key_id: number
  api_key_name: string
  api_key: string
  user_id?: number
  username?: string
  user_display_name?: string
  cost_cny: number
  settled_count: number
  unresolved_count: number
  channels: ChannelMonitorCostAPIKeyChannel[]
}

export type ChannelMonitorCostAPIKeyChannel = {
  channel_id: number
  channel_name: string
  channel_remark: string
  cost_cny: number
  settled_count: number
  unresolved_count: number
}

export type ChannelMonitorCostCoverage = {
  included_channel_count: number
  unresolved_channel_count: number
  missing_cost_config_channel_count: number
  free_group_channel_count: number
  settled_count: number
  unresolved_count: number
}

export type ChannelMonitorCostOverview = ChannelMonitorRealtimeMetadata & {
  days: number
  generated_at: number
  detail_date: string
  today_cost_cny: number
  today_probe_cost_cny?: number
  today_group_probe_cost_cny?: number
  today_model_detection_cost_cny?: number
  yesterday_cost_cny: number
  yesterday_probe_cost_cny?: number
  yesterday_group_probe_cost_cny?: number
  yesterday_model_detection_cost_cny?: number
  total_cost_cny: number
  total_probe_cost_cny?: number
  total_group_probe_cost_cny?: number
  total_model_detection_cost_cny?: number
  coverage: ChannelMonitorCostCoverage
  items: ChannelMonitorCostDay[]
  chart_items: ChannelMonitorCostDay[]
  item_total: number
  item_page: number
  item_page_size: number
  item_page_count: number
  channels: ChannelMonitorCostChannel[]
  api_keys: ChannelMonitorCostAPIKey[]
}

export type ChannelMonitorPerformanceRangeMinutes = number
export type ChannelMonitorPerformanceRangeSource = 'smart_schedule' | 'manual'

export type ChannelMonitorPerformanceMetric = {
  channel_id: number
  model_name: string
  sample_count: number
  first_token_sample_count: number
  tps_sample_count: number
  tps_output_tokens: number
  tps_generation_duration_ms: number
  average_first_token_ms: number | null
  average_tps: number | null
  latest_first_token_ms: number | null
  latest_tps: number | null
  last_used_time: number
}

export type ChannelMonitorSuccessSummary = {
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
}

export type ChannelMonitorSuccessMetric = ChannelMonitorSuccessSummary & {
  channel_id: number
  model_name: string
}

export type ChannelMonitorGroupSuccessMetric = ChannelMonitorSuccessSummary & {
  group: string
}

export type ChannelMonitorChannelSuccessMetric =
  ChannelMonitorSuccessSummary & {
    channel_id: number
  }

export type ChannelMonitorSuccessAPIKeyMetric = ChannelMonitorSuccessSummary & {
  api_key_id: number
  api_key_name: string
}

export type ChannelMonitorTodaySuccessChannelItem =
  ChannelMonitorSuccessSummary & {
    channel_id: number
    channel_name: string
    channel_remark: string
  }

export type ChannelMonitorTodayCacheWriteItem = {
  channel_id: number
  channel_name: string
  channel_remark: string
  request_count: number
}

export type ChannelMonitorDailyInsightDay = {
  date: string
  start_at: number
  request_count: number
  success_rate: number
  cache_sample_count: number
  cache_rate: number
  cache_read_tokens: number
  input_tokens: number
  cache_utilization_rate: number
  cache_write_channel_count: number
  cache_write_request_count: number
}

export type ChannelMonitorTodaySuccessResult =
  ChannelMonitorRealtimeMetadata & {
    days: number
    generated_at: number
    day_start: number
    detail_date: string
    success_metrics_available: boolean
    cache_write_metrics_available: boolean
    summary: ChannelMonitorSuccessSummary
    channel_items: ChannelMonitorTodaySuccessChannelItem[]
    api_key_items: ChannelMonitorSuccessAPIKeyMetric[]
    cache_write_items: ChannelMonitorTodayCacheWriteItem[]
    chart_items: ChannelMonitorDailyInsightDay[]
  }

export type ChannelMonitorFailureCategory = {
  channel_id: number
  status_code: number
  error_type: string
  error_code: string
  sample_content: string
  actual_count: number
  final_count: number
  last_occurred_at: number
}

export type ChannelMonitorSuccessDetail = {
  summary: ChannelMonitorSuccessSummary
  channel_items: ChannelMonitorChannelSuccessMetric[]
  api_key_items: ChannelMonitorSuccessAPIKeyMetric[]
  failure_categories: ChannelMonitorFailureCategory[]
}

export type ChannelMonitorSuccessDetailResult =
  ChannelMonitorRealtimeMetadata & {
    range_minutes: ChannelMonitorPerformanceRangeMinutes
    generated_at: number
    success_metrics_available: boolean
    scope: 'channel' | 'group' | ''
    detail: ChannelMonitorSuccessDetail
  }

export type ChannelMonitorSuccessMode = 'actual' | 'final'

export type ChannelMonitorSuccessDetailTarget =
  | {
      scope: 'channel'
      mode: 'actual'
      channelId: number
      channelName: string
      modelName?: string
    }
  | {
      scope: 'group'
      mode: ChannelMonitorSuccessMode
      groupName: string
    }

export type ChannelMonitorPerformanceResult = ChannelMonitorRealtimeMetadata & {
  range_minutes: ChannelMonitorPerformanceRangeMinutes
  range_source: ChannelMonitorPerformanceRangeSource
  generated_at: number
  metric_coverage: ChannelMonitorPerformanceMetricCoverage
  items: ChannelMonitorPerformanceMetric[]
  success_metrics_available: boolean
  success_items: ChannelMonitorSuccessMetric[]
  group_success_items: ChannelMonitorGroupSuccessMetric[]
}

export type ChannelMonitorPerformanceMetricCoverage = {
  aggregation_enabled: boolean
  aggregated_from: number
  aggregated_through: number
  window_start: number
  window_complete: boolean
}

export type ChannelMonitorChannelPerformance = {
  sample_count: number
  first_token_sample_count: number
  tps_sample_count: number
  average_first_token_ms: number | null
  average_tps: number | null
  last_used_time: number
}

export type ChannelMonitorSortMode =
  | 'custom'
  | 'channel_asc'
  | 'channel_desc'
  | 'ratio_asc'
  | 'ratio_desc'
  | 'today_cost_asc'
  | 'today_cost_desc'
  | 'first_token_asc'
  | 'first_token_desc'
  | 'tps_asc'
  | 'tps_desc'

export type ChannelMonitorPolicyAction =
  | 'none'
  | 'update_group_ratio'
  | 'disable_channel'
  | 'remove_from_group'

export type ChannelMonitorSmartScheduleMetricPercentages = {
  cost_ratio_percent: number
  first_token_percent: number
  tps_percent: number
}

export type ChannelMonitorSmartScheduleScoring = {
  stability_percent: number
  primary_traffic_percent: number
  primary_switch_threshold_percent: number
  smart: ChannelMonitorSmartScheduleMetricPercentages
  ratio: ChannelMonitorSmartScheduleMetricPercentages
}

export type ChannelMonitorSmartScheduleGroupPolicy = {
  group: string
  strategy: ChannelMonitorSmartScheduleStrategy
  stability_enabled: boolean
  stability_window_minutes: number
  jitter_enabled: boolean
  jitter_tolerance_percent: number
  jitter_slow_threshold_seconds: number
  scoring: ChannelMonitorSmartScheduleScoring
  apply_mode: ChannelMonitorSmartScheduleApplyMode
  models: string[]
  model_order?: string[]
  min_samples: number
  recovery_stability_score: number
  fast_failure_penalty_percent: number
  fast_failure_seconds: number
  fast_failure_same_channel_retry_count?: number
  fast_failure_same_channel_retry_delay_ms?: number
  slow_failure_seconds: number
  burst_failure_window_minutes?: number
  burst_failure_window_requests?: number
  burst_failure_threshold_percent?: number
  /** Legacy fields are accepted when reading persisted policies. */
  burst_failure_window_seconds?: number
  consecutive_failure_threshold?: number
  /** Legacy absolute threshold accepted when reading persisted policies. */
  burst_failure_threshold?: number
  recovery_success_threshold?: number
  cooldown_minutes: number
  sample_mode: ChannelMonitorSmartScheduleSampleMode
  sampling_order: ChannelMonitorSmartScheduleSamplingOrder
  exploration_traffic_percent: number
  /** API and persistence use the actual Token count, not K Token. */
  exploration_max_prompt_tokens?: number
  /** API and persistence use the actual Token count, not K Token. */
  stability_release_max_prompt_tokens?: number
  probe_interval_minutes: number
  degraded_probe_enabled?: boolean
  adaptive_sampling_enabled: boolean
  adaptive_sampling_base_percent: number
  adaptive_sampling_max_percent: number
  adaptive_sampling_error_warning_percent: number
  adaptive_sampling_error_critical_percent: number
  adaptive_sampling_first_token_warning_seconds: number
  adaptive_sampling_first_token_critical_seconds: number
  adaptive_sampling_window_minutes?: number
  adaptive_sampling_window_requests?: number
  /** Legacy seconds field accepted when reading persisted policies. */
  adaptive_sampling_window_seconds?: number
  adaptive_sampling_first_token_warning_request_percent: number
  adaptive_sampling_recover_request_percent: number
  adaptive_sampling_switch_confirm_request_percent: number
  adaptive_sampling_min_comparable_channels: number
}

export type ChannelMonitorSettings = {
  auto_update_interval_minutes: number
  auto_update_retry_count: number
  upstream_request_timeout_seconds?: number
  auto_update_consecutive_failure_limit: number
  auto_disable_on_update_failure: boolean
  auto_enable_on_cost_ratio_recovery: boolean
  auto_enable_on_balance_recovery: boolean
  cost_retention_days: number
  route_metric_retention_days: number
  duration_bucket_retention_days: number
  api_key_metric_retention_days: number
  execution_detail_retention_days: number
  task_retention_days: number
  ratio_monitor_task_retention_days: number
  smart_schedule_task_retention_days: number
  smart_schedule_probe_task_retention_days: number
  cleanup_task_retention_days: number
  model_detection_task_retention_days: number
  channel_test_task_retention_days: number
  model_update_task_retention_days: number
  task_keep_latest_count?: number
  ratio_history_retention_days: number
  status_probe_history_retention_days: number
  group_monitor_retention_days: number
  model_detection_retention_days: number
  cleanup_enabled: boolean
  cleanup_batch_size: number
  cleanup_budget_seconds: number
  cleanup_continuation_seconds: number
  cleanup_interval_minutes: number
  email_notification_enabled: boolean
  notification_email: string
  email_notification_types: ChannelMonitorEmailNotificationType[]
  error_message_mapping: string
  error_message_keywords: string
  probe_response_enabled: boolean
  probe_response_allowed_ips?: string
  probe_response_match_input?: string
  probe_response_text?: string
  probe_response_min_delay_ms?: number
  probe_response_max_delay_ms?: number
  probe_response_input_tokens?: number
  probe_response_cache_write_tokens?: number
  probe_response_cached_tokens?: number
  probe_response_output_tokens?: number
  relay_response_header_timeout_seconds?: number
  smart_schedule_enabled: boolean
  smart_schedule_config_error?: string
  smart_schedule_group_policies: ChannelMonitorSmartScheduleGroupPolicy[]
  smart_schedule_performance_window_minutes: number
  smart_schedule_realtime_retention_minutes: number
  smart_schedule_realtime_sample_limit: number
  smart_schedule_rate_limit_cooldown_seconds: number
  smart_schedule_control_revision: string
  smart_schedule_force_reset_task_created?: boolean
  smart_schedule_force_reset_task_id?: string
  smart_schedule_force_reset_task_error?: string
}

export type ChannelMonitorEmailNotificationType =
  | 'ratio_change'
  | 'balance_warning'
  | 'channel_disabled'
  | 'group_membership_removed'
  | 'upstream_sync_failed'
  | 'task_failed'

export type ChannelMonitorEmailPreview = {
  subject: string
  html: string
  notification_types: ChannelMonitorEmailNotificationType[]
}

export type ChannelMonitorSmartScheduleStrategy =
  | 'ratio'
  | 'first_token'
  | 'tps'
  | 'smart'

export type ChannelMonitorSmartScheduleApplyMode = 'weight' | 'priority_weight'

export type ChannelMonitorSmartScheduleEconomicRole =
  | 'normal'
  | 'break_even_fallback'
  | 'unknown'

export type ChannelMonitorSmartScheduleSampleMode = 'off' | 'traffic' | 'probe'

export type ChannelMonitorSmartScheduleSamplingOrder =
  | 'priority_weight'
  | 'ratio'

export type ChannelMonitorSmartScheduleScoreMetricInput = {
  value: number | null
  sample_count: number
}

export type ChannelMonitorSmartScheduleScoreCohort = {
  minimum: number | null
  maximum: number | null
  available_count: number
}

export type ChannelMonitorSmartScheduleScoreComponent = {
  available: boolean
  comparison_state: 'none' | 'insufficient' | 'comparable'
  raw_value: number | null
  normalized_score: number | null
  configured_weight_percent: number
  effective_weight_percent: number
}

export type ChannelMonitorSmartScheduleScoreDetails = {
  version: number
  strategy: ChannelMonitorSmartScheduleStrategy
  minimum_samples: number
  minimum_comparable_channels: number
  comparison_state: 'none' | 'insufficient' | 'comparable'
  sample_scope: 'channel_model'
  sample_group_count: number
  economics?: {
    cost_ratio?: number | null
    group_ratio?: number | null
    gross_margin?: number | null
    role: ChannelMonitorSmartScheduleEconomicRole
  }
  inputs: {
    cost_ratio: ChannelMonitorSmartScheduleScoreMetricInput
    first_token_ms: ChannelMonitorSmartScheduleScoreMetricInput
    tps: ChannelMonitorSmartScheduleScoreMetricInput
    stability: ChannelMonitorSmartScheduleScoreMetricInput
  }
  cohort: {
    priority?: number
    cost_ratio: ChannelMonitorSmartScheduleScoreCohort
    first_token_ms: ChannelMonitorSmartScheduleScoreCohort
    tps: ChannelMonitorSmartScheduleScoreCohort
  }
  components: {
    cost_ratio: ChannelMonitorSmartScheduleScoreComponent
    first_token_ms: ChannelMonitorSmartScheduleScoreComponent
    tps: ChannelMonitorSmartScheduleScoreComponent
  }
  business_score: number | null
  stability: {
    enabled: boolean
    available: boolean
    applied: boolean
    raw_score: number | null
    configured_weight_percent: number
    effective_weight_percent: number
    business_contribution: number
    contribution: number
  }
  health: {
    state: '' | 'unknown' | 'healthy' | 'observation' | 'pressure' | 'high_risk'
    evidence: boolean
    pressure: number
    error_pressure: number
    latency_pressure: number
    sample_count: number
    window_minutes: number
    window_requests: number
    error_request_percent: number
    risk_request_percent: number
    first_token_warning_request_percent: number
    healthy_request_percent: number
  }
  final_score: number | null
  decision: {
    apply_mode: ChannelMonitorSmartScheduleApplyMode
    current_primary_channel_id: number
    raw_winner_channel_id: number
    selected_primary_channel_id: number
    actual_primary_channel_id: number
    selected_primary: boolean
    manual_primary_channel_id: number
    base_rank: number
    base_priority: number
    base_weight: number
    applied_priority: number
    applied_weight: number
    actual_highest_priority: number
    actual_top_layer_channel_ids: number[] | null
    temporary_traffic_kind: '' | 'insufficient_samples' | 'adaptive_sampling'
    temporary_traffic_target_percent: number
    switch_threshold_percent: number
    primary_traffic_percent: number
    force_reset: boolean
    manual_primary: boolean
    selection_reason: string
    adjustment_reason: string
    reason: string
  }
}

export type ChannelMonitorSmartScheduleStabilityClearResult = {
  cleared: boolean
  previous_state: '' | 'degraded' | 'probing'
  priority: number
  weight: number
}

export type ChannelMonitorSmartScheduleExplorationClearResult = {
  cleared: boolean
  previous_kind: '' | 'insufficient_samples' | 'adaptive_sampling'
  priority: number
  weight: number
}

export type ChannelMonitorSmartScheduleRouteState = {
  id: number
  channel_id: number
  group: string
  model: string
  participation_set: boolean
  excluded: boolean
  last_schedule_status: '' | 'succeeded' | 'skipped' | 'failed'
  last_schedule_error: string
  last_schedule_score: number | null
  last_schedule_score_details?: ChannelMonitorSmartScheduleScoreDetails | null
  last_schedule_priority: number
  last_schedule_weight: number
  last_schedule_time: number
  stability_state: '' | 'degraded' | 'probing'
  stability_until: number
  stability_since: number
  stability_saved_priority: number
  stability_saved_weight: number
  runtime_protection_until: number
  base_rank: number
  base_priority: number
  base_weight: number
  temporary_traffic_kind: '' | 'insufficient_samples' | 'adaptive_sampling'
  temporary_traffic_since: number
  temporary_traffic_target_percent: number
  /** Runtime API value in actual Tokens, not K Token. */
  exploration_max_prompt_tokens?: number
  /** Runtime API value in actual Tokens, not K Token. */
  stability_release_max_prompt_tokens?: number
  manual_primary_until: number
  manual_primary_allow_stability_degrade: boolean
  adaptive_health_state?:
    | ''
    | 'unknown'
    | 'healthy'
    | 'observation'
    | 'pressure'
    | 'high_risk'
  adaptive_health_pressure?: number
  adaptive_health_first_token_warning_request_percent?: number
  adaptive_health_sample_count?: number
  adaptive_health_last_sample_at?: number
  rolling_stability_score: number | null
  rolling_stability_sample_count: number
  rolling_stability_slow_count: number
  rolling_stability_allowed_slow_count: number
  rolling_stability_updated_at: number
  sampling_debt: number
  sampling_candidate: boolean
  sampling_order: '' | ChannelMonitorSmartScheduleSamplingOrder
  last_sampling_at: number
}

export type ChannelMonitorSmartScheduleSharedSamples = {
  id: number
  channel_id: number
  model: string
  window_start: number
  observation_since: number
  recovery_success_count: number
  recovery_success_at: number
  last_time: number
  last_success: boolean
  last_error: string
  sample_count: number
  success_count: number
  failure_duration_sample_count: number
  average_failure_duration_ms: number | null
  first_token_sample_count: number
  average_first_token_ms: number | null
  tps_sample_count: number
  average_tps: number | null
}

export type ChannelMonitorSmartScheduleRoute = {
  channel_id: number
  channel_name: string
  channel_status: number
  channel_priority: number
  channel_weight: number
  group: string
  model: string
  sample_model?: string
  enabled: boolean
  priority: number
  weight: number
  traffic_paused_until?: number
  rate_limit_cooldown_until?: number
  cost_ratio?: number | null
  group_ratio?: number | null
  gross_margin?: number | null
  economic_role?: ChannelMonitorSmartScheduleEconomicRole
  current_window_score?: number | null
  current_window_score_details?: ChannelMonitorSmartScheduleScoreDetails | null
  state: ChannelMonitorSmartScheduleRouteState
  shared_samples?: ChannelMonitorSmartScheduleSharedSamples
}

export type ChannelMonitorSmartScheduleSampleItem = {
  channel_id: number
  model: string
  performance_window: ChannelMonitorSmartScheduleSharedSamples
  stability_window: ChannelMonitorSmartScheduleSharedSamples
}

export type ChannelMonitorSmartScheduleRoutePerformance = {
  channel_id: number
  group: string
  model: string
  group_count: number
  sample_count: number
  first_token_sample_count: number
  first_token_duration_sample_count: number
  tps_sample_count: number
  average_first_token_ms: number | null
  first_token_p50_ms: number | null
  first_token_p95_ms: number | null
  winsorized_average_first_token_ms: number | null
  average_tps: number | null
  cache_hit_count?: number
  cache_sample_count?: number
  cache_hit_rate?: number
  cache_read_tokens?: number
  input_tokens?: number
  cache_utilization_rate?: number
  last_used_time: number
}

export type ChannelMonitorSmartScheduleRouteStability = {
  channel_id: number
  group: string
  model: string
  group_count: number
  success_count: number
  failure_count: number
  final_failure_count: number
  retry_failure_count: number
  sample_count: number
  success_rate: number
  stability_score: number | null
  average_retry_failure_duration_ms: number
  retry_failure_duration_buckets: ChannelMonitorFailureDurationBucket[]
  jitter_available: boolean
  first_token_p50_ms: number | null
  first_token_p95_ms: number | null
  jitter_threshold_ms: number | null
  jitter_sample_count: number
  jitter_slow_count: number
  jitter_allowed_count: number
  jitter_penalty: number
}

export type ChannelMonitorFailureDurationBucket = {
  lower_bound_ms: number
  upper_bound_ms: number
  count: number
}

export type ChannelMonitorSmartScheduleRouteResult =
  ChannelMonitorRealtimeMetadata & {
    generated_at: number
    performance_window_minutes: number
    stability_window_minutes: number
    sample_scope: 'channel_model'
    enabled: boolean
    metrics_included?: boolean
    metric_coverage?: ChannelMonitorSmartScheduleMetricCoverage | null
    routes: ChannelMonitorSmartScheduleRoute[]
    sample_items?: ChannelMonitorSmartScheduleSampleItem[]
    business_performance_items?: ChannelMonitorSmartScheduleRoutePerformance[]
    performance_metrics_available?: boolean
    performance_items: ChannelMonitorSmartScheduleRoutePerformance[]
    stability_metrics_available: boolean
    stability_items: ChannelMonitorSmartScheduleRouteStability[]
    route_snapshot?: ChannelMonitorSmartScheduleRouteSnapshotStatus
  }

export type ChannelMonitorSmartScheduleRouteSnapshotStatus = {
  available: boolean
  revision: number
  generated_at: number
  source_watermark: number
  snapshot_age_seconds: number
  max_age_seconds: number
  redis_backed: boolean
  dirty: boolean
  stale: boolean
  degraded: boolean
  protection_mode: boolean
  last_redis_success_at: number
  last_redis_failure_at: number
  last_redis_error?: string
}

export type ChannelMonitorSmartScheduleMetricCoverage = {
  aggregation_enabled: boolean
  aggregated_from: number
  aggregated_through: number
  performance_window_start: number
  stability_window_start: number
  performance_window_complete: boolean
  stability_window_complete: boolean
  configured_retention_days: number
  required_retention_minutes: number
  configured_retention_sufficient: boolean
  realtime_retention_minutes: number
  realtime_sample_limit: number
  sample_limit_truncated: boolean
  sample_limit_cutoff_at: number
}

export type ChannelMonitorSmartSchedulePrimaryUpdateResult = {
  channel_id: number
  group: string
  model: string
  duration_minutes: number
  allow_stability_degrade: boolean
  manual_primary_until: number
  stability_protection_cleared: boolean
  routing_changed: boolean
  task: ChannelMonitorTask | null
}

export type ChannelMonitorSmartScheduleGroupPauseResult = {
  channel_id: number
  group: string
  model: string
  duration_minutes: number
  paused_until: number
  affected_routes: number
  changed: boolean
}

export type ChannelMonitorTaskRunResult = {
  created: boolean
  task: ChannelMonitorTask
}

export type ChannelMonitorGroupRatioSyncResult = {
  group: string
  upstream_ratio: number
  cost_ratio: number
  conversion_factor: number
  coefficient: number
  ratio: number
}

export type ChannelMonitorGroupChannelsUpdateResult = {
  group: string
  channel_ids: number[]
  added_channel_ids: number[]
  removed_channel_ids: number[]
}

export type ChannelMonitorTaskStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'

export type ChannelMonitorTaskProgress = {
  total: number
  processed: number
  progress: number
}

export type ChannelMonitorTaskResult = {
  total: number
  updated: number
  changed?: number
  balance_updated?: number
  balance_warnings?: number
  failed: number
  groups_updated?: number
  group_memberships_removed?: number
  group_update_failed?: boolean
  channels_disabled?: number
  channels_enabled?: number
  groups_skipped?: number
  retried?: number
  recovered_after_retry?: number
  force_reset?: boolean
  group_policies?: ChannelMonitorSmartScheduleGroupPolicy[]
  group_policy_count?: number
  performance_window_minutes?: number
  stability_window_minutes?: number
  planned?: number
  unchanged?: number
  skipped?: number
  failures?: ChannelMonitorTaskFailure[]
  failure_details_truncated?: boolean
  adjustments?: ChannelMonitorTaskAdjustment[]
  email_status?: 'sent' | 'failed'
  email_error?: string
}

export type ChannelMonitorTaskAdjustment = {
  channel_id: number
  channel_name: string
  group: string
  model: string
  action: 'updated' | 'unchanged' | 'skipped' | 'failed'
  old_priority: number
  new_priority: number
  old_weight: number
  new_weight: number
  score?: number
  score_details?: ChannelMonitorSmartScheduleScoreDetails | null
  manual_primary?: boolean
  manual_primary_until?: number
  manual_primary_allow_stability_degrade?: boolean
  failure_stage?: string
  previous_effective_time?: number
  previous_effective_priority?: number
  previous_effective_weight?: number
  reason: string
}

export type ChannelMonitorTaskFailure = {
  channel_id: number
  channel_name: string
  group: string
  model: string
  failure_stage: string
  error: string
}

export type ChannelMonitorTask = {
  id: number
  task_id: string
  type: 'channel_ratio_monitor' | 'channel_smart_schedule'
  status: ChannelMonitorTaskStatus
  state: ChannelMonitorTaskProgress | null
  result: ChannelMonitorTaskResult | null
  error: string
  created_at: number
  updated_at: number
}

export type ChannelMonitorTaskKind = 'ratio' | 'schedule'

export type ChannelMonitorTaskPage = {
  page: number
  page_size: number
  total: number
  items: ChannelMonitorTask[]
}

export type ChannelMonitorSmartScheduleExecutionDetailPage = {
  page: number
  page_size: number
  total: number
  items: ChannelMonitorTaskAdjustment[]
  groups: string[]
  models: string[]
  channel_names: Record<string, string>
}

export type ChannelRatioHistory = {
  id: number
  channel_id: number
  old_ratio: number
  new_ratio: number
  remark: string
  created_time: number
  operator_id: number
  operator_username: string
}

export type ChannelRatioHistoryPage = {
  page: number
  page_size: number
  total: number
  items: ChannelRatioHistory[]
}

export type ChannelStatusProbeResult =
  | 'success'
  | 'upstream_failure'
  | 'rate_limited'
  | 'local_failure'
  | 'skipped'
  | 'canceled'

export type ChannelStatusProbeHealth =
  | 'unconfigured'
  | 'paused'
  | 'pending'
  | 'healthy'
  | 'partial'
  | 'unhealthy'
  | 'rate_limited'
  | 'stale'

export type ChannelStatusProbeTrigger = 'scheduled' | 'manual'

export type ChannelStatusProbeSampleStatus =
  | 'pending'
  | 'recorded'
  | 'skipped'
  | 'failed'

export type ChannelStatusProbeDisplayUnit = 'minute' | 'hour' | 'day'

export type ChannelStatusProbeConfig = {
  id: number
  channel_id: number
  enabled: boolean
  models: string[]
  interval_seconds: number
  display_value: number
  display_unit: ChannelStatusProbeDisplayUnit
  record_sample: boolean
  next_run_at: number
  manual_request_id: string
  manual_requested_at: number
  revision: number
  running_trigger: '' | ChannelStatusProbeTrigger
  running_run_id: string
  running_started_at: number
  created_at: number
  updated_at: number
}

export type ChannelStatusProbeState = {
  id: number
  channel_id: number
  model_name: string
  execution_id: number
  run_id: string
  started_at: number
  finished_at: number
  result: ChannelStatusProbeResult
  success: boolean
  request_dispatched: boolean
  response_time_ms: number | null
  first_token_ms: number | null
  tps: number | null
  settled_cost_nano_cny?: number | null
  error_code: string
  error_message: string
  consecutive_successes: number
  consecutive_failures: number
  last_health_result: '' | ChannelStatusProbeResult
  last_health_execution_id: number
  last_health_finished_at: number
  sample_status: ChannelStatusProbeSampleStatus
  sample_message: string
  trigger: ChannelStatusProbeTrigger
  endpoint: string
  stream: boolean
  created_at: number
  updated_at: number
}

export type ChannelStatusProbeBucket = {
  started_at: number
  result: '' | ChannelStatusProbeResult
  success: number
  upstream_failure: number
  rate_limited: number
  local_failure: number
  skipped: number
  canceled: number
  models?: string[]
  first_token_total_ms?: number
  first_token_sample_count?: number
  tps_total?: number
  tps_sample_count?: number
  response_time_total_ms?: number
  response_time_sample_count?: number
  latest_execution_id?: number
  latest_finished_at?: number
  latest_result?: ChannelStatusProbeResult
  latest_model_name?: string
  latest_first_token_ms?: number | null
  latest_tps?: number | null
  latest_response_time_ms?: number | null
}

export type ChannelStatusProbeModelStatus = {
  model_name: string
  health_status: ChannelStatusProbeHealth
  latest: ChannelStatusProbeState | null
  recent_window: ChannelStatusProbeBucket[]
  avg_first_token_ms: number | null
  avg_tps: number | null
}

export type ChannelStatusProbeChannel = {
  id: number
  name: string
  type: number
  channel_status: number
  remark: string
  groups: string[]
  cost_ratio: number | null
  today_probe_cost_cny?: number
  supported_models: string[]
  allows_custom_model: boolean
  config: ChannelStatusProbeConfig | null
  health_status: ChannelStatusProbeHealth
  running: boolean
  latest: ChannelStatusProbeState | null
  avg_first_token_ms: number | null
  avg_tps: number | null
  model_statuses: ChannelStatusProbeModelStatus[]
  configured_model_count: number
}

export type ChannelStatusProbeOverview = {
  server_now: number
  snapshot_version: number
  snapshot_revision: number
  event_watermark: number
  generated_at: number
  snapshot_age_seconds: number
  stale: boolean
  scan_interval_seconds: number
  summary: Record<ChannelStatusProbeHealth, number>
  groups: string[]
  models: string[]
  models_by_group: Record<string, string[]>
  channels: ChannelStatusProbeChannel[]
}

export type ChannelStatusProbeExecution = {
  id: number
  run_id: string
  channel_id: number
  model_name: string
  config_revision: number
  trigger: ChannelStatusProbeTrigger
  result: ChannelStatusProbeResult
  started_at: number
  finished_at: number
  response_time_ms: number | null
  first_token_ms: number | null
  tps: number | null
  settled_cost_nano_cny?: number | null
  endpoint: string
  stream: boolean
  request_id: string
  request_dispatched: boolean
  usage_available: boolean
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cached_tokens: number
  cache_write_tokens: number
  reasoning_tokens: number
  error_code: string
  error_message: string
  sample_requested: boolean
  sample_status: ChannelStatusProbeSampleStatus
  sample_message: string
  created_at: number
}

export type ChannelStatusProbeExecutionPage = {
  page: number
  page_size: number
  total: number
  items: ChannelStatusProbeExecution[]
}

export type ChannelStatusProbeSortMode =
  | 'ratio_asc'
  | 'ratio_desc'
  | 'first_token_asc'
  | 'first_token_desc'
  | 'tps_desc'
  | 'tps_asc'

export type ChannelMonitorApiResponse<T> = {
  success: boolean
  message: string
  data: T
  code?: string
}

export type GroupMonitorItem = {
  name: string
  ratio: number
  coefficient: number
  channels: ChannelMonitorItem[]
}
