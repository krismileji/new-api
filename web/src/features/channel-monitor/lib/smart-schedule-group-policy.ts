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
import {
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING,
} from '../constants'
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
    ...DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS,
    scoring: channelMonitorSmartScheduleScoringToForm(
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING
    ),
    applyMode: 'priority_weight',
    models: [],
    modelOrder: [],
    sampleMode: 'off',
    prioritySamplingEnabled: true,
  }

export function channelMonitorSmartScheduleScoringToForm(
  scoring: ChannelMonitorSmartScheduleScoring
): SmartScheduleScoringFormValues {
  return {
    stabilityPercent: scoring.stability_percent,
    primaryTrafficPercent: scoring.primary_traffic_percent ?? 90,
    primarySwitchThresholdPercent:
      scoring.primary_switch_threshold_percent ?? 3,
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
    primary_traffic_percent: scoring.primaryTrafficPercent,
    primary_switch_threshold_percent: scoring.primarySwitchThresholdPercent,
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
    jitterAbsoluteToleranceSeconds: policy.jitter_absolute_tolerance_seconds,
    jitterBaselineMinutes: policy.jitter_baseline_minutes,
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
    prioritySamplingEnabled: policy.priority_sampling_enabled,
    prioritySamplingIntervalMinutes: policy.priority_sampling_interval_minutes,
    prioritySamplingBasePercent: policy.priority_sampling_base_percent,
    prioritySamplingDecayPercent: policy.priority_sampling_decay_percent,
    prioritySamplingMinPercent: policy.priority_sampling_min_percent,
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
    jitter_absolute_tolerance_seconds: policy.jitterAbsoluteToleranceSeconds,
    jitter_baseline_minutes: policy.jitterBaselineMinutes,
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
    priority_sampling_enabled: policy.prioritySamplingEnabled,
    priority_sampling_interval_minutes: policy.prioritySamplingIntervalMinutes,
    priority_sampling_base_percent: policy.prioritySamplingBasePercent,
    priority_sampling_decay_percent: policy.prioritySamplingDecayPercent,
    priority_sampling_min_percent: policy.prioritySamplingMinPercent,
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
    jitterAbsoluteToleranceSeconds: policy.jitterAbsoluteToleranceSeconds,
    jitterBaselineMinutes: policy.jitterBaselineMinutes,
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
    prioritySamplingEnabled: policy.prioritySamplingEnabled,
    prioritySamplingIntervalMinutes: policy.prioritySamplingIntervalMinutes,
    prioritySamplingBasePercent: policy.prioritySamplingBasePercent,
    prioritySamplingDecayPercent: policy.prioritySamplingDecayPercent,
    prioritySamplingMinPercent: policy.prioritySamplingMinPercent,
  }
}

function cloneScoring(
  scoring: SmartScheduleScoringFormValues
): SmartScheduleScoringFormValues {
  return {
    stabilityPercent: Number(scoring.stabilityPercent),
    primaryTrafficPercent: Number(scoring.primaryTrafficPercent),
    primarySwitchThresholdPercent: Number(
      scoring.primarySwitchThresholdPercent
    ),
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
