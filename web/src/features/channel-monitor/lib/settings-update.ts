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
import type { ChannelMonitorSettings } from '../types'
import type { ChannelMonitorSettingsFormValues } from './schema'
import { channelMonitorSmartScheduleGroupPoliciesToApi } from './smart-schedule-group-policy'

export type ChannelMonitorSettingsUpdateMode = 'general' | 'schedule'

export function createChannelMonitorSettingsUpdatePayload(
  mode: ChannelMonitorSettingsUpdateMode,
  values: ChannelMonitorSettingsFormValues,
  smartScheduleControlRevision: string
): Partial<ChannelMonitorSettings> & {
  smart_schedule_force_reset?: boolean
} {
  if (mode === 'schedule') {
    return {
      smart_schedule_control_revision: smartScheduleControlRevision,
      smart_schedule_enabled: values.smartScheduleEnabled,
      smart_schedule_group_policies:
        channelMonitorSmartScheduleGroupPoliciesToApi(
          values.smartScheduleGroupPolicies
        ),
      smart_schedule_interval_minutes: values.smartScheduleIntervalMinutes,
      smart_schedule_performance_window_minutes:
        values.smartSchedulePerformanceWindowMinutes,
      smart_schedule_stability_window_minutes:
        values.smartScheduleStabilityWindowMinutes,
      smart_schedule_rate_limit_cooldown_seconds:
        values.smartScheduleRateLimitCooldownSeconds,
      relay_response_header_timeout_seconds:
        values.relayResponseHeaderTimeoutSeconds,
      smart_schedule_force_reset: values.smartScheduleForceReset,
    }
  }

  return {
    auto_update_interval_minutes: values.autoUpdateIntervalMinutes,
    auto_update_retry_count: values.autoUpdateRetryCount,
    upstream_request_timeout_seconds: values.upstreamRequestTimeoutSeconds,
    auto_update_consecutive_failure_limit:
      values.autoUpdateConsecutiveFailureLimit,
    auto_disable_on_update_failure: values.autoDisableOnUpdateFailure,
    auto_enable_on_cost_ratio_recovery: values.autoEnableOnCostRatioRecovery,
    auto_enable_on_balance_recovery: values.autoEnableOnBalanceRecovery,
    cost_retention_days: values.costRetentionDays,
    execution_detail_retention_days: values.executionDetailRetentionDays,
    task_retention_days: values.taskRetentionDays,
    ratio_history_retention_days: values.ratioHistoryRetentionDays,
    email_notification_enabled: values.emailNotificationEnabled,
    notification_email: values.notificationEmail,
    email_notification_types: values.emailNotificationTypes,
    error_message_mapping: values.errorMessageMapping,
    probe_response_enabled: values.probeResponseEnabled,
    probe_response_match_input: values.probeResponseMatchInput,
    probe_response_text: values.probeResponseText,
    probe_response_min_delay_ms: values.probeResponseMinDelayMs,
    probe_response_max_delay_ms: values.probeResponseMaxDelayMs,
    probe_response_input_tokens: values.probeResponseInputTokens,
    probe_response_cache_write_tokens: values.probeResponseCacheWriteTokens,
    probe_response_cached_tokens: values.probeResponseCachedTokens,
    probe_response_output_tokens: values.probeResponseOutputTokens,
  }
}
