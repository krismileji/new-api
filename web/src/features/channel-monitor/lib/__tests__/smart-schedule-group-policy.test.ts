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

import type { ChannelMonitorSmartSchedulePolicyFormValues } from '../schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  channelMonitorSmartScheduleGroupPoliciesToApi,
  channelMonitorSmartScheduleGroupPoliciesToForm,
  createChannelMonitorSmartScheduleGroupPolicy,
} from '../smart-schedule-group-policy'

const defaultPolicy: ChannelMonitorSmartSchedulePolicyFormValues = {
  strategy: 'smart',
  stabilityEnabled: true,
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
  applyMode: 'weight',
  models: ['model-a'],
  modelOrder: ['model-c', 'model-a'],
  minSamples: 5,
  recoveryStabilityScore: 95,
  fastFailurePenaltyPercent: 40,
  fastFailureSeconds: 1,
  fastFailureSameChannelRetryCount: 2,
  fastFailureSameChannelRetryDelayMs: 750,
  slowFailureSeconds: 10,
  burstFailureWindowSeconds: 30,
  consecutiveFailureThreshold: 2,
  burstFailureThreshold: 3,
  recoverySuccessThreshold: 2,
  cooldownMinutes: 30,
  sampleMode: 'probe',
  samplingOrder: 'ratio',
  explorationTrafficPercent: 3,
  explorationMaxPromptKTokens: 50,
  stabilityReleaseMaxPromptKTokens: 0,
  probeIntervalMinutes: 15,
  degradedProbeEnabled: false,
  adaptiveSamplingEnabled: false,
  adaptiveSamplingBasePercent: 3,
  adaptiveSamplingMaxPercent: 30,
  adaptiveSamplingPrimaryMinPercent: 70,
  adaptiveSamplingErrorWarningPercent: 5,
  adaptiveSamplingErrorCriticalPercent: 15,
  adaptiveSamplingFirstTokenWarningSeconds: 5,
  adaptiveSamplingFirstTokenCriticalSeconds: 10,
  adaptiveSamplingWindowSeconds: 600,
  adaptiveSamplingFirstTokenWarningRequestPercent: 10,
  adaptiveSamplingRecoverRequestPercent: 95,
  adaptiveSamplingSwitchConfirmRequestPercent: 95,
  adaptiveSamplingMinComparableChannels: 2,
}

describe('smart schedule group policy', () => {
  test('creates a complete independent policy', () => {
    assert.deepEqual(
      createChannelMonitorSmartScheduleGroupPolicy('vip', defaultPolicy),
      {
        group: 'vip',
        ...defaultPolicy,
      }
    )

    assert.deepEqual(
      createChannelMonitorSmartScheduleGroupPolicy('standard', {
        ...defaultPolicy,
        strategy: 'ratio',
        stabilityEnabled: false,
        models: [],
      }),
      {
        group: 'standard',
        ...defaultPolicy,
        strategy: 'ratio',
        stabilityEnabled: false,
        models: [],
      }
    )
  })

  test('preserves a complete independent policy across API mapping', () => {
    const formPolicies = channelMonitorSmartScheduleGroupPoliciesToForm([
      {
        group: 'vip',
        strategy: defaultPolicy.strategy,
        stability_enabled: false,
        jitter_enabled: defaultPolicy.jitterEnabled,
        jitter_tolerance_percent: defaultPolicy.jitterTolerancePercent,
        jitter_slow_threshold_seconds: defaultPolicy.jitterSlowThresholdSeconds,
        scoring: {
          stability_percent: defaultPolicy.scoring.stabilityPercent,
          primary_traffic_percent: defaultPolicy.scoring.primaryTrafficPercent,
          primary_switch_threshold_percent:
            defaultPolicy.scoring.primarySwitchThresholdPercent,
          smart: {
            cost_ratio_percent: defaultPolicy.scoring.smart.costRatioPercent,
            first_token_percent: defaultPolicy.scoring.smart.firstTokenPercent,
            tps_percent: defaultPolicy.scoring.smart.tpsPercent,
          },
          ratio: {
            cost_ratio_percent: defaultPolicy.scoring.ratio.costRatioPercent,
            first_token_percent: defaultPolicy.scoring.ratio.firstTokenPercent,
            tps_percent: defaultPolicy.scoring.ratio.tpsPercent,
          },
        },
        apply_mode: defaultPolicy.applyMode,
        models: [],
        model_order: ['model-c', 'model-a'],
        min_samples: defaultPolicy.minSamples,
        recovery_stability_score: defaultPolicy.recoveryStabilityScore,
        fast_failure_penalty_percent: defaultPolicy.fastFailurePenaltyPercent,
        fast_failure_seconds: defaultPolicy.fastFailureSeconds,
        fast_failure_same_channel_retry_count:
          defaultPolicy.fastFailureSameChannelRetryCount,
        slow_failure_seconds: defaultPolicy.slowFailureSeconds,
        cooldown_minutes: defaultPolicy.cooldownMinutes,
        sample_mode: defaultPolicy.sampleMode,
        sampling_order: defaultPolicy.samplingOrder,
        exploration_traffic_percent: defaultPolicy.explorationTrafficPercent,
        exploration_max_prompt_tokens: 50_000,
        stability_release_max_prompt_tokens: 0,
        probe_interval_minutes: defaultPolicy.probeIntervalMinutes,
        adaptive_sampling_enabled: defaultPolicy.adaptiveSamplingEnabled,
        adaptive_sampling_base_percent:
          defaultPolicy.adaptiveSamplingBasePercent,
        adaptive_sampling_max_percent: defaultPolicy.adaptiveSamplingMaxPercent,
        adaptive_sampling_primary_min_percent:
          defaultPolicy.adaptiveSamplingPrimaryMinPercent,
        adaptive_sampling_error_warning_percent:
          defaultPolicy.adaptiveSamplingErrorWarningPercent,
        adaptive_sampling_error_critical_percent:
          defaultPolicy.adaptiveSamplingErrorCriticalPercent,
        adaptive_sampling_first_token_warning_seconds:
          defaultPolicy.adaptiveSamplingFirstTokenWarningSeconds,
        adaptive_sampling_first_token_critical_seconds:
          defaultPolicy.adaptiveSamplingFirstTokenCriticalSeconds,
        adaptive_sampling_window_seconds:
          defaultPolicy.adaptiveSamplingWindowSeconds,
        adaptive_sampling_first_token_warning_request_percent:
          defaultPolicy.adaptiveSamplingFirstTokenWarningRequestPercent,
        adaptive_sampling_recover_request_percent:
          defaultPolicy.adaptiveSamplingRecoverRequestPercent,
        adaptive_sampling_switch_confirm_request_percent:
          defaultPolicy.adaptiveSamplingSwitchConfirmRequestPercent,
        adaptive_sampling_min_comparable_channels:
          defaultPolicy.adaptiveSamplingMinComparableChannels,
      },
    ])
    const apiPolicies =
      channelMonitorSmartScheduleGroupPoliciesToApi(formPolicies)

    assert.equal(formPolicies[0]?.stabilityEnabled, false)
    assert.equal(formPolicies[0]?.degradedProbeEnabled, false)
    assert.deepEqual(formPolicies[0]?.models, [])
    assert.deepEqual(formPolicies[0]?.modelOrder, ['model-c', 'model-a'])
    assert.equal(apiPolicies[0]?.strategy, 'smart')
    assert.equal(apiPolicies[0]?.stability_enabled, false)
    assert.equal(apiPolicies[0]?.degraded_probe_enabled, false)
    assert.deepEqual(apiPolicies[0]?.model_order, ['model-c', 'model-a'])
    assert.equal(formPolicies[0]?.jitterEnabled, true)
    assert.equal(formPolicies[0]?.jitterTolerancePercent, 5)
    assert.equal(formPolicies[0]?.jitterSlowThresholdSeconds, 10)
    assert.equal(apiPolicies[0]?.jitter_enabled, true)
    assert.equal(apiPolicies[0]?.jitter_tolerance_percent, 5)
    assert.equal(
      Object.hasOwn(apiPolicies[0] ?? {}, 'jitter_threshold_multiplier'),
      false
    )
    assert.equal(apiPolicies[0]?.jitter_slow_threshold_seconds, 10)
    assert.equal(
      Object.hasOwn(apiPolicies[0] ?? {}, 'jitter_absolute_tolerance_seconds'),
      false
    )
    assert.equal(apiPolicies[0]?.scoring.primary_traffic_percent, 90)
    assert.equal(apiPolicies[0]?.scoring.primary_switch_threshold_percent, 3)
    assert.deepEqual(apiPolicies[0]?.models, [])
    assert.equal(apiPolicies[0]?.min_samples, 5)
    assert.equal(apiPolicies[0]?.recovery_stability_score, 95)
    assert.equal(apiPolicies[0]?.fast_failure_penalty_percent, 40)
    assert.equal(apiPolicies[0]?.fast_failure_seconds, 1)
    assert.equal(apiPolicies[0]?.fast_failure_same_channel_retry_count, 2)
    assert.equal(formPolicies[0]?.fastFailureSameChannelRetryDelayMs, 1_000)
    assert.equal(
      apiPolicies[0]?.fast_failure_same_channel_retry_delay_ms,
      1_000
    )
    assert.equal(apiPolicies[0]?.slow_failure_seconds, 10)
    assert.equal(apiPolicies[0]?.burst_failure_window_seconds, 30)
    assert.equal(apiPolicies[0]?.consecutive_failure_threshold, 2)
    assert.equal(apiPolicies[0]?.burst_failure_threshold, 3)
    assert.equal(apiPolicies[0]?.recovery_success_threshold, 2)
    assert.equal(apiPolicies[0]?.sample_mode, 'probe')
    assert.equal(apiPolicies[0]?.sampling_order, 'ratio')
    assert.equal(apiPolicies[0]?.exploration_traffic_percent, 3)
    assert.equal(formPolicies[0]?.explorationMaxPromptKTokens, 50)
    assert.equal(formPolicies[0]?.stabilityReleaseMaxPromptKTokens, 0)
    assert.equal(apiPolicies[0]?.exploration_max_prompt_tokens, 50_000)
    assert.equal(apiPolicies[0]?.stability_release_max_prompt_tokens, 0)
    assert.equal(apiPolicies[0]?.probe_interval_minutes, 15)
    for (const removedField of [
      'priority_sampling_enabled',
      'priority_sampling_interval_minutes',
      'priority_sampling_base_percent',
      'priority_sampling_decay_percent',
      'priority_sampling_min_percent',
      'adaptive_sampling_enter_request_percent',
    ]) {
      assert.equal(Object.hasOwn(apiPolicies[0] ?? {}, removedField), false)
    }
    assert.equal(
      apiPolicies[0]?.adaptive_sampling_first_token_warning_request_percent,
      10
    )
  })

  test('uses a complete editor template without creating a runtime policy', () => {
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.strategy,
      'smart'
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.applyMode,
      'priority_weight'
    )
    assert.deepEqual(CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.models, [])
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.sampleMode,
      'off'
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.samplingOrder,
      'priority_weight'
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.recoveryStabilityScore,
      95
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterEnabled,
      true
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterTolerancePercent,
      5
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterSlowThresholdSeconds,
      10
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.scoring
        .primaryTrafficPercent,
      90
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.scoring
        .primarySwitchThresholdPercent,
      3
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.fastFailurePenaltyPercent,
      40
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.fastFailureSeconds,
      1
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.fastFailureSameChannelRetryCount,
      0
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.fastFailureSameChannelRetryDelayMs,
      1_000
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.slowFailureSeconds,
      10
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.burstFailureWindowSeconds,
      30
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.consecutiveFailureThreshold,
      2
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.burstFailureThreshold,
      3
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.recoverySuccessThreshold,
      2
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.explorationTrafficPercent,
      3
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.explorationMaxPromptKTokens,
      50
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.stabilityReleaseMaxPromptKTokens,
      0
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.probeIntervalMinutes,
      10
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.adaptiveSamplingFirstTokenWarningRequestPercent,
      10
    )
  })
})
