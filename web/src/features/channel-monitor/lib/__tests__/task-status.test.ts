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

import type { ChannelMonitorTask } from '../../types'
import { getLatestCompletedChannelMonitorTaskTime } from '../task-status'

function createTask(
  status: ChannelMonitorTask['status'],
  updatedAt: number
): ChannelMonitorTask {
  return {
    id: updatedAt,
    task_id: `task-${updatedAt}`,
    type: 'channel_ratio_monitor',
    status,
    state: null,
    result: null,
    error: '',
    created_at: updatedAt - 1,
    updated_at: updatedAt,
  }
}

describe('channel monitor task completion refresh', () => {
  test('uses the latest completed ratio task and ignores active tasks', () => {
    const tasks = [
      createTask('succeeded', 100),
      createTask('failed', 200),
      createTask('running', 300),
      createTask('pending', 400),
    ]

    assert.equal(getLatestCompletedChannelMonitorTaskTime(tasks), 200)
  })

  test('does not request an overview refresh when every task is active', () => {
    const tasks = [createTask('running', 100), createTask('pending', 200)]

    assert.equal(getLatestCompletedChannelMonitorTaskTime(tasks), 0)
  })
})
