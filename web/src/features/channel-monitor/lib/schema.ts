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
import * as z from 'zod'

import {
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING,
} from '../constants'
import type {
  ChannelMonitorEmailNotificationType,
  ChannelMonitorPolicyAction,
  ChannelMonitorSmartScheduleApplyMode,
  ChannelMonitorSmartScheduleSampleMode,
  ChannelMonitorSmartScheduleStrategy,
  ChannelMonitorUpstreamAuthType,
  ChannelMonitorUpstreamType,
} from '../types'
import { CHANNEL_MONITOR_SUBSCRIPTION_DAYS } from './cost-conversion'
import { CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES } from './email-notification'

export const MAX_MONITOR_RATIO = 1_000_000
export const MAX_BALANCE_THRESHOLD = 1_000_000_000_000
export const MAX_COST_CONVERSION_AMOUNT = 1_000_000_000_000
export const MAX_CUSTOM_UPSTREAM_BALANCE = 1_000_000_000_000_000
export const MAX_CUSTOM_UPSTREAM_ENTRIES = 32
export const MAX_CUSTOM_UPSTREAM_BODY_BYTES = 49_152
export const MAX_AUTO_UPDATE_INTERVAL_MINUTES = 525_600
export const MAX_AUTO_UPDATE_RETRY_COUNT = 10
export const MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS = 1
export const MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS = 600
export const DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS = 30
export const MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT = 1
export const MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT = 100
export const DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT = 2
export const MAX_CHANNEL_CONCURRENCY_LIMIT = 100_000
export const MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS = 1
export const MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS = 3_650
export const DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS = 120
export const DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS = 14
export const DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS = 90
export const DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS = 365
export const MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS = 600
export const MAX_ERROR_MESSAGE_MAPPING_ENTRIES = 100
export const MAX_ERROR_MESSAGE_MAPPING_KEY_LENGTH = 128
export const MAX_ERROR_MESSAGE_MAPPING_MESSAGE_LENGTH = 4096
export const DEFAULT_PROBE_RESPONSE_MATCH_INPUT = 'hi'
export const DEFAULT_PROBE_RESPONSE_TEXT = 'Hi. What are you working on?'
export const DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS = 500
export const DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS = 2_000
export const DEFAULT_PROBE_RESPONSE_INPUT_TOKENS = 4_387
export const DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS = 172
export const DEFAULT_PROBE_RESPONSE_CACHED_TOKENS = 4_001
export const DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS = 12
export const MAX_PROBE_RESPONSE_MATCH_INPUT_LENGTH = 4_096
export const MAX_PROBE_RESPONSE_TEXT_LENGTH = 16_384
export const MAX_PROBE_RESPONSE_DELAY_MS = 600_000
export const MAX_PROBE_RESPONSE_TOKEN_COUNT = 1_000_000
export const MAX_SMART_SCHEDULE_MIN_SAMPLES = 100_000
export const MAX_SMART_SCHEDULE_MODEL_COUNT = 100
export const MAX_SMART_SCHEDULE_GROUP_COUNT = 100
export const MAX_SMART_SCHEDULE_COOLDOWN_MINUTES = 525_600
export const MAX_SMART_SCHEDULE_EXPLORATION_TRAFFIC_PERCENT = 20
export const MAX_SMART_SCHEDULE_EXPLORATION_PROMPT_TOKENS = 1_000_000
export const MAX_SMART_SCHEDULE_PROBE_INTERVAL_MINUTES = 525_600
export const MIN_SMART_SCHEDULE_WINDOW_MINUTES = 1
export const MAX_SMART_SCHEDULE_WINDOW_MINUTES = 43_200
export const DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS = 30
export const MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS = 300
export const MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_INTERVAL_MINUTES = 1_440
export const MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_BASE_PERCENT = 0.1
export const MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_BASE_PERCENT = 20
export const MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_DECAY_PERCENT = 1
export const MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_DECAY_PERCENT = 100
export const MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_MIN_PERCENT = 0.01
export const MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_MIN_PERCENT = 5
export const MAX_SMART_SCHEDULE_JITTER_TOLERANCE_PERCENT = 50
export const MIN_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT = 51
export const MAX_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT = 99
export const MAX_SMART_SCHEDULE_PRIMARY_SWITCH_THRESHOLD_PERCENT = 100
export const MAX_SMART_SCHEDULE_JITTER_SLOW_THRESHOLD_SECONDS = 60
export const MAX_SMART_SCHEDULE_FAST_FAILURE_SAME_CHANNEL_RETRY_COUNT = 10
export const MAX_SMART_SCHEDULE_FAST_FAILURE_SAME_CHANNEL_RETRY_DELAY_MS = 60_000

const channelMonitorSmartScheduleApplyModes = [
  'weight',
  'priority_weight',
] as const satisfies readonly ChannelMonitorSmartScheduleApplyMode[]

const channelMonitorSmartScheduleStrategies = [
  'ratio',
  'first_token',
  'tps',
  'smart',
] as const satisfies readonly ChannelMonitorSmartScheduleStrategy[]

const channelMonitorSmartScheduleSampleModes = [
  'off',
  'traffic',
  'probe',
] as const satisfies readonly ChannelMonitorSmartScheduleSampleMode[]

const channelMonitorPolicyActions = [
  'none',
  'update_group_ratio',
  'disable_channel',
  'remove_from_group',
] as const satisfies readonly ChannelMonitorPolicyAction[]

const smartSchedulePercentageSchema = z.coerce
  .number()
  .finite('占比必须是有效数字')
  .min(0, '占比不能小于 0%')
  .max(100, '占比不能超过 100%')

const smartScheduleMetricPercentagesSchema = z.object({
  costRatioPercent: smartSchedulePercentageSchema,
  firstTokenPercent: smartSchedulePercentageSchema,
  tpsPercent: smartSchedulePercentageSchema,
})

const smartScheduleScoringSchema = z
  .object({
    stabilityPercent: smartSchedulePercentageSchema,
    primaryTrafficPercent: z.coerce
      .number()
      .finite('主渠道目标流量必须是有效数字')
      .min(
        MIN_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT,
        '主渠道目标流量不能小于 51%'
      )
      .max(
        MAX_SMART_SCHEDULE_PRIMARY_TRAFFIC_PERCENT,
        '主渠道目标流量不能超过 99%'
      ),
    primarySwitchThresholdPercent: z.coerce
      .number()
      .finite('主渠道切换分差必须是有效数字')
      .min(0, '主渠道切换分差不能小于 0%')
      .max(
        MAX_SMART_SCHEDULE_PRIMARY_SWITCH_THRESHOLD_PERCENT,
        '主渠道切换分差不能超过 100%'
      ),
    smart: smartScheduleMetricPercentagesSchema,
    ratio: smartScheduleMetricPercentagesSchema,
  })
  .superRefine((values, context) => {
    const scoringGroups = [
      {
        key: 'smart' as const,
        label: '智能调度',
        percentages: values.smart,
      },
      {
        key: 'ratio' as const,
        label: '按成本倍率调度',
        percentages: values.ratio,
      },
    ]
    for (const group of scoringGroups) {
      const total =
        group.percentages.costRatioPercent +
        group.percentages.firstTokenPercent +
        group.percentages.tpsPercent
      if (Math.abs(total - 100) > 0.000001) {
        context.addIssue({
          code: 'custom',
          path: [group.key, 'tpsPercent'],
          message: `${group.label}的指标占比合计必须为 100%`,
        })
      }
    }
    if (values.ratio.costRatioPercent <= 0) {
      context.addIssue({
        code: 'custom',
        path: ['ratio', 'costRatioPercent'],
        message: '按成本倍率调度的成本倍率占比必须大于 0%',
      })
    }
  })

const smartScheduleModelsSchema = z
  .array(
    z
      .string()
      .trim()
      .min(1, '基准模型不能为空')
      .max(255, '基准模型不能超过 255 个字符')
  )
  .max(MAX_SMART_SCHEDULE_MODEL_COUNT, '基准模型不能超过 100 个')

const smartScheduleMinSamplesSchema = z.coerce
  .number()
  .int('最少样本数必须是整数')
  .min(1, '最少样本数不能小于 1')
  .max(MAX_SMART_SCHEDULE_MIN_SAMPLES, '最少样本数不能超过 100000')

const smartScheduleStabilityScoreSchema = z.coerce
  .number()
  .finite('稳定性得分必须是有效数字')
  .min(0, '稳定性得分不能小于 0%')
  .max(100, '稳定性得分不能超过 100%')

const smartScheduleFastFailurePenaltySchema = z.coerce
  .number()
  .finite('快速失败惩罚必须是有效数字')
  .min(0, '快速失败惩罚不能小于 0%')
  .max(100, '快速失败惩罚不能超过 100%')

const smartScheduleFailureSecondsSchema = z.coerce
  .number()
  .finite('失败耗时界限必须是有效数字')
  .gt(0, '失败耗时界限必须大于 0 秒')
  .max(60, '失败耗时界限不能超过 60 秒')

const smartScheduleFastFailureSecondsSchema = z.coerce
  .number()
  .finite('快速失败界限必须是有效数字')
  .gt(0, '快速失败界限必须大于 0 秒')
  .lt(60, '快速失败界限必须小于 60 秒')

const smartScheduleFastFailureSameChannelRetryCountSchema = z.preprocess(
  (value) => (value === '' ? Number.NaN : value),
  z.coerce
    .number()
    .int('快速失败同渠道重试次数必须是整数')
    .min(0, '快速失败同渠道重试次数不能小于 0 次')
    .max(
      MAX_SMART_SCHEDULE_FAST_FAILURE_SAME_CHANNEL_RETRY_COUNT,
      '快速失败同渠道重试次数不能超过 10 次'
    )
)

const smartScheduleFastFailureSameChannelRetryDelaySchema = z.preprocess(
  (value) => (value === '' ? Number.NaN : value),
  z.coerce
    .number()
    .int('快速失败同渠道重试延迟必须是整数')
    .min(0, '快速失败同渠道重试延迟不能小于 0 毫秒')
    .max(
      MAX_SMART_SCHEDULE_FAST_FAILURE_SAME_CHANNEL_RETRY_DELAY_MS,
      '快速失败同渠道重试延迟不能超过 60000 毫秒'
    )
)

const smartScheduleRuntimeFailureThresholdSchema = z.coerce
  .number()
  .int('运行时失败阈值必须是整数')
  .min(1, '运行时失败阈值不能小于 1 次')
  .max(100, '运行时失败阈值不能超过 100 次')

const smartScheduleBurstFailureWindowSchema = z.coerce
  .number()
  .int('保护失败窗口必须是整数')
  .min(1, '保护失败窗口不能小于 1 秒')
  .max(300, '保护失败窗口不能超过 300 秒')

const smartScheduleJitterToleranceSchema = z.coerce
  .number()
  .finite('允许抖动必须是有效数字')
  .min(0, '允许抖动不能小于 0%')
  .max(MAX_SMART_SCHEDULE_JITTER_TOLERANCE_PERCENT, '允许抖动不能超过 50%')

const smartScheduleJitterSlowThresholdSchema = z.preprocess(
  (value) => (value === '' ? undefined : value),
  z.coerce
    .number()
    .finite('慢成功阈值必须是有效数字')
    .min(0, '慢成功阈值不能小于 0 秒')
    .max(
      MAX_SMART_SCHEDULE_JITTER_SLOW_THRESHOLD_SECONDS,
      '慢成功阈值不能超过 60 秒'
    )
)

const smartScheduleCooldownSchema = z.coerce
  .number()
  .int('降级时长必须是整数')
  .min(1, '降级时长不能小于 1 分钟')
  .max(MAX_SMART_SCHEDULE_COOLDOWN_MINUTES, '降级时长不能超过 525600 分钟')

const smartScheduleExplorationTrafficSchema = z.coerce
  .number()
  .finite('探索流量必须是有效数字')
  .gt(0, '探索流量必须大于 0%')
  .max(MAX_SMART_SCHEDULE_EXPLORATION_TRAFFIC_PERCENT, '探索流量不能超过 20%')

const smartScheduleExplorationPromptTokensSchema = z.preprocess(
  (value) => (value === '' ? undefined : value),
  z.coerce
    .number()
    .int('探索请求上限必须是整数')
    .min(0, '探索请求上限不能小于 0 Token')
    .max(
      MAX_SMART_SCHEDULE_EXPLORATION_PROMPT_TOKENS,
      '探索请求上限不能超过 1000000 Token'
    )
)

const smartScheduleStabilityReleasePromptTokensSchema = z.preprocess(
  (value) => (value === '' ? undefined : value),
  z.coerce
    .number()
    .int('稳定性释放请求上限必须是整数')
    .min(0, '稳定性释放请求上限不能小于 0 Token')
    .max(
      MAX_SMART_SCHEDULE_EXPLORATION_PROMPT_TOKENS,
      '稳定性释放请求上限不能超过 1000000 Token'
    )
)

const smartScheduleProbeIntervalSchema = z.coerce
  .number()
  .int('探测间隔必须是整数')
  .min(1, '探测间隔不能小于 1 分钟')
  .max(
    MAX_SMART_SCHEDULE_PROBE_INTERVAL_MINUTES,
    '探测间隔不能超过 525600 分钟'
  )

const smartSchedulePrioritySamplingIntervalSchema = z.coerce
  .number()
  .int('轮转间隔必须是整数')
  .min(1, '轮转间隔不能小于 1 分钟')
  .max(
    MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_INTERVAL_MINUTES,
    '轮转间隔不能超过 1440 分钟'
  )

const smartSchedulePrioritySamplingBasePercentSchema = z.coerce
  .number()
  .finite('基础采样比例必须是有效数字')
  .min(
    MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_BASE_PERCENT,
    '基础采样比例不能小于 0.1%'
  )
  .max(
    MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_BASE_PERCENT,
    '基础采样比例不能超过 20%'
  )

const smartSchedulePrioritySamplingDecayPercentSchema = z.coerce
  .number()
  .finite('排名递减比例必须是有效数字')
  .min(
    MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_DECAY_PERCENT,
    '排名递减比例不能小于 1%'
  )
  .max(
    MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_DECAY_PERCENT,
    '排名递减比例不能超过 100%'
  )

const smartSchedulePrioritySamplingMinPercentSchema = z.coerce
  .number()
  .finite('最低采样比例必须是有效数字')
  .min(
    MIN_SMART_SCHEDULE_PRIORITY_SAMPLING_MIN_PERCENT,
    '最低采样比例不能小于 0.01%'
  )
  .max(
    MAX_SMART_SCHEDULE_PRIORITY_SAMPLING_MIN_PERCENT,
    '最低采样比例不能超过 5%'
  )

const smartSchedulePolicyShape = {
  strategy: z.enum(channelMonitorSmartScheduleStrategies),
  stabilityEnabled: z.boolean(),
  jitterEnabled: z.boolean(),
  jitterTolerancePercent: smartScheduleJitterToleranceSchema,
  jitterSlowThresholdSeconds: smartScheduleJitterSlowThresholdSchema,
  scoring: smartScheduleScoringSchema,
  applyMode: z.enum(channelMonitorSmartScheduleApplyModes),
  models: smartScheduleModelsSchema,
  modelOrder: smartScheduleModelsSchema.default([]),
  minSamples: smartScheduleMinSamplesSchema,
  degradeStabilityScore: smartScheduleStabilityScoreSchema,
  recoveryStabilityScore: smartScheduleStabilityScoreSchema,
  fastFailurePenaltyPercent: smartScheduleFastFailurePenaltySchema,
  fastFailureSeconds: smartScheduleFastFailureSecondsSchema,
  fastFailureSameChannelRetryCount:
    smartScheduleFastFailureSameChannelRetryCountSchema.default(0),
  fastFailureSameChannelRetryDelayMs:
    smartScheduleFastFailureSameChannelRetryDelaySchema.default(
      DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS.fastFailureSameChannelRetryDelayMs
    ),
  slowFailureSeconds: smartScheduleFailureSecondsSchema,
  burstFailureWindowSeconds: smartScheduleBurstFailureWindowSchema.default(30),
  consecutiveFailureThreshold:
    smartScheduleRuntimeFailureThresholdSchema.default(2),
  burstFailureThreshold: smartScheduleRuntimeFailureThresholdSchema.default(3),
  recoverySuccessThreshold:
    smartScheduleRuntimeFailureThresholdSchema.default(2),
  cooldownMinutes: smartScheduleCooldownSchema,
  sampleMode: z.enum(channelMonitorSmartScheduleSampleModes),
  explorationTrafficPercent: smartScheduleExplorationTrafficSchema,
  explorationMaxPromptTokens: smartScheduleExplorationPromptTokensSchema,
  stabilityReleaseMaxPromptTokens:
    smartScheduleStabilityReleasePromptTokensSchema,
  probeIntervalMinutes: smartScheduleProbeIntervalSchema,
  degradedProbeEnabled: z.boolean().default(false),
  prioritySamplingEnabled: z.boolean(),
  prioritySamplingIntervalMinutes: smartSchedulePrioritySamplingIntervalSchema,
  prioritySamplingBasePercent: smartSchedulePrioritySamplingBasePercentSchema,
  prioritySamplingDecayPercent: smartSchedulePrioritySamplingDecayPercentSchema,
  prioritySamplingMinPercent: smartSchedulePrioritySamplingMinPercentSchema,
}

function normalizeInactiveSmartSchedulePolicy(value: unknown): unknown {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return value
  }

  const policy = value as Record<string, unknown>
  const normalized: Record<string, unknown> = { ...policy }
  const defaults = DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS

  if (policy.sampleMode !== 'traffic') {
    normalized.explorationTrafficPercent = defaults.explorationTrafficPercent
    normalized.explorationMaxPromptTokens = defaults.explorationMaxPromptTokens
  }
  if (
    policy.sampleMode !== 'probe' &&
    !(policy.stabilityEnabled === true && policy.degradedProbeEnabled === true)
  ) {
    normalized.probeIntervalMinutes = defaults.probeIntervalMinutes
  }

  if (
    policy.applyMode !== 'priority_weight' ||
    policy.prioritySamplingEnabled !== true
  ) {
    normalized.prioritySamplingIntervalMinutes =
      defaults.prioritySamplingIntervalMinutes
    normalized.prioritySamplingBasePercent =
      defaults.prioritySamplingBasePercent
    normalized.prioritySamplingDecayPercent =
      defaults.prioritySamplingDecayPercent
    normalized.prioritySamplingMinPercent = defaults.prioritySamplingMinPercent
  }
  if (policy.applyMode !== 'priority_weight') {
    normalized.prioritySamplingEnabled = false
  }

  if (policy.stabilityEnabled !== true) {
    normalized.minSamples = defaults.minSamples
    normalized.degradeStabilityScore = defaults.degradeStabilityScore
    normalized.recoveryStabilityScore = defaults.recoveryStabilityScore
    normalized.fastFailurePenaltyPercent = defaults.fastFailurePenaltyPercent
    normalized.fastFailureSeconds = defaults.fastFailureSeconds
    normalized.fastFailureSameChannelRetryCount =
      defaults.fastFailureSameChannelRetryCount
    normalized.fastFailureSameChannelRetryDelayMs =
      defaults.fastFailureSameChannelRetryDelayMs
    normalized.slowFailureSeconds = defaults.slowFailureSeconds
    normalized.burstFailureWindowSeconds = defaults.burstFailureWindowSeconds
    normalized.consecutiveFailureThreshold =
      defaults.consecutiveFailureThreshold
    normalized.burstFailureThreshold = defaults.burstFailureThreshold
    normalized.recoverySuccessThreshold = defaults.recoverySuccessThreshold
    normalized.cooldownMinutes = defaults.cooldownMinutes
    normalized.stabilityReleaseMaxPromptTokens =
      defaults.stabilityReleaseMaxPromptTokens
    normalized.degradedProbeEnabled = defaults.degradedProbeEnabled
    if (
      typeof policy.scoring === 'object' &&
      policy.scoring !== null &&
      !Array.isArray(policy.scoring)
    ) {
      normalized.scoring = {
        ...(policy.scoring as Record<string, unknown>),
        stabilityPercent:
          DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING.stability_percent,
      }
    }
  }

  if (policy.stabilityEnabled !== true || policy.jitterEnabled !== true) {
    normalized.jitterTolerancePercent = defaults.jitterTolerancePercent
    normalized.jitterSlowThresholdSeconds = defaults.jitterSlowThresholdSeconds
  }

  return normalized
}

function validateSmartSchedulePolicy(
  values: {
    applyMode: ChannelMonitorSmartScheduleApplyMode
    sampleMode: ChannelMonitorSmartScheduleSampleMode
    fastFailureSeconds: number
    slowFailureSeconds: number
  },
  context: z.RefinementCtx
) {
  if (values.applyMode === 'weight' && values.sampleMode === 'traffic') {
    context.addIssue({
      code: 'custom',
      path: ['sampleMode'],
      message: '探索流量只适用于优先级分层 + 权重',
    })
  }
  if (values.slowFailureSeconds <= values.fastFailureSeconds) {
    context.addIssue({
      code: 'custom',
      path: ['slowFailureSeconds'],
      message: '慢失败界限必须大于快速失败界限',
    })
  }
}

export function createChannelMonitorSmartSchedulePolicySchema() {
  return z.preprocess(
    normalizeInactiveSmartSchedulePolicy,
    z.object(smartSchedulePolicyShape).superRefine(validateSmartSchedulePolicy)
  )
}

const smartScheduleGroupPolicySchema = z.preprocess(
  normalizeInactiveSmartSchedulePolicy,
  z
    .object({
      ...smartSchedulePolicyShape,
      group: z
        .string()
        .trim()
        .min(1, '分组名称不能为空')
        .max(64, '分组名称不能超过 64 个字符'),
    })
    .superRefine(validateSmartSchedulePolicy)
)

export function createChannelRatioSchema() {
  return z.object({
    ratio: z.coerce
      .number()
      .finite('倍率必须是有效数字')
      .min(0, '倍率不能小于 0')
      .max(MAX_MONITOR_RATIO, '倍率不能超过 1000000'),
    remark: z.string().max(255, '备注不能超过 255 个字符'),
  })
}

export function createGroupRatioSchema() {
  return z.object({
    ratio: z.coerce
      .number()
      .finite('倍率必须是有效数字')
      .min(0, '倍率不能小于 0')
      .max(MAX_MONITOR_RATIO, '倍率不能超过 1000000'),
  })
}

export function createChannelConcurrencyLimitSchema() {
  return z.object({
    concurrencyLimit: z.preprocess(
      (value) => (value === '' ? undefined : value),
      z.coerce
        .number({ error: '并发限制必须是有效数字' })
        .finite('并发限制必须是有效数字')
        .int('并发限制必须是整数')
        .min(0, '并发限制不能小于 0')
        .max(MAX_CHANNEL_CONCURRENCY_LIMIT, '并发限制不能超过 100000')
    ),
  })
}

const errorMessageMappingSchema = z
  .string()
  .default('')
  .superRefine((value, context) => {
    const raw = value.trim()
    if (!raw || raw === 'null') return

    let parsed: unknown
    try {
      parsed = JSON.parse(raw)
    } catch {
      context.addIssue({
        code: 'custom',
        message: '错误信息映射必须是有效的 JSON 对象',
      })
      return
    }
    if (
      typeof parsed !== 'object' ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      context.addIssue({
        code: 'custom',
        message: '错误信息映射必须是 JSON 对象',
      })
      return
    }

    const entries = Object.entries(parsed as Record<string, unknown>)
    if (entries.length > MAX_ERROR_MESSAGE_MAPPING_ENTRIES) {
      context.addIssue({
        code: 'custom',
        message: `错误信息映射最多支持 ${MAX_ERROR_MESSAGE_MAPPING_ENTRIES} 条规则`,
      })
      return
    }
    for (const [code, message] of entries) {
      if (!code.trim() || code.length > MAX_ERROR_MESSAGE_MAPPING_KEY_LENGTH) {
        context.addIssue({
          code: 'custom',
          message: `错误码不能为空且不能超过 ${MAX_ERROR_MESSAGE_MAPPING_KEY_LENGTH} 个字符`,
        })
        return
      }
      if (typeof message !== 'string' || !message.trim()) {
        context.addIssue({
          code: 'custom',
          message: '错误信息必须是非空字符串',
        })
        return
      }
      if (message.length > MAX_ERROR_MESSAGE_MAPPING_MESSAGE_LENGTH) {
        context.addIssue({
          code: 'custom',
          message: `错误信息不能超过 ${MAX_ERROR_MESSAGE_MAPPING_MESSAGE_LENGTH} 个字符`,
        })
        return
      }
    }
  })

export function createChannelMonitorSettingsSchema() {
  return z
    .object({
      autoUpdateIntervalMinutes: z.coerce
        .number()
        .int('自动更新间隔必须是整数')
        .min(0, '自动更新间隔不能小于 0')
        .max(
          MAX_AUTO_UPDATE_INTERVAL_MINUTES,
          '自动更新间隔不能超过 525600 分钟'
        ),
      autoUpdateRetryCount: z.coerce
        .number()
        .int('失败重试次数必须是整数')
        .min(0, '失败重试次数不能小于 0')
        .max(MAX_AUTO_UPDATE_RETRY_COUNT, '失败重试次数不能超过 10 次'),
      upstreamRequestTimeoutSeconds: z.coerce
        .number()
        .int('上游请求超时时间必须是整数')
        .min(
          MIN_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
          '上游请求超时时间不能小于 1 秒'
        )
        .max(
          MAX_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
          '上游请求超时时间不能超过 600 秒'
        ),
      autoUpdateConsecutiveFailureLimit: z.coerce
        .number()
        .int('连续失败停止次数必须是整数')
        .min(
          MIN_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
          '连续失败停止次数不能小于 1 次'
        )
        .max(
          MAX_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
          '连续失败停止次数不能超过 100 次'
        ),
      autoDisableOnUpdateFailure: z.boolean(),
      autoEnableOnCostRatioRecovery: z.boolean(),
      autoEnableOnBalanceRecovery: z.boolean(),
      costRetentionDays: z.coerce
        .number()
        .int('成本数据保留天数必须是整数')
        .min(
          MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '成本数据保留天数不能小于 1 天'
        )
        .max(
          MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '成本数据保留天数不能超过 3650 天'
        ),
      executionDetailRetentionDays: z.coerce
        .number()
        .int('调度执行明细保留天数必须是整数')
        .min(
          MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '调度执行明细保留天数不能小于 1 天'
        )
        .max(
          MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '调度执行明细保留天数不能超过 3650 天'
        ),
      taskRetentionDays: z.coerce
        .number()
        .int('监控任务保留天数必须是整数')
        .min(
          MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '监控任务保留天数不能小于 1 天'
        )
        .max(
          MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '监控任务保留天数不能超过 3650 天'
        ),
      ratioHistoryRetentionDays: z.coerce
        .number()
        .int('倍率历史保留天数必须是整数')
        .min(
          MIN_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '倍率历史保留天数不能小于 1 天'
        )
        .max(
          MAX_CHANNEL_MONITOR_COST_RETENTION_DAYS,
          '倍率历史保留天数不能超过 3650 天'
        ),
      emailNotificationEnabled: z.boolean(),
      emailNotificationTypes: z
        .array(
          z.enum(
            CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES
          ) as z.ZodType<ChannelMonitorEmailNotificationType>
        )
        .max(
          CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES.length,
          '邮件通知类型选择无效'
        ),
      notificationEmail: z
        .string()
        .trim()
        .max(254, '通知邮箱不能超过 254 个字符')
        .refine(
          (value) =>
            value === '' || z.string().email().safeParse(value).success,
          '请输入有效的通知邮箱'
        ),
      errorMessageMapping: errorMessageMappingSchema,
      probeResponseEnabled: z.boolean(),
      probeResponseMatchInput: z
        .string()
        .trim()
        .min(1, '探针匹配输入不能为空')
        .max(
          MAX_PROBE_RESPONSE_MATCH_INPUT_LENGTH,
          '探针匹配输入不能超过 4096 个字符'
        ),
      probeResponseText: z
        .string()
        .trim()
        .min(1, '探针响应文本不能为空')
        .max(
          MAX_PROBE_RESPONSE_TEXT_LENGTH,
          '探针响应文本不能超过 16384 个字符'
        ),
      probeResponseMinDelayMs: z.coerce
        .number()
        .int('探针最小延迟必须是整数')
        .min(0, '探针最小延迟不能小于 0 毫秒')
        .max(MAX_PROBE_RESPONSE_DELAY_MS, '探针最小延迟不能超过 600000 毫秒'),
      probeResponseMaxDelayMs: z.coerce
        .number()
        .int('探针最大延迟必须是整数')
        .min(0, '探针最大延迟不能小于 0 毫秒')
        .max(MAX_PROBE_RESPONSE_DELAY_MS, '探针最大延迟不能超过 600000 毫秒'),
      probeResponseInputTokens: z.coerce
        .number()
        .int('探针输入 Token 必须是整数')
        .min(0, '探针输入 Token 不能小于 0')
        .max(MAX_PROBE_RESPONSE_TOKEN_COUNT, '探针输入 Token 不能超过 1000000'),
      probeResponseCacheWriteTokens: z.coerce
        .number()
        .int('探针缓存写 Token 必须是整数')
        .min(0, '探针缓存写 Token 不能小于 0')
        .max(
          MAX_PROBE_RESPONSE_TOKEN_COUNT,
          '探针缓存写 Token 不能超过 1000000'
        ),
      probeResponseCachedTokens: z.coerce
        .number()
        .int('探针缓存命中 Token 必须是整数')
        .min(0, '探针缓存命中 Token 不能小于 0')
        .max(
          MAX_PROBE_RESPONSE_TOKEN_COUNT,
          '探针缓存命中 Token 不能超过 1000000'
        ),
      probeResponseOutputTokens: z.coerce
        .number()
        .int('探针输出 Token 必须是整数')
        .min(0, '探针输出 Token 不能小于 0')
        .max(MAX_PROBE_RESPONSE_TOKEN_COUNT, '探针输出 Token 不能超过 1000000'),
      relayResponseHeaderTimeoutSeconds: z.coerce
        .number()
        .int('上游响应等待时间必须是整数')
        .min(0, '上游响应等待时间不能小于 0 秒')
        .max(
          MAX_RELAY_RESPONSE_HEADER_TIMEOUT_SECONDS,
          '上游响应等待时间不能超过 600 秒'
        ),
      smartScheduleEnabled: z.boolean(),
      smartScheduleGroupPolicies: z
        .array(smartScheduleGroupPolicySchema)
        .max(MAX_SMART_SCHEDULE_GROUP_COUNT, '分组调度策略不能超过 100 个')
        .default([]),
      smartScheduleIntervalMinutes: z.coerce
        .number()
        .int('智能调度间隔必须是整数')
        .min(1, '智能调度间隔不能小于 1 分钟')
        .max(
          MAX_AUTO_UPDATE_INTERVAL_MINUTES,
          '智能调度间隔不能超过 525600 分钟'
        ),
      smartSchedulePerformanceWindowMinutes: z.coerce
        .number()
        .int('性能窗口必须是整数')
        .min(MIN_SMART_SCHEDULE_WINDOW_MINUTES, '性能窗口不能小于 1 分钟')
        .max(MAX_SMART_SCHEDULE_WINDOW_MINUTES, '性能窗口不能超过 43200 分钟'),
      smartScheduleStabilityWindowMinutes: z.coerce
        .number()
        .int('稳定性评分窗口必须是整数')
        .min(
          MIN_SMART_SCHEDULE_WINDOW_MINUTES,
          '稳定性评分窗口不能小于 1 分钟'
        )
        .max(
          MAX_SMART_SCHEDULE_WINDOW_MINUTES,
          '稳定性评分窗口不能超过 43200 分钟'
        ),
      smartScheduleRateLimitCooldownSeconds: z.coerce
        .number()
        .int('429 冷却时间必须是整数')
        .min(0, '429 冷却时间不能小于 0 秒')
        .max(
          MAX_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
          '429 冷却时间不能超过 300 秒'
        )
        .default(DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS),
      smartScheduleForceReset: z.boolean(),
    })
    .superRefine((values, context) => {
      if (values.taskRetentionDays < values.executionDetailRetentionDays) {
        context.addIssue({
          code: 'custom',
          path: ['taskRetentionDays'],
          message: '监控任务保留天数不能小于调度执行明细保留天数',
        })
      }
      if (values.emailNotificationEnabled && !values.notificationEmail) {
        context.addIssue({
          code: 'custom',
          path: ['notificationEmail'],
          message: '开启邮件通知时请填写通知邮箱',
        })
      }
      if (
        values.emailNotificationEnabled &&
        values.emailNotificationTypes.length === 0
      ) {
        context.addIssue({
          code: 'custom',
          path: ['emailNotificationTypes'],
          message: '开启邮件通知时请至少选择一种通知类型',
        })
      }
      if (
        values.smartScheduleEnabled &&
        values.smartScheduleGroupPolicies.length === 0
      ) {
        context.addIssue({
          code: 'custom',
          path: ['smartScheduleGroupPolicies'],
          message: '启用智能调度前请至少配置一个分组策略',
        })
      }
      if (values.probeResponseMinDelayMs > values.probeResponseMaxDelayMs) {
        context.addIssue({
          code: 'custom',
          path: ['probeResponseMinDelayMs'],
          message: '探针最小延迟不能大于最大延迟',
        })
      }
      const configuredGroups = new Set<string>()
      for (const [
        index,
        policy,
      ] of values.smartScheduleGroupPolicies.entries()) {
        if (configuredGroups.has(policy.group)) {
          context.addIssue({
            code: 'custom',
            path: ['smartScheduleGroupPolicies', index, 'group'],
            message: '同一分组不能配置多个调度策略',
          })
        } else {
          configuredGroups.add(policy.group)
        }
      }
    })
}

export function createChannelGroupsSchema() {
  return z.object({
    groups: z
      .array(
        z
          .string()
          .trim()
          .min(1, '分组名称不能为空')
          .max(64, '单个分组名称不能超过 64 个字符')
      )
      .min(1, '请至少选择一个关联分组')
      .refine(
        (groups) => groups.join(',').length <= 64,
        '关联分组名称合计不能超过 64 个字符'
      ),
  })
}

export function createGroupRatioSyncSchema(highestCostRatio: number | null) {
  return z
    .object({
      coefficient: z.coerce
        .number()
        .finite('系数必须是有效数字')
        .min(0, '系数不能小于 0')
        .max(MAX_MONITOR_RATIO, '系数不能超过 1000000'),
    })
    .superRefine((values, context) => {
      if (highestCostRatio == null) return
      if (highestCostRatio * values.coefficient > MAX_MONITOR_RATIO) {
        context.addIssue({
          code: 'custom',
          path: ['coefficient'],
          message: '成本倍率乘以系数后的结果不能超过 1000000',
        })
      }
    })
}

type SavedUpstreamCredential = {
  type: ChannelMonitorUpstreamType
  baseUrl: string
  authType: ChannelMonitorUpstreamAuthType
  hasAccessToken: boolean
  account: string
  hasPassword: boolean
} | null

const customKeyValueSchema = z.object({
  key: z.string().trim().max(256, '名称不能超过 256 个字符'),
  value: z.string().max(8192, '值不能超过 8192 个字符'),
  secret: z.boolean(),
  hasValue: z.boolean(),
})

const customRequestSchema = z.object({
  method: z.enum(['GET', 'POST']),
  path: z.string().trim().max(2048, '接口路径不能超过 2048 个字符'),
  query: z
    .array(customKeyValueSchema)
    .max(MAX_CUSTOM_UPSTREAM_ENTRIES, '查询参数不能超过 32 项'),
  headers: z
    .array(customKeyValueSchema)
    .max(MAX_CUSTOM_UPSTREAM_ENTRIES, '请求头不能超过 32 项'),
  bodyType: z.enum(['none', 'json', 'form']),
  body: z
    .string()
    .max(MAX_CUSTOM_UPSTREAM_BODY_BYTES, 'JSON 请求体不能超过 49152 字节'),
  bodySecret: z.boolean(),
  hasBody: z.boolean(),
  form: z
    .array(customKeyValueSchema)
    .max(MAX_CUSTOM_UPSTREAM_ENTRIES, '表单参数不能超过 32 项'),
})

const customResultSchema = z.object({
  responseType: z.enum(['json', 'text']),
  valuePath: z.string().trim().max(512, 'JSON 取值路径不能超过 512 个字符'),
  multiplier: z.coerce
    .number()
    .finite('结果乘数必须是有效数字')
    .min(0, '结果乘数不能小于 0')
    .max(MAX_MONITOR_RATIO, '结果乘数不能超过 1000000'),
})

const customMetricSchema = z.object({
  source: z.enum(['fixed', 'http']),
  fixedValue: z.coerce.number().finite('固定值必须是有效数字'),
  request: customRequestSchema,
  result: customResultSchema,
})

const customUpstreamConfigSchema = z.object({
  version: z.literal(1),
  ratio: customMetricSchema,
  balance: customMetricSchema,
  balanceReuseRatioRequest: z.boolean(),
})

type CustomMetricFormValue = z.infer<typeof customMetricSchema>

function validateCustomEntries(
  entries: z.infer<typeof customKeyValueSchema>[],
  path: (string | number)[],
  label: string,
  context: z.RefinementCtx
) {
  const keys = new Set<string>()
  for (const [index, entry] of entries.entries()) {
    const key = entry.key.trim()
    if (!key) {
      context.addIssue({
        code: 'custom',
        path: [...path, index, 'key'],
        message: `${label}名称不能为空`,
      })
      continue
    }
    const normalizedKey = key.toLowerCase()
    if (keys.has(normalizedKey)) {
      context.addIssue({
        code: 'custom',
        path: [...path, index, 'key'],
        message: `${label}名称不能重复`,
      })
    }
    keys.add(normalizedKey)
    if (entry.secret && !entry.value && !entry.hasValue) {
      context.addIssue({
        code: 'custom',
        path: [...path, index, 'value'],
        message: `敏感${label}的值不能为空`,
      })
    }
  }
}

function validateCustomMetric(
  metric: CustomMetricFormValue,
  metricName: 'ratio' | 'balance',
  reuseRequest: boolean,
  context: z.RefinementCtx
) {
  const pathPrefix = ['customConfig', metricName]
  if (metric.source === 'fixed') {
    if (metricName === 'ratio') {
      if (metric.fixedValue < 0 || metric.fixedValue > MAX_MONITOR_RATIO) {
        context.addIssue({
          code: 'custom',
          path: [...pathPrefix, 'fixedValue'],
          message: '固定倍率必须在 0 到 1000000 之间',
        })
      }
    } else if (Math.abs(metric.fixedValue) > MAX_CUSTOM_UPSTREAM_BALANCE) {
      context.addIssue({
        code: 'custom',
        path: [...pathPrefix, 'fixedValue'],
        message: '固定余额绝对值不能超过 1000000000000000',
      })
    }
    return
  }

  if (!reuseRequest) {
    if (!metric.request.path.trim()) {
      context.addIssue({
        code: 'custom',
        path: [...pathPrefix, 'request', 'path'],
        message: '请输入接口路径',
      })
    }
    let decodedPath = metric.request.path
    try {
      decodedPath = decodeURIComponent(metric.request.path)
    } catch {
      context.addIssue({
        code: 'custom',
        path: [...pathPrefix, 'request', 'path'],
        message: '接口路径格式无效',
      })
    }
    if (
      decodedPath.includes('?') ||
      decodedPath.includes('#') ||
      /^https?:\/\//i.test(metric.request.path)
    ) {
      context.addIssue({
        code: 'custom',
        path: [...pathPrefix, 'request', 'path'],
        message: '接口路径请填写不含查询参数的相对路径',
      })
    }
    if (metric.request.method === 'GET' && metric.request.bodyType !== 'none') {
      context.addIssue({
        code: 'custom',
        path: [...pathPrefix, 'request', 'bodyType'],
        message: 'GET 请求不能配置请求体',
      })
    }
    validateCustomEntries(
      metric.request.query,
      [...pathPrefix, 'request', 'query'],
      '查询参数',
      context
    )
    validateCustomEntries(
      metric.request.headers,
      [...pathPrefix, 'request', 'headers'],
      '请求头',
      context
    )
    if (metric.request.bodyType === 'json') {
      const preservesSavedBody =
        metric.request.bodySecret && metric.request.hasBody
      if (!metric.request.body && !preservesSavedBody) {
        context.addIssue({
          code: 'custom',
          path: [...pathPrefix, 'request', 'body'],
          message: 'JSON 请求体不能为空',
        })
      } else if (metric.request.body) {
        try {
          JSON.parse(metric.request.body)
        } catch {
          context.addIssue({
            code: 'custom',
            path: [...pathPrefix, 'request', 'body'],
            message: 'JSON 请求体格式无效',
          })
        }
      }
    }
    if (metric.request.bodyType === 'form') {
      validateCustomEntries(
        metric.request.form,
        [...pathPrefix, 'request', 'form'],
        '表单参数',
        context
      )
    }
  }

  if (
    metric.result.responseType === 'json' &&
    !metric.result.valuePath.trim()
  ) {
    context.addIssue({
      code: 'custom',
      path: [...pathPrefix, 'result', 'valuePath'],
      message: '请输入 JSON 取值路径',
    })
  }
  if (metric.result.multiplier <= 0) {
    context.addIssue({
      code: 'custom',
      path: [...pathPrefix, 'result', 'multiplier'],
      message: '结果乘数必须大于 0',
    })
  }
}

export function createUpstreamConfigSchema(
  savedCredential: SavedUpstreamCredential
) {
  return z
    .object({
      upstreamType: z.enum(['new_api', 'sub2api', 'custom']),
      baseUrl: z
        .string()
        .trim()
        .min(1, '请输入上游地址')
        .max(2048, '上游地址过长')
        .url({ error: '请输入有效的上游地址' }),
      group: z.string().trim().max(64, '上游分组不能超过 64 个字符'),
      authType: z.enum([
        'public',
        'user',
        'api_key',
        'account',
        'token',
        'custom',
      ]),
      userId: z.coerce.number().int().min(0, '上游用户 ID 必须大于 0'),
      accessToken: z.string().trim().max(4096, '访问令牌过长'),
      account: z
        .string()
        .trim()
        .max(320, 'Sub2API 登录邮箱过长')
        .email('请输入有效的 Sub2API 登录邮箱')
        .or(z.literal('')),
      password: z.string().max(4096, 'Sub2API 登录密码过长'),
      singleChannelAction: z.enum(channelMonitorPolicyActions),
      multipleChannelsAction: z.enum(channelMonitorPolicyActions),
      ratioSyncEnabled: z.boolean(),
      balanceSyncEnabled: z.boolean(),
      balanceWarningThreshold: z
        .number()
        .finite('余额预警值必须是有效数字')
        .min(0, '余额预警值不能小于 0')
        .max(MAX_BALANCE_THRESHOLD, '余额预警值不能超过 1000000000000')
        .nullable(),
      balanceAutoDisableThreshold: z
        .number()
        .finite('余额自动禁用阈值必须是有效数字')
        .min(0, '余额自动禁用阈值不能小于 0')
        .max(MAX_BALANCE_THRESHOLD, '余额自动禁用阈值不能超过 1000000000000')
        .nullable(),
      costConversionMode: z.enum(['none', 'recharge', 'subscription']),
      rechargePaidCny: z.coerce
        .number()
        .finite('实付人民币金额必须是有效数字')
        .min(0, '实付人民币金额不能小于 0')
        .max(
          MAX_COST_CONVERSION_AMOUNT,
          '实付人民币金额不能超过 1000000000000'
        ),
      rechargeCreditedUsd: z.coerce
        .number()
        .finite('到账美元额度必须是有效数字')
        .min(0, '到账美元额度不能小于 0')
        .max(MAX_COST_CONVERSION_AMOUNT, '到账美元额度不能超过 1000000000000'),
      subscriptionPeriod: z.enum(['day', 'week', 'month']),
      subscriptionPriceCny: z.coerce
        .number()
        .finite('订阅价格必须是有效数字')
        .min(0, '订阅价格不能小于 0')
        .max(MAX_COST_CONVERSION_AMOUNT, '订阅价格不能超过 1000000000000'),
      subscriptionDailyUsd: z.coerce
        .number()
        .finite('每日美元额度必须是有效数字')
        .min(0, '每日美元额度不能小于 0')
        .max(MAX_COST_CONVERSION_AMOUNT, '每日美元额度不能超过 1000000000000'),
      customConfig: customUpstreamConfigSchema,
    })
    .superRefine((values, context) => {
      if (values.costConversionMode === 'recharge') {
        if (values.rechargePaidCny <= 0) {
          context.addIssue({
            code: 'custom',
            path: ['rechargePaidCny'],
            message: '实付人民币金额必须大于 0',
          })
        }
        if (values.rechargeCreditedUsd <= 0) {
          context.addIssue({
            code: 'custom',
            path: ['rechargeCreditedUsd'],
            message: '到账美元额度必须大于 0',
          })
        }
        const factor = values.rechargePaidCny / values.rechargeCreditedUsd
        if (Number.isFinite(factor) && factor > MAX_MONITOR_RATIO) {
          context.addIssue({
            code: 'custom',
            path: ['rechargePaidCny'],
            message: '倍率换算系数不能超过 1000000',
          })
        }
      }
      if (values.costConversionMode === 'subscription') {
        if (values.subscriptionPriceCny <= 0) {
          context.addIssue({
            code: 'custom',
            path: ['subscriptionPriceCny'],
            message: '订阅价格必须大于 0',
          })
        }
        if (values.subscriptionDailyUsd <= 0) {
          context.addIssue({
            code: 'custom',
            path: ['subscriptionDailyUsd'],
            message: '每日美元额度必须大于 0',
          })
        }
        const factor =
          values.subscriptionPriceCny /
          (values.subscriptionDailyUsd *
            CHANNEL_MONITOR_SUBSCRIPTION_DAYS[values.subscriptionPeriod])
        if (Number.isFinite(factor) && factor > MAX_MONITOR_RATIO) {
          context.addIssue({
            code: 'custom',
            path: ['subscriptionPriceCny'],
            message: '倍率换算系数不能超过 1000000',
          })
        }
      }
      if (values.upstreamType === 'custom') {
        if (values.authType !== 'custom') {
          context.addIssue({
            code: 'custom',
            path: ['authType'],
            message: '自定义上游认证方式无效',
          })
        }
        if (
          values.customConfig.balanceReuseRatioRequest &&
          (values.customConfig.ratio.source !== 'http' ||
            values.customConfig.balance.source !== 'http')
        ) {
          context.addIssue({
            code: 'custom',
            path: ['customConfig', 'balanceReuseRatioRequest'],
            message: '只有倍率和余额都使用接口查询时才能复用倍率接口',
          })
        }
        validateCustomMetric(values.customConfig.ratio, 'ratio', false, context)
        validateCustomMetric(
          values.customConfig.balance,
          'balance',
          values.customConfig.balanceReuseRatioRequest,
          context
        )
        return
      }
      const hasSavedCredential =
        savedCredential?.type === values.upstreamType &&
        savedCredential.authType === values.authType
      const hasSavedAccessToken =
        hasSavedCredential && savedCredential?.hasAccessToken === true
      if (values.upstreamType === 'new_api') {
        if (values.authType !== 'public' && values.authType !== 'user') {
          context.addIssue({
            code: 'custom',
            path: ['authType'],
            message: '请选择 New API 认证方式',
          })
          return
        }
        if (values.authType === 'public') return
        if (values.userId <= 0) {
          context.addIssue({
            code: 'custom',
            path: ['userId'],
            message: '上游用户 ID 必须大于 0',
          })
        }
        if (!values.accessToken && !hasSavedAccessToken) {
          context.addIssue({
            code: 'custom',
            path: ['accessToken'],
            message: '请输入上游访问令牌',
          })
        }
        return
      }

      if (values.authType === 'api_key') return
      if (values.authType === 'account') {
        if (!values.account) {
          context.addIssue({
            code: 'custom',
            path: ['account'],
            message: '请输入 Sub2API 登录邮箱',
          })
        }
        const hasSavedPassword =
          hasSavedCredential &&
          savedCredential?.hasPassword === true &&
          savedCredential.baseUrl === values.baseUrl &&
          savedCredential.account === values.account
        if (!values.password && !hasSavedPassword) {
          context.addIssue({
            code: 'custom',
            path: ['password'],
            message: '请输入 Sub2API 登录密码',
          })
        }
        return
      }
      if (values.authType !== 'token') {
        context.addIssue({
          code: 'custom',
          path: ['authType'],
          message: '请选择 Sub2API 认证方式',
        })
        return
      }
      if (!values.accessToken && !hasSavedAccessToken) {
        context.addIssue({
          code: 'custom',
          path: ['accessToken'],
          message: '请输入 Sub2API Token（旧版访问令牌）',
        })
      }
    })
}

export type ChannelRatioFormValues = z.infer<
  ReturnType<typeof createChannelRatioSchema>
>

export type GroupRatioFormValues = z.infer<
  ReturnType<typeof createGroupRatioSchema>
>

export type ChannelConcurrencyLimitFormValues = z.infer<
  ReturnType<typeof createChannelConcurrencyLimitSchema>
>

export type ChannelMonitorSettingsFormValues = z.infer<
  ReturnType<typeof createChannelMonitorSettingsSchema>
>

export type ChannelMonitorSmartSchedulePolicyFormValues = z.infer<
  ReturnType<typeof createChannelMonitorSmartSchedulePolicySchema>
>

export type ChannelMonitorSmartScheduleGroupPolicyFormValues =
  ChannelMonitorSettingsFormValues['smartScheduleGroupPolicies'][number]

export type ChannelGroupsFormValues = z.infer<
  ReturnType<typeof createChannelGroupsSchema>
>

export type GroupRatioSyncFormValues = z.infer<
  ReturnType<typeof createGroupRatioSyncSchema>
>

export type UpstreamConfigFormValues = z.infer<
  ReturnType<typeof createUpstreamConfigSchema>
>
