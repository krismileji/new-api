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

export type ChannelModelDetectionPreset = 'low' | 'medium' | 'high'

export type ChannelModelDetectionDisplayUnit = 'minute' | 'hour' | 'day'

export type ChannelModelDetectionBucketResult =
  | ''
  | 'success'
  | 'attention'
  | 'unhealthy'
  | 'failed'
  | 'running'
  | 'inactive'

export type ChannelModelDetectionTrigger = 'scheduled' | 'manual'

export type ChannelModelDetectionPresetSource =
  | 'scheduled_default'
  | 'manual_selected'

export type ChannelModelDetectionClaimedModel =
  | 'gpt-5.6-sol'
  | 'gpt-5.6-terra'
  | 'gpt-5.6-luna'

export type ChannelModelDetectionKnownOutcomeCode =
  | 'juice_pass_fingerprint_strong'
  | 'juice_pass_fingerprint_unclear'
  | 'juice_mismatch_fingerprint_strong'
  | 'juice_mismatch_fingerprint_unclear'
  | 'juice_insufficient_fingerprint_strong'
  | 'juice_insufficient_fingerprint_unclear'
  | 'possible_non_gpt'

export type ChannelModelDetectionOutcomeCode =
  | ChannelModelDetectionKnownOutcomeCode
  | (string & {})

export type ChannelModelDetectionHealth =
  | 'unconfigured'
  | 'paused'
  | 'pending'
  | 'running'
  | 'healthy'
  | 'attention'
  | 'unhealthy'
  | 'detector_unavailable'
  | 'stale'

export type ChannelModelDetectionRunStatus =
  | 'queued'
  | 'waiting_detector'
  | 'submitting'
  | 'submission_unknown'
  | 'running'
  | 'canceling'
  | 'completed'
  | 'partial'
  | 'failed'
  | 'external_session_conflict'
  | 'canceled'

export type ChannelModelDetectionExecutionStatus =
  | 'pending'
  | 'submitting'
  | 'running'
  | 'completed'
  | 'failed'
  | 'canceled'
  | 'skipped'

export type ChannelModelDetectionCostStatus =
  | 'pending'
  | 'not_started'
  | 'settled'
  | 'unresolved'
  | 'partial'

export type ChannelModelDetectionDetectorState =
  | 'unconfigured'
  | 'available'
  | 'degraded'
  | 'offline'
  | 'incompatible'
  | 'unknown'

export type ChannelModelDetectionCost = {
  currency: 'CNY'
  estimated_quota: number | null
  estimated_cost_nano_cny: number | null
  estimated_cost_cny: string | null
  cost_estimate_unknown_count: number
  settled_quota: number
  cost_basis_quota: number
  settled_cost_nano_cny: number | null
  settled_cost_cny: string | null
  unresolved_cost_nano_cny: number | null
  unresolved_cost_cny: string | null
  unresolved_cost_unknown_count: number
  settled_request_count: number
  unresolved_request_count: number
  status: ChannelModelDetectionCostStatus
  cost_scope: 'channel_upstream_api'
}

export type ChannelModelDetectionProgress = {
  planned: number
  logical_completed: number
  successful: number
  errors: number
  cancelled: number
  http_attempts: number
  retries: number
}

export type ChannelModelDetectionExecutionSummary = {
  run_id: string
  target_key: string
  status: ChannelModelDetectionExecutionStatus
  request_model: string
  claimed_model: ChannelModelDetectionClaimedModel
  outcome_code: ChannelModelDetectionOutcomeCode | ''
  title_cn: string
  subtitle_cn: string
  juice_verdict_state?: string
  fingerprint_verdict_state?: string
  fingerprint_model?: string
  fingerprint_claim_mismatch?: boolean
  preset: ChannelModelDetectionPreset
  preset_source: ChannelModelDetectionPresetSource
  trigger: ChannelModelDetectionTrigger
  progress: ChannelModelDetectionProgress
  cost: ChannelModelDetectionCost
  started_at: number
  finished_at: number
  updated_at: number
}

export type ChannelModelDetectionResultBucket = {
  started_at: number
  result: ChannelModelDetectionBucketResult
  detection_count: number
  success: number
  attention: number
  unhealthy: number
  failed: number
  running: number
  inactive: number
}

export type ChannelModelDetectionTargetSummary = {
  target_key: string
  request_model: string
  claimed_model: ChannelModelDetectionClaimedModel
  enabled: boolean
  position: number
  latest: ChannelModelDetectionExecutionSummary | null
  recent_window: ChannelModelDetectionResultBucket[]
}

export type ChannelModelDetectionChannelConfig = {
  channel_id: number
  schedule_enabled: boolean
  revision: number
  created_at: number
  updated_at: number
}

export type ChannelModelDetectionConfiguredTarget = {
  target_key: string
  request_model: string
  claimed_model: ChannelModelDetectionClaimedModel
  enabled: boolean
  position: number
}

export type ChannelModelDetectionChannelConfigResult =
  ChannelModelDetectionChannelConfig & {
    targets: ChannelModelDetectionConfiguredTarget[]
  }

export type ChannelModelDetectionActiveRun = {
  run_id: string
  status: ChannelModelDetectionRunStatus
  trigger: ChannelModelDetectionTrigger
  preset: ChannelModelDetectionPreset
  preset_source: ChannelModelDetectionPresetSource
  progress: ChannelModelDetectionProgress
  queued_at: number
  started_at: number
  updated_at: number
}

export type ChannelModelDetectionChannel = {
  id: number
  name: string
  type: number
  channel_status: number
  remark: string
  groups: string[]
  cost_ratio: number | null
  supported_models: string[]
  health_status: ChannelModelDetectionHealth
  config: ChannelModelDetectionChannelConfig | null
  active_run: ChannelModelDetectionActiveRun | null
  targets: ChannelModelDetectionTargetSummary[]
  latest_run_cost: ChannelModelDetectionCost | null
  today_model_detection_cost?: ChannelModelDetectionCost | null
  today_model_detection_cost_cny?: number
}

export type ChannelModelDetectionPresetEstimate = {
  preset: ChannelModelDetectionPreset
  available: boolean
  logical_requests: number | null
  fixed_32k_requests: number | null
  config_hash: string
  unavailable_reason: string
}

export type ChannelModelDetectionDetectorService = {
  state: ChannelModelDetectionDetectorState
  detector_url_configured: boolean
  detector_url_masked: string
  busy: boolean
  active_session_owned: boolean
  deployment_id: string | null
  last_checked_at: number
  last_error: string
  compatibility_message: string
  estimates: Partial<
    Record<ChannelModelDetectionPreset, ChannelModelDetectionPresetEstimate>
  >
}

export type ChannelModelDetectionSettingsSummary = {
  detector_url_configured: boolean
  detector_url_masked: string
  scheduled_preset: ChannelModelDetectionPreset
  schedule_enabled: boolean
  interval_minutes: number
  display_value: number
  display_unit: ChannelModelDetectionDisplayUnit
  interval_hours?: number
  schedule_time?: string
  timezone?: string
  next_batch_at: number
  revision: number
}

export type ChannelModelDetectionSettings = {
  detector_url_configured: boolean
  detector_url: string
  detector_url_masked: string
  pending_detector_url_configured: boolean
  pending_detector_url: string
  pending_detector_url_masked: string
  detector_url_switch_pending: boolean
  scheduled_preset: ChannelModelDetectionPreset
  schedule_enabled: boolean
  interval_minutes: number
  display_value: number
  display_unit: ChannelModelDetectionDisplayUnit
  interval_hours?: number
  schedule_time?: string
  timezone?: string
  schedule_anchor_at?: number
  next_batch_at: number
  revision: number
  connection_test_required: boolean
  created_at: number
  updated_at: number
}

export type ChannelModelDetectionOverview = {
  server_now: number
  settings: ChannelModelDetectionSettingsSummary
  detector: ChannelModelDetectionDetectorService
  summary: Record<ChannelModelDetectionHealth, number>
  groups: string[]
  models: string[]
  models_by_group: Record<string, string[]>
  channels: ChannelModelDetectionChannel[]
}

export type ChannelModelDetectionStatusFilter =
  | 'all'
  | 'issue'
  | 'attention'
  | 'running'
  | 'healthy'
  | 'paused'
  | 'unconfigured'

export type ChannelModelDetectionSortMode =
  | 'ratio_asc'
  | 'ratio_desc'
  | 'latest_desc'
  | 'latest_asc'
  | 'issue_first'
  | 'schedule_first'
  | 'channel_id_asc'

export type ChannelModelDetectionFilters = {
  status: ChannelModelDetectionStatusFilter
  group: string
  model: string
  search: string
  sort: ChannelModelDetectionSortMode
  onlyConfigured: boolean
}

export type ChannelModelDetectionApiResponse<T> = {
  success: boolean
  message: string
  data: T
  code?: string
}

export type ChannelModelDetectionSettingsUpdateRequest = {
  detector_url?: string
  clear_detector_url: boolean
  scheduled_preset: ChannelModelDetectionPreset
  confirm_high_cost: boolean
  schedule_enabled: boolean
  interval_minutes: number
  display_value: number
  display_unit: ChannelModelDetectionDisplayUnit
  revision: number
}

export type ChannelModelDetectionServiceTestRequest = {
  detector_url: string
}

export type ChannelModelDetectionTargetUpdateRequest = {
  target_key: string
  request_model: string
  claimed_model: ChannelModelDetectionClaimedModel
}

export type ChannelModelDetectionConfigUpdateRequest = {
  schedule_enabled: boolean
  targets: ChannelModelDetectionTargetUpdateRequest[]
  revision: number
}

export type ChannelModelDetectionRunRequest = {
  preset?: ChannelModelDetectionPreset
  confirm_high_cost: boolean
}

export type ChannelModelDetectionRunAccepted = {
  run_id: string
  status: 'queued'
  preset: ChannelModelDetectionPreset
  preset_source: 'manual_selected'
}

export type ChannelModelDetectionCancelResponse = {
  run_id: string
  status: ChannelModelDetectionRunStatus
}

export type ChannelModelDetectionRunSummary = {
  run_id: string
  channel_id: number
  trigger: ChannelModelDetectionTrigger
  preset: ChannelModelDetectionPreset
  preset_source: ChannelModelDetectionPresetSource
  status: ChannelModelDetectionRunStatus
  target_count: number
  completed_target_count: number
  progress: ChannelModelDetectionProgress
  cost: ChannelModelDetectionCost
  queued_at: number
  started_at: number
  finished_at: number
  updated_at: number
  cancel_requested_at: number
  error_code: string
  error_message: string
  created_by_user_id: number
  created_by_username: string
  created_at: number
}

export type ChannelModelDetectionHistoryQuery = {
  page: number
  page_size: number
  trigger: '' | ChannelModelDetectionTrigger
  status: '' | ChannelModelDetectionRunStatus
  model: string
  outcome: string
}

export type ChannelModelDetectionRunHistoryPage = {
  page: number
  page_size: number
  total: number
  items: ChannelModelDetectionRunSummary[]
}

export type ChannelModelDetectionExecutionDetail =
  ChannelModelDetectionExecutionSummary & {
    official_session_id: string
    official: boolean
    config_hash: string
    schema_version: number
    scoring_version: string
    baseline_id: string
    baseline_sha256: string
    build_hash: string
    usage_available: boolean
    input_tokens: number
    output_tokens: number
    total_tokens: number
    report_sha256: string
    final_error_code: string
    error_code: string
    error_message: string
    report: unknown
  }

export type ChannelModelDetectionRunDetail = {
  run: ChannelModelDetectionRunSummary
  executions: ChannelModelDetectionExecutionDetail[]
}

export type ChannelModelDetectionEstimateRequest = {
  preset: ChannelModelDetectionPreset
}

export type ChannelModelDetectionTargetEstimate = {
  target_key: string
  request_model: string
  claimed_model: ChannelModelDetectionClaimedModel
  estimated_logical_requests: number
  estimated_http_attempts: number
  estimated_quota: number | null
  estimated_cost_nano_cny: number | null
  estimated_cost_cny: string | null
  cost_estimate_unknown: boolean
  estimate_basis: string
}

export type ChannelModelDetectionEstimateResult = {
  preset: ChannelModelDetectionPreset
  official_estimate: ChannelModelDetectionPresetEstimate
  targets: ChannelModelDetectionTargetEstimate[]
  estimated_quota: number | null
  estimated_cost_nano_cny: number | null
  estimated_cost_cny: string | null
  cost_estimate_unknown_count: number
}
