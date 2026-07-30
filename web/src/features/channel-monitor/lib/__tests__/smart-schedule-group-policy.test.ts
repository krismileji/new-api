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
  jitterThresholdMultiplier: 3,
  jitterAbsoluteToleranceMs: 1000,
  jitterBaselineHours: 24,
  scoring: {
    stabilityPercent: 50,
    curveExponent: 1,
    relativeWeightEnabled: true,
    relativeWeightStartPercent: 3,
    relativeWeightFullPercent: 10,
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
  degradeStabilityScore: 90,
  recoveryStabilityScore: 95,
  fastFailurePenaltyPercent: 40,
  fastFailureSeconds: 1,
  slowFailureSeconds: 10,
  cooldownMinutes: 30,
  sampleMode: 'probe',
  explorationTrafficPercent: 3,
  probeIntervalMinutes: 15,
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
        jitter_threshold_multiplier: defaultPolicy.jitterThresholdMultiplier,
        jitter_absolute_tolerance_ms: defaultPolicy.jitterAbsoluteToleranceMs,
        jitter_baseline_hours: defaultPolicy.jitterBaselineHours,
        scoring: {
          stability_percent: defaultPolicy.scoring.stabilityPercent,
          curve_exponent: defaultPolicy.scoring.curveExponent,
          relative_weight_enabled: defaultPolicy.scoring.relativeWeightEnabled,
          relative_weight_start_percent:
            defaultPolicy.scoring.relativeWeightStartPercent,
          relative_weight_full_percent:
            defaultPolicy.scoring.relativeWeightFullPercent,
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
        degrade_stability_score: defaultPolicy.degradeStabilityScore,
        recovery_stability_score: defaultPolicy.recoveryStabilityScore,
        fast_failure_penalty_percent: defaultPolicy.fastFailurePenaltyPercent,
        fast_failure_seconds: defaultPolicy.fastFailureSeconds,
        slow_failure_seconds: defaultPolicy.slowFailureSeconds,
        cooldown_minutes: defaultPolicy.cooldownMinutes,
        sample_mode: defaultPolicy.sampleMode,
        exploration_traffic_percent: defaultPolicy.explorationTrafficPercent,
        probe_interval_minutes: defaultPolicy.probeIntervalMinutes,
      },
    ])
    const apiPolicies =
      channelMonitorSmartScheduleGroupPoliciesToApi(formPolicies)

    assert.equal(formPolicies[0]?.stabilityEnabled, false)
    assert.deepEqual(formPolicies[0]?.models, [])
    assert.deepEqual(formPolicies[0]?.modelOrder, ['model-c', 'model-a'])
    assert.equal(apiPolicies[0]?.strategy, 'smart')
    assert.equal(apiPolicies[0]?.stability_enabled, false)
    assert.deepEqual(apiPolicies[0]?.model_order, ['model-c', 'model-a'])
    assert.equal(formPolicies[0]?.jitterEnabled, true)
    assert.equal(formPolicies[0]?.jitterTolerancePercent, 5)
    assert.equal(formPolicies[0]?.jitterThresholdMultiplier, 3)
    assert.equal(formPolicies[0]?.jitterAbsoluteToleranceMs, 1000)
    assert.equal(formPolicies[0]?.jitterBaselineHours, 24)
    assert.equal(apiPolicies[0]?.jitter_enabled, true)
    assert.equal(apiPolicies[0]?.jitter_tolerance_percent, 5)
    assert.equal(apiPolicies[0]?.jitter_threshold_multiplier, 3)
    assert.equal(apiPolicies[0]?.jitter_absolute_tolerance_ms, 1000)
    assert.equal(apiPolicies[0]?.jitter_baseline_hours, 24)
    assert.deepEqual(apiPolicies[0]?.models, [])
    assert.equal(apiPolicies[0]?.min_samples, 5)
    assert.equal(apiPolicies[0]?.degrade_stability_score, 90)
    assert.equal(apiPolicies[0]?.recovery_stability_score, 95)
    assert.equal(apiPolicies[0]?.fast_failure_penalty_percent, 40)
    assert.equal(apiPolicies[0]?.fast_failure_seconds, 1)
    assert.equal(apiPolicies[0]?.slow_failure_seconds, 10)
    assert.equal(apiPolicies[0]?.sample_mode, 'probe')
    assert.equal(apiPolicies[0]?.exploration_traffic_percent, 3)
    assert.equal(apiPolicies[0]?.probe_interval_minutes, 15)
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
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.degradeStabilityScore,
      90
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
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterThresholdMultiplier,
      3
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterAbsoluteToleranceMs,
      1000
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.jitterBaselineHours,
      24
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
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.slowFailureSeconds,
      10
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.explorationTrafficPercent,
      3
    )
    assert.equal(
      CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.probeIntervalMinutes,
      10
    )
  })
})
