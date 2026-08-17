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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ChannelMonitorSettingsFormValues } from '../../lib/schema'
import { createChannelMonitorSettingsUpdatePayload } from '../../lib/settings-update'
import type { ChannelMonitorSettings } from '../../types'

type OptionalKeys<T> = {
  [K in keyof T]-?: {} extends Pick<T, K> ? K : never
}[keyof T]

type StorageSettingKeys =
  | 'cost_retention_days'
  | 'route_metric_retention_days'
  | 'api_key_metric_retention_days'
  | 'execution_detail_retention_days'
  | 'task_retention_days'
  | 'ratio_history_retention_days'
  | 'status_probe_history_retention_days'
  | 'model_detection_retention_days'
  | 'cleanup_enabled'
  | 'cleanup_batch_size'
  | 'cleanup_budget_seconds'
  | 'cleanup_continuation_seconds'
  | 'cleanup_interval_minutes'

type StorageSettingsAreRequired =
  Exclude<StorageSettingKeys, keyof ChannelMonitorSettings> extends never
    ? Extract<
        OptionalKeys<ChannelMonitorSettings>,
        StorageSettingKeys
      > extends never
      ? true
      : false
    : false

const storageSettingsAreRequired: StorageSettingsAreRequired = true

const formValues = {
  autoUpdateIntervalMinutes: 15,
  autoUpdateRetryCount: 2,
  upstreamRequestTimeoutSeconds: 45,
  autoUpdateConsecutiveFailureLimit: 3,
  autoDisableOnUpdateFailure: true,
  autoEnableOnCostRatioRecovery: true,
  autoEnableOnBalanceRecovery: false,
  costRetentionDays: 90,
  routeMetricRetentionDays: 30,
  apiKeyMetricRetentionDays: 7,
  executionDetailRetentionDays: 14,
  taskRetentionDays: 90,
  ratioHistoryRetentionDays: 365,
  statusProbeHistoryRetentionDays: 7,
  modelDetectionRetentionDays: 30,
  cleanupEnabled: false,
  cleanupBatchSize: 2500,
  cleanupBudgetSeconds: 45,
  cleanupContinuationSeconds: 90,
  cleanupIntervalMinutes: 360,
  emailNotificationEnabled: true,
  notificationEmail: 'ops@example.com',
  emailNotificationTypes: ['balance_warning', 'task_failed'],
  errorMessageMapping: '{"429":"请求过于频繁，请稍后再试"}',
  probeResponseEnabled: true,
  probeResponseMatchInput: 'health check',
  probeResponseText: 'healthy',
  probeResponseMinDelayMs: 125,
  probeResponseMaxDelayMs: 875,
  probeResponseInputTokens: 7,
  probeResponseCacheWriteTokens: 1,
  probeResponseCachedTokens: 2,
  probeResponseOutputTokens: 11,
  relayResponseHeaderTimeoutSeconds: 45,
  smartScheduleEnabled: true,
  smartScheduleGroupPolicies: [
    {
      group: 'vip',
      strategy: 'smart',
      stabilityEnabled: true,
      stabilityWindowMinutes: 15,
      jitterEnabled: true,
      jitterTolerancePercent: 5,
      jitterSlowThresholdSeconds: 10,
      scoring: {
        stabilityPercent: 50,
        primaryTrafficPercent: 90,
        primarySwitchThresholdPercent: 3,
        smart: {
          costRatioPercent: 40,
          firstTokenPercent: 40,
          tpsPercent: 20,
        },
        ratio: {
          costRatioPercent: 70,
          firstTokenPercent: 20,
          tpsPercent: 10,
        },
      },
      applyMode: 'priority_weight',
      models: ['gpt-5'],
      modelOrder: [],
      minSamples: 20,
      recoveryStabilityScore: 95,
      fastFailurePenaltyPercent: 40,
      fastFailureSeconds: 1,
      fastFailureSameChannelRetryCount: 2,
      fastFailureSameChannelRetryDelayMs: 750,
      slowFailureSeconds: 10,
      burstFailureWindowMinutes: 2,
      burstFailureWindowRequests: 80,
      burstFailureThresholdPercent: 5,
      consecutiveFailureThreshold: 3,
      recoverySuccessThreshold: 4,
      cooldownMinutes: 30,
      sampleMode: 'probe',
      samplingOrder: 'ratio',
      explorationTrafficPercent: 3,
      explorationMaxPromptKTokens: 50,
      stabilityReleaseMaxPromptKTokens: 0,
      probeIntervalMinutes: 15,
      degradedProbeEnabled: false,
      adaptiveSamplingEnabled: true,
      adaptiveSamplingBasePercent: 3,
      adaptiveSamplingMaxPercent: 30,
      adaptiveSamplingErrorWarningPercent: 5,
      adaptiveSamplingErrorCriticalPercent: 15,
      adaptiveSamplingFirstTokenWarningSeconds: 5,
      adaptiveSamplingFirstTokenCriticalSeconds: 10,
      adaptiveSamplingWindowMinutes: 10,
      adaptiveSamplingWindowRequests: 100,
      adaptiveSamplingFirstTokenWarningRequestPercent: 10,
      adaptiveSamplingRecoverRequestPercent: 95,
      adaptiveSamplingSwitchConfirmRequestPercent: 95,
      adaptiveSamplingMinComparableChannels: 2,
    },
  ],
  smartSchedulePerformanceWindowMinutes: 60,
  smartScheduleRealtimeRetentionMinutes: 120,
  smartScheduleRealtimeSampleLimit: 20_000,
  smartScheduleRateLimitCooldownSeconds: 30,
  smartScheduleForceReset: true,
} as ChannelMonitorSettingsFormValues

describe('channel monitor settings submit payload', () => {
  test('requires every retention and cleanup field in the API contract', () => {
    assert.equal(storageSettingsAreRequired, true)
  })

  test('schedule mode submits only explicit policies and global runtime fields', () => {
    const payload = createChannelMonitorSettingsUpdatePayload(
      'schedule',
      formValues,
      'revision-a'
    )

    assert.deepEqual(Object.keys(payload).sort(), [
      'relay_response_header_timeout_seconds',
      'smart_schedule_control_revision',
      'smart_schedule_enabled',
      'smart_schedule_force_reset',
      'smart_schedule_group_policies',
      'smart_schedule_performance_window_minutes',
      'smart_schedule_rate_limit_cooldown_seconds',
      'smart_schedule_realtime_retention_minutes',
      'smart_schedule_realtime_sample_limit',
    ])
    assert.equal(payload.smart_schedule_control_revision, 'revision-a')
    assert.equal(payload.smart_schedule_group_policies?.[0]?.group, 'vip')
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.scoring
        ?.primary_traffic_percent,
      90
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.scoring
        ?.primary_switch_threshold_percent,
      3
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.sample_mode,
      'probe'
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.sampling_order,
      'ratio'
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.probe_interval_minutes,
      15
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.degraded_probe_enabled,
      false
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.exploration_max_prompt_tokens,
      50_000
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.stability_release_max_prompt_tokens,
      0
    )
    for (const removedField of [
      'priority_sampling_enabled',
      'priority_sampling_interval_minutes',
      'priority_sampling_base_percent',
      'priority_sampling_decay_percent',
      'priority_sampling_min_percent',
      'adaptive_sampling_enter_request_percent',
      'adaptive_sampling_primary_min_percent',
      'burst_failure_window_seconds',
      'burst_failure_threshold',
      'adaptive_sampling_window_seconds',
    ]) {
      assert.equal(
        Object.hasOwn(
          payload.smart_schedule_group_policies?.[0] ?? {},
          removedField
        ),
        false
      )
    }
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.adaptive_sampling_first_token_warning_request_percent,
      10
    )
    assert.equal(payload.smart_schedule_performance_window_minutes, 60)
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.stability_window_minutes,
      15
    )
    assert.equal(payload.smart_schedule_realtime_retention_minutes, 120)
    assert.equal(payload.smart_schedule_realtime_sample_limit, 20_000)
    assert.equal(payload.smart_schedule_rate_limit_cooldown_seconds, 30)
    assert.equal(
      Object.hasOwn(
        payload.smart_schedule_group_policies?.[0] ?? {},
        'degrade_stability_score'
      ),
      false
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.recovery_stability_score,
      95
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.fast_failure_penalty_percent,
      40
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.fast_failure_seconds,
      1
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.fast_failure_same_channel_retry_count,
      2
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.fast_failure_same_channel_retry_delay_ms,
      750
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.slow_failure_seconds,
      10
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.burst_failure_window_minutes,
      2
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.burst_failure_window_requests,
      80
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.burst_failure_threshold_percent,
      5
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.consecutive_failure_threshold,
      3
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.adaptive_sampling_window_minutes,
      10
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.adaptive_sampling_window_requests,
      100
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.recovery_success_threshold,
      4
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.jitter_enabled,
      true
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.jitter_tolerance_percent,
      5
    )
    assert.equal(
      Object.hasOwn(
        payload.smart_schedule_group_policies?.[0] ?? {},
        'jitter_threshold_multiplier'
      ),
      false
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.jitter_slow_threshold_seconds,
      10
    )
    assert.equal(
      Object.hasOwn(
        payload.smart_schedule_group_policies?.[0] ?? {},
        'jitter_absolute_tolerance_seconds'
      ),
      false
    )
    assert.equal('smart_schedule_groups' in payload, false)
    assert.equal('smart_schedule_strategy' in payload, false)
    assert.equal('auto_update_interval_minutes' in payload, false)
    assert.equal('upstream_request_timeout_seconds' in payload, false)
  })

  test('general mode submits only channel monitoring fields', () => {
    const payload = createChannelMonitorSettingsUpdatePayload(
      'general',
      formValues,
      'revision-a'
    )

    assert.deepEqual(Object.keys(payload).sort(), [
      'api_key_metric_retention_days',
      'auto_disable_on_update_failure',
      'auto_enable_on_balance_recovery',
      'auto_enable_on_cost_ratio_recovery',
      'auto_update_consecutive_failure_limit',
      'auto_update_interval_minutes',
      'auto_update_retry_count',
      'cleanup_batch_size',
      'cleanup_budget_seconds',
      'cleanup_continuation_seconds',
      'cleanup_enabled',
      'cleanup_interval_minutes',
      'cost_retention_days',
      'email_notification_enabled',
      'email_notification_types',
      'error_message_mapping',
      'execution_detail_retention_days',
      'model_detection_retention_days',
      'notification_email',
      'probe_response_cache_write_tokens',
      'probe_response_cached_tokens',
      'probe_response_enabled',
      'probe_response_input_tokens',
      'probe_response_match_input',
      'probe_response_max_delay_ms',
      'probe_response_min_delay_ms',
      'probe_response_output_tokens',
      'probe_response_text',
      'ratio_history_retention_days',
      'route_metric_retention_days',
      'status_probe_history_retention_days',
      'task_retention_days',
      'upstream_request_timeout_seconds',
    ])
    assert.equal(payload.upstream_request_timeout_seconds, 45)
    assert.equal(payload.execution_detail_retention_days, 14)
    assert.equal(payload.route_metric_retention_days, 30)
    assert.equal(payload.api_key_metric_retention_days, 7)
    assert.equal(payload.task_retention_days, 90)
    assert.equal(payload.ratio_history_retention_days, 365)
    assert.equal(payload.status_probe_history_retention_days, 7)
    assert.equal(payload.model_detection_retention_days, 30)
    assert.equal(payload.cleanup_enabled, false)
    assert.equal(payload.cleanup_batch_size, 2500)
    assert.equal(payload.cleanup_budget_seconds, 45)
    assert.equal(payload.cleanup_continuation_seconds, 90)
    assert.equal(payload.cleanup_interval_minutes, 360)
    assert.deepEqual(payload.email_notification_types, [
      'balance_warning',
      'task_failed',
    ])
    assert.equal(
      payload.error_message_mapping,
      '{"429":"请求过于频繁，请稍后再试"}'
    )
    assert.equal(payload.probe_response_match_input, 'health check')
    assert.equal(payload.probe_response_text, 'healthy')
    assert.equal(payload.probe_response_min_delay_ms, 125)
    assert.equal(payload.probe_response_max_delay_ms, 875)
    assert.equal(payload.probe_response_input_tokens, 7)
    assert.equal(payload.probe_response_cache_write_tokens, 1)
    assert.equal(payload.probe_response_cached_tokens, 2)
    assert.equal(payload.probe_response_output_tokens, 11)
    assert.equal('smart_schedule_enabled' in payload, false)
    assert.equal('smart_schedule_group_policies' in payload, false)
    assert.equal('relay_response_header_timeout_seconds' in payload, false)
    assert.equal('smart_schedule_control_revision' in payload, false)
  })
})
