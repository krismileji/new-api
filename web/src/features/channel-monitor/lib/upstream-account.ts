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

export const upstreamAccountSchema = z
  .object({
    id: z.number().int().nonnegative(),
    revision: z.number().int().nonnegative(),
    name: z
      .string()
      .trim()
      .min(1, '请输入账户名称')
      .max(80, '账户名称最多 80 个字符'),
    source_channel_id: z.number().int().nonnegative(),
    channel_ids: z
      .array(z.number().int().positive())
      .max(100, '最多关联 100 个渠道'),
    channel_revisions: z.record(z.string(), z.number()),
    refresh_interval_minutes: z
      .number()
      .int('请输入整数分钟')
      .min(0)
      .max(10080, '刷新间隔最多 10080 分钟'),
    proxy: z.string().max(2048).optional(),
    balance_key: z.string().max(4096).optional(),
  })
  .superRefine((input, context) => {
    if (
      !input.id &&
      (!input.source_channel_id ||
        !input.channel_ids.includes(input.source_channel_id))
    ) {
      context.addIssue({
        code: 'custom',
        path: ['source_channel_id'],
        message: '请选择并保留配置来源渠道',
      })
    }
  })

export type UpstreamAccountFormValues = z.infer<typeof upstreamAccountSchema>
