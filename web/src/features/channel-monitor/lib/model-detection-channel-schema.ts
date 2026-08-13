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
import { z } from 'zod'

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionChannelConfigResult,
  ChannelModelDetectionClaimedModel,
  ChannelModelDetectionConfigUpdateRequest,
  ChannelModelDetectionEstimateRequest,
  ChannelModelDetectionPreset,
} from '../types-model-detection'

export const CHANNEL_MODEL_DETECTION_CLAIMED_MODELS = [
  'gpt-5.6-sol',
  'gpt-5.6-terra',
  'gpt-5.6-luna',
] as const satisfies readonly ChannelModelDetectionClaimedModel[]

const targetSchema = z.object({
  targetKey: z.string().trim(),
  requestModel: z.string().trim().min(1, '请选择请求模型'),
  claimedModel: z.enum(CHANNEL_MODEL_DETECTION_CLAIMED_MODELS),
})

const channelConfigSchema = z.object({
  scheduleEnabled: z.boolean(),
  targets: z
    .array(targetSchema)
    .min(1, '至少配置 1 个检测目标')
    .max(10, '最多配置 10 个检测目标'),
  revision: z.number().int().nonnegative(),
})

export type ChannelModelDetectionConfigFormValues = z.infer<
  typeof channelConfigSchema
>

export function channelModelDetectionExactModels(models: string[]) {
  const unique = new Set<string>()
  for (const model of models) {
    const normalized = model.trim()
    if (!normalized || normalized.includes('*')) continue
    unique.add(normalized)
  }
  return [...unique]
}

export function createChannelModelDetectionConfigSchema(
  supportedModels: string[],
  detectorURLConfigured: boolean
) {
  const exactModels = new Set(channelModelDetectionExactModels(supportedModels))
  return channelConfigSchema.superRefine((values, context) => {
    if (values.scheduleEnabled && !detectorURLConfigured) {
      context.addIssue({
        code: 'custom',
        path: ['scheduleEnabled'],
        message: '配置检测器地址后才能参加统一定时检测',
      })
    }

    const targetKeys = new Set<string>()
    const targetPairs = new Set<string>()
    values.targets.forEach((target, index) => {
      if (!exactModels.has(target.requestModel)) {
        context.addIssue({
          code: 'custom',
          path: ['targets', index, 'requestModel'],
          message: '请选择该渠道支持的精确模型',
        })
      }

      if (target.targetKey) {
        if (targetKeys.has(target.targetKey)) {
          context.addIssue({
            code: 'custom',
            path: ['targets', index, 'targetKey'],
            message: '目标标识不能重复',
          })
        }
        targetKeys.add(target.targetKey)
      }

      const pair = `${target.requestModel}\u0000${target.claimedModel}`
      if (targetPairs.has(pair)) {
        context.addIssue({
          code: 'custom',
          path: ['targets', index, 'claimedModel'],
          message: '请求模型和申报型号组合不能重复',
        })
      }
      targetPairs.add(pair)
    })
  })
}

function defaultClaimedModel(
  requestModel: string
): ChannelModelDetectionClaimedModel {
  if (
    CHANNEL_MODEL_DETECTION_CLAIMED_MODELS.includes(
      requestModel as ChannelModelDetectionClaimedModel
    )
  ) {
    return requestModel as ChannelModelDetectionClaimedModel
  }
  return 'gpt-5.6-sol'
}

export function channelModelDetectionChannelToConfigFormValues(
  channel: ChannelModelDetectionChannel
): ChannelModelDetectionConfigFormValues {
  const exactModels = channelModelDetectionExactModels(channel.supported_models)
  const requestModel = exactModels[0] ?? ''
  const targets = [...channel.targets]
    .filter((target) => target.enabled)
    .sort((left, right) => left.position - right.position)
    .map((target) => ({
      targetKey: target.target_key,
      requestModel: target.request_model,
      claimedModel: target.claimed_model,
    }))

  return {
    scheduleEnabled: channel.config?.schedule_enabled ?? false,
    targets:
      targets.length > 0
        ? targets
        : [
            {
              targetKey: '',
              requestModel,
              claimedModel: defaultClaimedModel(requestModel),
            },
          ],
    revision: channel.config?.revision ?? 0,
  }
}

export function channelModelDetectionConfigResultToFormValues(
  result: ChannelModelDetectionChannelConfigResult
): ChannelModelDetectionConfigFormValues {
  return {
    scheduleEnabled: result.schedule_enabled,
    targets: [...result.targets]
      .filter((target) => target.enabled)
      .sort((left, right) => left.position - right.position)
      .map((target) => ({
        targetKey: target.target_key,
        requestModel: target.request_model,
        claimedModel: target.claimed_model,
      })),
    revision: result.revision,
  }
}

export function createChannelModelDetectionConfigUpdateRequest(
  values: ChannelModelDetectionConfigFormValues
): ChannelModelDetectionConfigUpdateRequest {
  return {
    schedule_enabled: values.scheduleEnabled,
    targets: values.targets.map((target) => ({
      target_key: target.targetKey.trim(),
      request_model: target.requestModel.trim(),
      claimed_model: target.claimedModel,
    })),
    revision: values.revision,
  }
}

const manualEstimateSchema = z
  .object({
    preset: z.union([
      z.literal(''),
      z.enum(['low', 'medium', 'high'] as const),
    ]),
    confirmHighCost: z.boolean(),
  })
  .superRefine((values, context) => {
    if (!values.preset) {
      context.addIssue({
        code: 'custom',
        path: ['preset'],
        message: '请选择本次手动检测档位',
      })
    }
    if (values.preset === 'high' && !values.confirmHighCost) {
      context.addIssue({
        code: 'custom',
        path: ['confirmHighCost'],
        message: '使用高档手动检测前必须确认成本风险',
      })
    }
  })

export type ChannelModelDetectionManualEstimateFormValues = z.infer<
  typeof manualEstimateSchema
>

export const CHANNEL_MODEL_DETECTION_MANUAL_ESTIMATE_EMPTY_VALUES: ChannelModelDetectionManualEstimateFormValues =
  {
    preset: '',
    confirmHighCost: false,
  }

export { manualEstimateSchema as channelModelDetectionManualEstimateSchema }

export function createChannelModelDetectionEstimateRequest(
  preset: ChannelModelDetectionPreset
): ChannelModelDetectionEstimateRequest {
  return { preset }
}
