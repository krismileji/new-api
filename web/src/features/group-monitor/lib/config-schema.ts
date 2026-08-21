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

export const CHANNEL_GROUP_MONITOR_DEFAULT_INTERVAL_SECONDS = 60

export const CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS = {
  minute: 60,
  hour: 24,
  day: 30,
} as const

function channelGroupMonitorDisplaySeconds(
  value: number,
  unit: 'minute' | 'hour' | 'day'
): number {
  if (unit === 'minute') return value * 60
  if (unit === 'hour') return value * 3600
  return value * 86400
}

export const channelGroupMonitorConfigSchema = z
  .object({
    enabled: z.boolean(),
    groups: z
      .array(
        z.object({
          groupName: z
            .string()
            .trim()
            .min(1, '请选择监控分组')
            .max(64, '分组名称不能超过 64 个字符'),
          probeModel: z
            .string()
            .trim()
            .min(1, '请选择探测模型')
            .max(255, '探测模型不能超过 255 个字符')
            .refine((value) => !value.includes('*'), '必须选择具体模型'),
          displayInitial: z
            .string()
            .trim()
            .refine(
              (value) => [...value].length <= 1,
              '分组展示字只能配置一个字符'
            )
            .default(''),
        })
      )
      .max(100, '最多配置 100 个监控分组'),
    intervalSeconds: z
      .number()
      .int('探测间隔必须是整数')
      .min(30, '探测间隔不能小于 30 秒')
      .max(86400, '探测间隔不能大于 86400 秒'),
    displayValue: z
      .number()
      .int('状态展示数值必须是整数')
      .min(1, '状态展示数值不能小于 1'),
    displayUnit: z.enum(['minute', 'hour', 'day']),
    revision: z.number().int().min(0),
  })
  .superRefine((value, context) => {
    if (
      value.displayValue >
      CHANNEL_GROUP_MONITOR_DISPLAY_LIMITS[value.displayUnit]
    ) {
      context.addIssue({
        code: 'custom',
        path: ['displayValue'],
        message: '分钟最多 60、小时最多 24、天最多 30',
      })
    }

    const displaySeconds = channelGroupMonitorDisplaySeconds(
      value.displayValue,
      value.displayUnit
    )
    if (displaySeconds < value.intervalSeconds * 2) {
      context.addIssue({
        code: 'custom',
        path: ['displayValue'],
        message: '状态展示范围至少需要覆盖两个探测周期',
      })
    }

    const groups = new Set<string>()
    for (const [index, group] of value.groups.entries()) {
      if (groups.has(group.groupName)) {
        context.addIssue({
          code: 'custom',
          path: ['groups', index, 'groupName'],
          message: '同一个监控分组只能配置一次',
        })
      }
      groups.add(group.groupName)
    }
  })

export type ChannelGroupMonitorConfigFormValues = z.infer<
  typeof channelGroupMonitorConfigSchema
>
