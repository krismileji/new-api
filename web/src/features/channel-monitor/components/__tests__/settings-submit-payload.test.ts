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

const formValues = {
  autoUpdateIntervalMinutes: 15,
  autoUpdateRetryCount: 2,
  upstreamRequestTimeoutSeconds: 45,
  autoUpdateConsecutiveFailureLimit: 3,
  autoDisableOnUpdateFailure: true,
  autoEnableOnCostRatioRecovery: true,
  autoEnableOnBalanceRecovery: false,
  costRetentionDays: 90,
  emailNotificationEnabled: true,
  notificationEmail: 'ops@example.com',
  emailNotificationTypes: ['balance_warning', 'task_failed'],
  probeResponseEnabled: true,
  relayResponseHeaderTimeoutSeconds: 45,
  smartScheduleEnabled: true,
  smartScheduleGroupPolicies: [
    {
      group: 'vip',
      strategy: 'smart',
      stabilityEnabled: true,
      jitterEnabled: true,
      jitterTolerancePercent: 5,
      jitterAbsoluteToleranceSeconds: 10,
      jitterBaselineMinutes: 60,
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
      minSamples: 20,
      degradeStabilityScore: 90,
      recoveryStabilityScore: 95,
      fastFailurePenaltyPercent: 40,
      fastFailureSeconds: 1,
      slowFailureSeconds: 10,
      cooldownMinutes: 30,
      sampleMode: 'probe',
      explorationTrafficPercent: 3,
      probeIntervalMinutes: 15,
      prioritySamplingEnabled: true,
      prioritySamplingIntervalMinutes: 10,
      prioritySamplingBasePercent: 3,
      prioritySamplingDecayPercent: 70,
      prioritySamplingMinPercent: 0.5,
    },
  ],
  smartScheduleIntervalMinutes: 10,
  smartSchedulePerformanceWindowMinutes: 60,
  smartScheduleStabilityWindowMinutes: 120,
  smartScheduleRateLimitCooldownSeconds: 30,
  smartScheduleForceReset: true,
} as ChannelMonitorSettingsFormValues

describe('channel monitor settings submit payload', () => {
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
      'smart_schedule_interval_minutes',
      'smart_schedule_performance_window_minutes',
      'smart_schedule_rate_limit_cooldown_seconds',
      'smart_schedule_stability_window_minutes',
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
      payload.smart_schedule_group_policies?.[0]?.probe_interval_minutes,
      15
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.priority_sampling_enabled,
      true
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.priority_sampling_interval_minutes,
      10
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]
        ?.priority_sampling_decay_percent,
      70
    )
    assert.equal(payload.smart_schedule_performance_window_minutes, 60)
    assert.equal(payload.smart_schedule_stability_window_minutes, 120)
    assert.equal(payload.smart_schedule_rate_limit_cooldown_seconds, 30)
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.degrade_stability_score,
      90
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
      payload.smart_schedule_group_policies?.[0]?.slow_failure_seconds,
      10
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
      payload.smart_schedule_group_policies?.[0]
        ?.jitter_absolute_tolerance_seconds,
      10
    )
    assert.equal(
      payload.smart_schedule_group_policies?.[0]?.jitter_baseline_minutes,
      60
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
      'auto_disable_on_update_failure',
      'auto_enable_on_balance_recovery',
      'auto_enable_on_cost_ratio_recovery',
      'auto_update_consecutive_failure_limit',
      'auto_update_interval_minutes',
      'auto_update_retry_count',
      'cost_retention_days',
      'email_notification_enabled',
      'email_notification_types',
      'notification_email',
      'probe_response_enabled',
      'upstream_request_timeout_seconds',
    ])
    assert.equal(payload.upstream_request_timeout_seconds, 45)
    assert.deepEqual(payload.email_notification_types, [
      'balance_warning',
      'task_failed',
    ])
    assert.equal('smart_schedule_enabled' in payload, false)
    assert.equal('smart_schedule_group_policies' in payload, false)
    assert.equal('relay_response_header_timeout_seconds' in payload, false)
    assert.equal('smart_schedule_control_revision' in payload, false)
  })
})
