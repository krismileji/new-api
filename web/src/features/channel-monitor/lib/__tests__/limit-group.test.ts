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
import { describe, expect, test } from 'vitest'

import { channelLimitGroupSchema, emptyChannelLimitGroup } from '../limit-group'

describe('共享限流配置校验', () => {
  test('接受同级共享预留和单维度不限额', () => {
    const group = emptyChannelLimitGroup()
    group.name = '上游 A'
    group.members = [
      { channel_id: 1, priority: 100 },
      { channel_id: 2, priority: 100 },
    ]
    group.concurrency_limit = 10
    group.tiers[0].reserved_concurrency = 3
    expect(channelLimitGroupSchema.safeParse(group).success).toBe(true)
  })
  test.each([
    [
      '预留超过上限',
      {
        concurrency_limit: 2,
        tiers: [{ priority: 100, reserved_concurrency: 3, reserved_rpm: 0 }],
      },
    ],
    [
      '重复成员',
      {
        members: [
          { channel_id: 1, priority: 100 },
          { channel_id: 1, priority: 100 },
        ],
      },
    ],
    ['未知等级', { members: [{ channel_id: 1, priority: 50 }] }],
    [
      '不限 RPM 时预留',
      { tiers: [{ priority: 100, reserved_concurrency: 0, reserved_rpm: 1 }] },
    ],
    ['非整数并发', { concurrency_limit: 1.5 }],
  ])('拒绝%s', (_name, patch) => {
    const group = {
      ...emptyChannelLimitGroup(),
      name: '上游 A',
      members: [{ channel_id: 1, priority: 100 }],
      ...patch,
    }
    expect(channelLimitGroupSchema.safeParse(group).success).toBe(false)
  })
})
