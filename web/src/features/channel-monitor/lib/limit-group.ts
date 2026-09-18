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

import type { ChannelLimitGroup } from '../api-limit-groups'

const amount = z
  .number()
  .int('请输入整数')
  .min(0, '不能小于 0')
  .max(100000, '不能超过 100000')
export const channelLimitGroupSchema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, '请输入名称')
      .max(128, '名称不能超过 128 个字符'),
    concurrency_limit: amount,
    rpm_limit: amount,
    tiers: z
      .array(
        z.object({
          priority: z.number().int().min(0).max(1000),
          reserved_concurrency: amount,
          reserved_rpm: amount,
        })
      )
      .min(1, '至少保留一个等级')
      .max(8, '最多 8 个等级'),
    members: z
      .array(
        z.object({
          channel_id: z.number().int().positive(),
          priority: z.number().int().min(0).max(1000),
        })
      )
      .min(1, '请选择成员渠道')
      .max(64, '最多 64 个成员'),
  })
  .superRefine((value, ctx) => {
    const priorities = new Set(value.tiers.map((tier) => tier.priority))
    if (priorities.size !== value.tiers.length) {
      ctx.addIssue({
        code: 'custom',
        message: '资源优先级不能重复',
        path: ['tiers'],
      })
    }
    if (
      value.tiers.reduce((sum, tier) => sum + tier.reserved_concurrency, 0) >
      value.concurrency_limit
    ) {
      ctx.addIssue({
        code: 'custom',
        message: '并发预留合计不能超过组上限',
        path: ['tiers'],
      })
    }
    if (
      value.tiers.reduce((sum, tier) => sum + tier.reserved_rpm, 0) >
      value.rpm_limit
    ) {
      ctx.addIssue({
        code: 'custom',
        message: 'RPM 预留合计不能超过组上限',
        path: ['tiers'],
      })
    }
    if (value.members.some((member) => !priorities.has(member.priority))) {
      ctx.addIssue({
        code: 'custom',
        message: '成员优先级必须对应已配置等级',
        path: ['members'],
      })
    }
    if (
      new Set(value.members.map((member) => member.channel_id)).size !==
      value.members.length
    ) {
      ctx.addIssue({
        code: 'custom',
        message: '成员渠道不能重复',
        path: ['members'],
      })
    }
  })
export type ChannelLimitForm = z.infer<typeof channelLimitGroupSchema>
export function emptyChannelLimitGroup(): ChannelLimitGroup {
  return {
    id: 0,
    name: '',
    revision: 0,
    enabled: true,
    updated_at: 0,
    concurrency_limit: 0,
    rpm_limit: 0,
    tiers: [
      { priority: 100, reserved_concurrency: 0, reserved_rpm: 0 },
      { priority: 0, reserved_concurrency: 0, reserved_rpm: 0 },
    ],
    members: [],
  }
}
