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
import {
  channelMonitorSmartScheduleKTokensToTokens,
  channelMonitorSmartScheduleTokensToKTokens,
} from './smart-schedule-prompt-tokens'

type SmartScheduleScoringFormValues =
  ChannelMonitorSmartSchedulePolicyFormValues['scoring']

function legacyWindowSecondsToMinutes(
  seconds: number | undefined,
  defaultMinutes: number
): number {
  return seconds == null ? defaultMinutes : Math.ceil(seconds / 60)
}

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
  policies: ChannelMonitorSmartScheduleGroupPolicy[] = [],
  legacyStabilityWindowMinutes: number = DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.stabilityWindowMinutes
): ChannelMonitorSmartScheduleGroupPolicyFormValues[] {
  return policies.map((policy) => ({
    group: policy.group,
    strategy: policy.strategy,
    stabilityEnabled: policy.stability_enabled,
    stabilityWindowMinutes:
      policy.stability_window_minutes ?? legacyStabilityWindowMinutes,
    jitterEnabled: policy.jitter_enabled,
    jitterTolerancePercent: policy.jitter_tolerance_percent,
    jitterSlowThresholdSeconds: policy.jitter_slow_threshold_seconds,
    scoring: channelMonitorSmartScheduleScoringToForm(policy.scoring),
    applyMode: policy.apply_mode,
    models: [...policy.models],
    modelOrder: [...(policy.model_order ?? [])],
    minSamples: policy.min_samples,
    recoveryStabilityScore: policy.recovery_stability_score,
    fastFailurePenaltyPercent: policy.fast_failure_penalty_percent,
    fastFailureSeconds: policy.fast_failure_seconds,
    fastFailureSameChannelRetryCount:
      policy.fast_failure_same_channel_retry_count ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.fastFailureSameChannelRetryCount,
    fastFailureSameChannelRetryDelayMs:
      policy.fast_failure_same_channel_retry_delay_ms ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.fastFailureSameChannelRetryDelayMs,
    slowFailureSeconds: policy.slow_failure_seconds,
    burstFailureWindowMinutes:
      policy.burst_failure_window_minutes ??
      legacyWindowSecondsToMinutes(
        policy.burst_failure_window_seconds,
        DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.burstFailureWindowMinutes
      ),
    burstFailureWindowRequests:
      policy.burst_failure_window_requests ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.burstFailureWindowRequests,
    burstFailureThresholdPercent:
      policy.burst_failure_threshold_percent ??
      policy.burst_failure_threshold ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.burstFailureThresholdPercent,
    consecutiveFailureThreshold:
      policy.consecutive_failure_threshold ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.consecutiveFailureThreshold,
    recoverySuccessThreshold:
      policy.recovery_success_threshold ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.recoverySuccessThreshold,
    cooldownMinutes: policy.cooldown_minutes,
    sampleMode: policy.sample_mode,
    samplingOrder: policy.sampling_order,
    explorationTrafficPercent: policy.exploration_traffic_percent,
    explorationMaxPromptKTokens:
      policy.exploration_max_prompt_tokens == null
        ? DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.explorationMaxPromptKTokens
        : channelMonitorSmartScheduleTokensToKTokens(
            policy.exploration_max_prompt_tokens
          ),
    stabilityReleaseMaxPromptKTokens:
      policy.stability_release_max_prompt_tokens == null
        ? DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.stabilityReleaseMaxPromptKTokens
        : channelMonitorSmartScheduleTokensToKTokens(
            policy.stability_release_max_prompt_tokens
          ),
    probeIntervalMinutes: policy.probe_interval_minutes,
    degradedProbeEnabled: policy.degraded_probe_enabled ?? false,
    adaptiveSamplingEnabled: policy.adaptive_sampling_enabled,
    adaptiveSamplingBasePercent: policy.adaptive_sampling_base_percent,
    adaptiveSamplingMaxPercent: policy.adaptive_sampling_max_percent,
    adaptiveSamplingErrorWarningPercent:
      policy.adaptive_sampling_error_warning_percent,
    adaptiveSamplingErrorCriticalPercent:
      policy.adaptive_sampling_error_critical_percent,
    adaptiveSamplingFirstTokenWarningSeconds:
      policy.adaptive_sampling_first_token_warning_seconds,
    adaptiveSamplingFirstTokenCriticalSeconds:
      policy.adaptive_sampling_first_token_critical_seconds,
    adaptiveSamplingWindowMinutes:
      policy.adaptive_sampling_window_minutes ??
      legacyWindowSecondsToMinutes(
        policy.adaptive_sampling_window_seconds,
        DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.adaptiveSamplingWindowMinutes
      ),
    adaptiveSamplingWindowRequests:
      policy.adaptive_sampling_window_requests ??
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.adaptiveSamplingWindowRequests,
    adaptiveSamplingFirstTokenWarningRequestPercent:
      policy.adaptive_sampling_first_token_warning_request_percent,
    adaptiveSamplingRecoverRequestPercent:
      policy.adaptive_sampling_recover_request_percent,
    adaptiveSamplingSwitchConfirmRequestPercent:
      policy.adaptive_sampling_switch_confirm_request_percent,
    adaptiveSamplingMinComparableChannels:
      policy.adaptive_sampling_min_comparable_channels,
  }))
}

export function channelMonitorSmartScheduleGroupPoliciesToApi(
  policies: ChannelMonitorSmartScheduleGroupPolicyFormValues[]
): ChannelMonitorSmartScheduleGroupPolicy[] {
  return policies.map((policy) => ({
    group: policy.group,
    strategy: policy.strategy,
    stability_enabled: policy.stabilityEnabled,
    stability_window_minutes: policy.stabilityWindowMinutes,
    jitter_enabled: policy.jitterEnabled,
    jitter_tolerance_percent: policy.jitterTolerancePercent,
    jitter_slow_threshold_seconds: policy.jitterSlowThresholdSeconds,
    scoring: channelMonitorSmartScheduleScoringToApi(policy.scoring),
    apply_mode: policy.applyMode,
    models: policy.models,
    model_order: policy.modelOrder,
    min_samples: policy.minSamples,
    recovery_stability_score: policy.recoveryStabilityScore,
    fast_failure_penalty_percent: policy.fastFailurePenaltyPercent,
    fast_failure_seconds: policy.fastFailureSeconds,
    fast_failure_same_channel_retry_count:
      policy.fastFailureSameChannelRetryCount,
    fast_failure_same_channel_retry_delay_ms:
      policy.fastFailureSameChannelRetryDelayMs,
    slow_failure_seconds: policy.slowFailureSeconds,
    burst_failure_window_minutes: policy.burstFailureWindowMinutes,
    burst_failure_window_requests: policy.burstFailureWindowRequests,
    burst_failure_threshold_percent: policy.burstFailureThresholdPercent,
    consecutive_failure_threshold: policy.consecutiveFailureThreshold,
    recovery_success_threshold: policy.recoverySuccessThreshold,
    cooldown_minutes: policy.cooldownMinutes,
    sample_mode: policy.sampleMode,
    sampling_order: policy.samplingOrder,
    exploration_traffic_percent: policy.explorationTrafficPercent,
    exploration_max_prompt_tokens: channelMonitorSmartScheduleKTokensToTokens(
      policy.explorationMaxPromptKTokens
    ),
    stability_release_max_prompt_tokens:
      channelMonitorSmartScheduleKTokensToTokens(
        policy.stabilityReleaseMaxPromptKTokens
      ),
    probe_interval_minutes: policy.probeIntervalMinutes,
    degraded_probe_enabled: policy.degradedProbeEnabled,
    adaptive_sampling_enabled: policy.adaptiveSamplingEnabled,
    adaptive_sampling_base_percent: policy.adaptiveSamplingBasePercent,
    adaptive_sampling_max_percent: policy.adaptiveSamplingMaxPercent,
    adaptive_sampling_error_warning_percent:
      policy.adaptiveSamplingErrorWarningPercent,
    adaptive_sampling_error_critical_percent:
      policy.adaptiveSamplingErrorCriticalPercent,
    adaptive_sampling_first_token_warning_seconds:
      policy.adaptiveSamplingFirstTokenWarningSeconds,
    adaptive_sampling_first_token_critical_seconds:
      policy.adaptiveSamplingFirstTokenCriticalSeconds,
    adaptive_sampling_window_minutes: policy.adaptiveSamplingWindowMinutes,
    adaptive_sampling_window_requests: policy.adaptiveSamplingWindowRequests,
    adaptive_sampling_first_token_warning_request_percent:
      policy.adaptiveSamplingFirstTokenWarningRequestPercent,
    adaptive_sampling_recover_request_percent:
      policy.adaptiveSamplingRecoverRequestPercent,
    adaptive_sampling_switch_confirm_request_percent:
      policy.adaptiveSamplingSwitchConfirmRequestPercent,
    adaptive_sampling_min_comparable_channels:
      policy.adaptiveSamplingMinComparableChannels,
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
    stabilityWindowMinutes: policy.stabilityWindowMinutes,
    jitterEnabled: policy.jitterEnabled,
    jitterTolerancePercent: policy.jitterTolerancePercent,
    jitterSlowThresholdSeconds: policy.jitterSlowThresholdSeconds,
    scoring: cloneScoring(policy.scoring),
    applyMode: policy.applyMode,
    models: [...policy.models],
    modelOrder: [...policy.modelOrder],
    minSamples: policy.minSamples,
    recoveryStabilityScore: policy.recoveryStabilityScore,
    fastFailurePenaltyPercent: policy.fastFailurePenaltyPercent,
    fastFailureSeconds: policy.fastFailureSeconds,
    fastFailureSameChannelRetryCount: policy.fastFailureSameChannelRetryCount,
    fastFailureSameChannelRetryDelayMs:
      policy.fastFailureSameChannelRetryDelayMs,
    slowFailureSeconds: policy.slowFailureSeconds,
    burstFailureWindowMinutes: policy.burstFailureWindowMinutes,
    burstFailureWindowRequests: policy.burstFailureWindowRequests,
    burstFailureThresholdPercent: policy.burstFailureThresholdPercent,
    consecutiveFailureThreshold: policy.consecutiveFailureThreshold,
    recoverySuccessThreshold: policy.recoverySuccessThreshold,
    cooldownMinutes: policy.cooldownMinutes,
    sampleMode: policy.sampleMode,
    samplingOrder: policy.samplingOrder,
    explorationTrafficPercent: policy.explorationTrafficPercent,
    explorationMaxPromptKTokens: policy.explorationMaxPromptKTokens,
    stabilityReleaseMaxPromptKTokens: policy.stabilityReleaseMaxPromptKTokens,
    probeIntervalMinutes: policy.probeIntervalMinutes,
    degradedProbeEnabled: policy.degradedProbeEnabled,
    adaptiveSamplingEnabled: policy.adaptiveSamplingEnabled,
    adaptiveSamplingBasePercent: policy.adaptiveSamplingBasePercent,
    adaptiveSamplingMaxPercent: policy.adaptiveSamplingMaxPercent,
    adaptiveSamplingErrorWarningPercent:
      policy.adaptiveSamplingErrorWarningPercent,
    adaptiveSamplingErrorCriticalPercent:
      policy.adaptiveSamplingErrorCriticalPercent,
    adaptiveSamplingFirstTokenWarningSeconds:
      policy.adaptiveSamplingFirstTokenWarningSeconds,
    adaptiveSamplingFirstTokenCriticalSeconds:
      policy.adaptiveSamplingFirstTokenCriticalSeconds,
    adaptiveSamplingWindowMinutes: policy.adaptiveSamplingWindowMinutes,
    adaptiveSamplingWindowRequests: policy.adaptiveSamplingWindowRequests,
    adaptiveSamplingFirstTokenWarningRequestPercent:
      policy.adaptiveSamplingFirstTokenWarningRequestPercent,
    adaptiveSamplingRecoverRequestPercent:
      policy.adaptiveSamplingRecoverRequestPercent,
    adaptiveSamplingSwitchConfirmRequestPercent:
      policy.adaptiveSamplingSwitchConfirmRequestPercent,
    adaptiveSamplingMinComparableChannels:
      policy.adaptiveSamplingMinComparableChannels,
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
