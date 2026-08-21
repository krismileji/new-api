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

export const CHANNEL_STATUS_PROBE_DEFAULT_INTERVAL_SECONDS = 60

export const CHANNEL_STATUS_PROBE_DISPLAY_LIMITS = {
  minute: 60,
  hour: 24,
  day: 30,
} as const

export const channelStatusProbeConfigSchema = z
  .object({
    enabled: z.boolean(),
    models: z
      .array(
        z
          .string()
          .trim()
          .min(1, '模型名称不能为空')
          .max(255, '模型名称不能超过 255 个字符')
          .refine((value) => !value.includes('*'), '必须填写具体模型名称')
      )
      .min(1, '至少选择一个探测模型')
      .max(20, '最多选择 20 个探测模型'),
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
    recordSample: z.boolean(),
  })
  .superRefine((value, context) => {
    if (
      value.displayValue >
      CHANNEL_STATUS_PROBE_DISPLAY_LIMITS[value.displayUnit]
    ) {
      context.addIssue({
        code: 'custom',
        path: ['displayValue'],
        message: '分钟最多 60、小时最多 24、天最多 30',
      })
    }
    if (value.recordSample && value.intervalSeconds < 60) {
      context.addIssue({
        code: 'custom',
        path: ['intervalSeconds'],
        message: '计入智能调度样本时，探测间隔不能小于 60 秒',
      })
    }
  })

export type ChannelStatusProbeConfigFormValues = z.infer<
  typeof channelStatusProbeConfigSchema
>
