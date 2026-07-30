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
import { DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING } from '../constants'
import type {
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleScoring,
} from '../types'
import type {
  ChannelMonitorSmartScheduleGroupPolicyFormValues,
  ChannelMonitorSmartSchedulePolicyFormValues,
} from './schema'

type SmartScheduleScoringFormValues =
  ChannelMonitorSmartSchedulePolicyFormValues['scoring']

export const CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE: ChannelMonitorSmartSchedulePolicyFormValues =
  {
    strategy: 'smart',
    stabilityEnabled: true,
    jitterEnabled: true,
    jitterTolerancePercent: 5,
    jitterThresholdMultiplier: 3,
    jitterAbsoluteToleranceMs: 1000,
    jitterBaselineHours: 24,
    scoring: channelMonitorSmartScheduleScoringToForm(
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING
    ),
    applyMode: 'priority_weight',
    models: [],
    modelOrder: [],
    minSamples: 5,
    degradeStabilityScore: 90,
    recoveryStabilityScore: 95,
    fastFailurePenaltyPercent: 40,
    fastFailureSeconds: 1,
    slowFailureSeconds: 10,
    cooldownMinutes: 30,
    sampleMode: 'off',
    explorationTrafficPercent: 3,
    probeIntervalMinutes: 10,
  }

export function channelMonitorSmartScheduleScoringToForm(
  scoring: ChannelMonitorSmartScheduleScoring
): SmartScheduleScoringFormValues {
  return {
    stabilityPercent: scoring.stability_percent,
    curveExponent: scoring.curve_exponent,
    relativeWeightEnabled: scoring.relative_weight_enabled,
    relativeWeightStartPercent: scoring.relative_weight_start_percent,
    relativeWeightFullPercent: scoring.relative_weight_full_percent,
    smart: {
      costRatioPercent: scoring.smart.cost_ratio_percent,
      firstTokenPercent: scoring.smart.first_token_percent,
      tpsPercent: scoring.smart.tps_percent,
    },
    ratio: {
      costRatioPercent: scoring.ratio.cost_ratio_percent,
      firstTokenPercent: scoring.ratio.first_token_percent,
      tpsPercent: scoring.ratio.tps_percent,
    },
  }
}

export function channelMonitorSmartScheduleScoringToApi(
  scoring: SmartScheduleScoringFormValues
): ChannelMonitorSmartScheduleScoring {
  return {
    stability_percent: scoring.stabilityPercent,
    curve_exponent: scoring.curveExponent,
    relative_weight_enabled: scoring.relativeWeightEnabled,
    relative_weight_start_percent: scoring.relativeWeightStartPercent,
    relative_weight_full_percent: scoring.relativeWeightFullPercent,
    smart: {
      cost_ratio_percent: scoring.smart.costRatioPercent,
      first_token_percent: scoring.smart.firstTokenPercent,
      tps_percent: scoring.smart.tpsPercent,
    },
    ratio: {
      cost_ratio_percent: scoring.ratio.costRatioPercent,
      first_token_percent: scoring.ratio.firstTokenPercent,
      tps_percent: scoring.ratio.tpsPercent,
    },
  }
}

export function channelMonitorSmartScheduleGroupPoliciesToForm(
  policies: ChannelMonitorSmartScheduleGroupPolicy[] = []
): ChannelMonitorSmartScheduleGroupPolicyFormValues[] {
  return policies.map((policy) => ({
    group: policy.group,
    strategy: policy.strategy,
    stabilityEnabled: policy.stability_enabled,
    jitterEnabled: policy.jitter_enabled,
    jitterTolerancePercent: policy.jitter_tolerance_percent,
    jitterThresholdMultiplier: policy.jitter_threshold_multiplier,
    jitterAbsoluteToleranceMs: policy.jitter_absolute_tolerance_ms,
    jitterBaselineHours: policy.jitter_baseline_hours,
    scoring: channelMonitorSmartScheduleScoringToForm(policy.scoring),
    applyMode: policy.apply_mode,
    models: [...policy.models],
    modelOrder: [...(policy.model_order ?? [])],
    minSamples: policy.min_samples,
    degradeStabilityScore: policy.degrade_stability_score,
    recoveryStabilityScore: policy.recovery_stability_score,
    fastFailurePenaltyPercent: policy.fast_failure_penalty_percent,
    fastFailureSeconds: policy.fast_failure_seconds,
    slowFailureSeconds: policy.slow_failure_seconds,
    cooldownMinutes: policy.cooldown_minutes,
    sampleMode: policy.sample_mode,
    explorationTrafficPercent: policy.exploration_traffic_percent,
    probeIntervalMinutes: policy.probe_interval_minutes,
  }))
}

export function channelMonitorSmartScheduleGroupPoliciesToApi(
  policies: ChannelMonitorSmartScheduleGroupPolicyFormValues[]
): ChannelMonitorSmartScheduleGroupPolicy[] {
  return policies.map((policy) => ({
    group: policy.group,
    strategy: policy.strategy,
    stability_enabled: policy.stabilityEnabled,
    jitter_enabled: policy.jitterEnabled,
    jitter_tolerance_percent: policy.jitterTolerancePercent,
    jitter_threshold_multiplier: policy.jitterThresholdMultiplier,
    jitter_absolute_tolerance_ms: policy.jitterAbsoluteToleranceMs,
    jitter_baseline_hours: policy.jitterBaselineHours,
    scoring: channelMonitorSmartScheduleScoringToApi(policy.scoring),
    apply_mode: policy.applyMode,
    models: policy.models,
    model_order: policy.modelOrder,
    min_samples: policy.minSamples,
    degrade_stability_score: policy.degradeStabilityScore,
    recovery_stability_score: policy.recoveryStabilityScore,
    fast_failure_penalty_percent: policy.fastFailurePenaltyPercent,
    fast_failure_seconds: policy.fastFailureSeconds,
    slow_failure_seconds: policy.slowFailureSeconds,
    cooldown_minutes: policy.cooldownMinutes,
    sample_mode: policy.sampleMode,
    exploration_traffic_percent: policy.explorationTrafficPercent,
    probe_interval_minutes: policy.probeIntervalMinutes,
  }))
}

export function createChannelMonitorSmartScheduleGroupPolicy(
  group: string,
  policy: ChannelMonitorSmartSchedulePolicyFormValues
): ChannelMonitorSmartScheduleGroupPolicyFormValues {
  return {
    group,
    strategy: policy.strategy,
    stabilityEnabled: policy.stabilityEnabled,
    jitterEnabled: policy.jitterEnabled,
    jitterTolerancePercent: policy.jitterTolerancePercent,
    jitterThresholdMultiplier: policy.jitterThresholdMultiplier,
    jitterAbsoluteToleranceMs: policy.jitterAbsoluteToleranceMs,
    jitterBaselineHours: policy.jitterBaselineHours,
    scoring: cloneScoring(policy.scoring),
    applyMode: policy.applyMode,
    models: [...policy.models],
    modelOrder: [...policy.modelOrder],
    minSamples: policy.minSamples,
    degradeStabilityScore: policy.degradeStabilityScore,
    recoveryStabilityScore: policy.recoveryStabilityScore,
    fastFailurePenaltyPercent: policy.fastFailurePenaltyPercent,
    fastFailureSeconds: policy.fastFailureSeconds,
    slowFailureSeconds: policy.slowFailureSeconds,
    cooldownMinutes: policy.cooldownMinutes,
    sampleMode: policy.sampleMode,
    explorationTrafficPercent: policy.explorationTrafficPercent,
    probeIntervalMinutes: policy.probeIntervalMinutes,
  }
}

function cloneScoring(
  scoring: SmartScheduleScoringFormValues
): SmartScheduleScoringFormValues {
  return {
    stabilityPercent: Number(scoring.stabilityPercent),
    curveExponent: Number(scoring.curveExponent),
    relativeWeightEnabled: scoring.relativeWeightEnabled,
    relativeWeightStartPercent: Number(scoring.relativeWeightStartPercent),
    relativeWeightFullPercent: Number(scoring.relativeWeightFullPercent),
    smart: {
      costRatioPercent: Number(scoring.smart.costRatioPercent),
      firstTokenPercent: Number(scoring.smart.firstTokenPercent),
      tpsPercent: Number(scoring.smart.tpsPercent),
    },
    ratio: {
      costRatioPercent: Number(scoring.ratio.costRatioPercent),
      firstTokenPercent: Number(scoring.ratio.firstTokenPercent),
      tpsPercent: Number(scoring.ratio.tpsPercent),
    },
  }
}
