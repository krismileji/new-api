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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import type { ChannelMonitorSettings } from '../../types'
import { normalizeChannelMonitorSettings } from '../settings-normalization'

const fallback = {
  smart_schedule_enabled: false,
  smart_schedule_group_policies: [],
} as unknown as ChannelMonitorSettings

describe('渠道监控设置规范化', () => {
  test('旧服务返回空策略时使用空数组，避免智能调度页面崩溃', () => {
    const settings = {
      ...fallback,
      smart_schedule_enabled: true,
      smart_schedule_group_policies: null,
    } as unknown as ChannelMonitorSettings

    const normalized = normalizeChannelMonitorSettings(settings, fallback)

    assert.deepEqual(normalized.smart_schedule_group_policies, [])
  })

  test('保留服务端返回的有效策略列表', () => {
    const policies = [
      { group: 'default' },
    ] as ChannelMonitorSettings['smart_schedule_group_policies']
    const settings = {
      ...fallback,
      smart_schedule_group_policies: policies,
    }

    const normalized = normalizeChannelMonitorSettings(settings, fallback)

    assert.equal(normalized.smart_schedule_group_policies, policies)
  })
})
