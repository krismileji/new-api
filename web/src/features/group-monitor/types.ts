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
export type ChannelGroupMonitorDisplayUnit = 'minute' | 'hour' | 'day'

export type ChannelGroupMonitorStatus =
  | 'unconfigured'
  | 'paused'
  | 'pending'
  | 'healthy'
  | 'unavailable'
  | 'unhealthy'
  | 'rate_limited'
  | 'stale'

export type ChannelGroupMonitorResult =
  | 'success'
  | 'upstream_failure'
  | 'rate_limited'
  | 'local_failure'
  | 'unavailable'
  | 'skipped'

export type ChannelGroupMonitorBucketResult = '' | ChannelGroupMonitorResult

export type ChannelGroupMonitorGroup = {
  group_name: string
  probe_model: string
  display_initial?: string
}

export type ChannelGroupMonitorBucket = {
  started_at: number
  success: number
  upstream_failure: number
  rate_limited: number
  local_failure: number
  unavailable: number
  skipped: number
  first_token_total_ms?: number
  first_token_sample_count?: number
  tps_total?: number
  tps_sample_count?: number
  response_time_total_ms?: number
  response_time_sample_count?: number
  result: ChannelGroupMonitorBucketResult
}

export type ChannelGroupMonitorSettings = {
  enabled: boolean
  groups: ChannelGroupMonitorGroup[]
  interval_seconds: number
  display_value: number
  display_unit: ChannelGroupMonitorDisplayUnit
  next_run_at: number
  manual_request_id: string
  manual_requested_at: number
  revision: number
  running_trigger: '' | 'scheduled' | 'manual'
  running_run_id: string
  running_started_at: number
  updated_at: number
}

export type ChannelGroupMonitorItem = {
  group: string
  initial: string
  status: ChannelGroupMonitorStatus
  latest_first_token_ms: number | null
  success_rate: number | null
  success_count: number
  completed_count: number
  last_finished_at: number
  recent_window: ChannelGroupMonitorBucket[]
}

export type PricingGroupMonitorItem = Pick<
  ChannelGroupMonitorItem,
  | 'group'
  | 'initial'
  | 'status'
  | 'latest_first_token_ms'
  | 'success_rate'
  | 'last_finished_at'
  | 'recent_window'
>

export type ChannelGroupMonitorAdminItem = ChannelGroupMonitorItem & {
  probe_model: string
  config_valid: boolean
  latest_result: ChannelGroupMonitorResult | ''
  last_success_at: number
  last_failure_at: number
  consecutive_success: number
  consecutive_failure: number
}

export type ChannelGroupMonitorSettingsResponse = {
  settings: ChannelGroupMonitorSettings
  candidate_models_by_group: Record<string, string[]>
}

export type ChannelGroupMonitorOverview = {
  server_now: number
  settings: ChannelGroupMonitorSettings
  candidate_models_by_group: Record<string, string[]>
  items: ChannelGroupMonitorAdminItem[]
}

export type PricingGroupMonitor = {
  enabled: boolean
  server_now: number
  data_cutoff_at: number
  display_value: number
  display_unit: ChannelGroupMonitorDisplayUnit
  items: PricingGroupMonitorItem[]
}

export type ChannelGroupMonitorExecution = {
  id: number
  run_id: string
  group_name: string
  config_revision: number
  trigger: 'scheduled' | 'manual'
  probe_model: string
  channel_id: number
  request_id: string
  result: ChannelGroupMonitorResult
  request_dispatched: boolean
  response_time_ms: number | null
  first_token_ms: number | null
  tps: number | null
  settled_cost_nano_cny: number | null
  error_code: string
  error_message: string
  started_at: number
  finished_at: number
  created_at: number
}

export type ChannelGroupMonitorExecutionPage = {
  page: number
  page_size: number
  total: number
  items: ChannelGroupMonitorExecution[]
}

export type ChannelGroupMonitorApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}
