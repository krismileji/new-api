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
import type {
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleScoring,
} from '../types'
import type {
  ChannelMonitorSettingsFormValues,
  ChannelMonitorSmartScheduleGroupPolicyFormValues,
  ChannelMonitorSmartSchedulePolicyFormValues,
} from './schema'

type SmartScheduleScoringFormValues =
  ChannelMonitorSmartSchedulePolicyFormValues['scoring']

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
  policies: ChannelMonitorSmartScheduleGroupPolicy[] = [],
  defaults: ChannelMonitorSmartSchedulePolicyFormValues
): ChannelMonitorSmartScheduleGroupPolicyFormValues[] {
  return policies.map((policy) =>
    createChannelMonitorSmartScheduleGroupPolicy(
      policy.group,
      resolveChannelMonitorSmartScheduleGroupPolicy(defaults, {
        strategy: policy.strategy,
        stabilityEnabled: policy.stability_enabled,
        scoring: policy.scoring
          ? channelMonitorSmartScheduleScoringToForm(policy.scoring)
          : undefined,
        applyMode: policy.apply_mode,
        models: policy.models,
        minSamples: policy.min_samples,
        minSuccessRate: policy.min_success_rate,
        cooldownMinutes: policy.cooldown_minutes,
      })
    )
  )
}

export function channelMonitorSmartScheduleGroupPoliciesToApi(
  policies: ChannelMonitorSmartScheduleGroupPolicyFormValues[]
): ChannelMonitorSmartScheduleGroupPolicy[] {
  return policies.map((policy) => ({
    group: policy.group,
    strategy: policy.strategy,
    stability_enabled: policy.stabilityEnabled,
    scoring: channelMonitorSmartScheduleScoringToApi(policy.scoring),
    apply_mode: policy.applyMode,
    models: policy.models,
    min_samples: policy.minSamples,
    min_success_rate: policy.minSuccessRate,
    cooldown_minutes: policy.cooldownMinutes,
  }))
}

export function getChannelMonitorSmartScheduleDefaultPolicy(
  values: ChannelMonitorSettingsFormValues
): ChannelMonitorSmartSchedulePolicyFormValues {
  return {
    strategy: values.smartScheduleStrategy,
    stabilityEnabled: values.smartScheduleStabilityEnabled,
    scoring: cloneScoring(values.smartScheduleScoring),
    applyMode: values.smartScheduleApplyMode,
    models: [...values.smartScheduleModels],
    minSamples: Number(values.smartScheduleMinSamples),
    minSuccessRate: Number(values.smartScheduleMinSuccessRate),
    cooldownMinutes: Number(values.smartScheduleCooldownMinutes),
  }
}

export function resolveChannelMonitorSmartScheduleGroupPolicy(
  defaults: ChannelMonitorSmartSchedulePolicyFormValues,
  override?: Partial<ChannelMonitorSmartSchedulePolicyFormValues>
): ChannelMonitorSmartSchedulePolicyFormValues {
  return {
    strategy: override?.strategy ?? defaults.strategy,
    stabilityEnabled: override?.stabilityEnabled ?? defaults.stabilityEnabled,
    scoring: cloneScoring(override?.scoring ?? defaults.scoring),
    applyMode: override?.applyMode ?? defaults.applyMode,
    models: [...(override?.models ?? defaults.models)],
    minSamples: override?.minSamples ?? defaults.minSamples,
    minSuccessRate: override?.minSuccessRate ?? defaults.minSuccessRate,
    cooldownMinutes: override?.cooldownMinutes ?? defaults.cooldownMinutes,
  }
}

export function createChannelMonitorSmartScheduleGroupPolicy(
  group: string,
  policy: ChannelMonitorSmartSchedulePolicyFormValues
): ChannelMonitorSmartScheduleGroupPolicyFormValues {
  return {
    group,
    strategy: policy.strategy,
    stabilityEnabled: policy.stabilityEnabled,
    scoring: cloneScoring(policy.scoring),
    applyMode: policy.applyMode,
    models: [...policy.models],
    minSamples: policy.minSamples,
    minSuccessRate: policy.minSuccessRate,
    cooldownMinutes: policy.cooldownMinutes,
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
