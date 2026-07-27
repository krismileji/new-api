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
import { describe, test } from 'node:test'

import { getChannelMonitorSmartSchedulePoolStatus } from '../smart-schedule-summary'

const normalPool = {
  routeCount: 3,
  participatingCount: 3,
  activeCount: 3,
  degradedCount: 0,
  probingCount: 0,
}

describe('smart schedule pool status', () => {
  test('distinguishes participation configuration from current availability', () => {
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        participatingCount: 0,
        activeCount: 0,
      }),
      '未参与调度'
    )
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        activeCount: 0,
      }),
      '当前不可调度'
    )
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        activeCount: 2,
      }),
      '部分可调度'
    )
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        participatingCount: 2,
        activeCount: 2,
      }),
      '部分参与'
    )
  })

  test('prioritizes protection states over participation and availability', () => {
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        participatingCount: 0,
        activeCount: 0,
        degradedCount: 1,
      }),
      '低成功率'
    )
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        activeCount: 0,
        probingCount: 1,
      }),
      '稳定性试放'
    )
  })
})
