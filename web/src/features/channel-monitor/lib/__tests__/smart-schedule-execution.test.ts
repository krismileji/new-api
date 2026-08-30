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
import { afterEach, describe, expect, test } from 'vitest'

import type {
  ChannelMonitorTask,
  ChannelMonitorTaskAdjustment,
} from '../../types'
import {
  loadChannelMonitorSmartScheduleExecutionSelection,
  orderChannelMonitorSmartScheduleAdjustmentsByRoutingPolicy,
  orderChannelMonitorSmartScheduleModels,
  orderChannelMonitorTasksByExecutionTime,
  saveChannelMonitorSmartScheduleExecutionSelection,
} from '../smart-schedule-execution'

const STORAGE_KEY = 'channel-monitor:smart-schedule-display:v1'

function createTask(
  id: number,
  taskId: string,
  createdAt: number
): ChannelMonitorTask {
  return {
    id,
    task_id: taskId,
    type: 'channel_smart_schedule',
    status: 'succeeded',
    state: null,
    result: null,
    error: '',
    created_at: createdAt,
    updated_at: createdAt,
  }
}

function createAdjustment(
  channelId: number,
  newPriority: number,
  newWeight: number,
  overrides: Partial<ChannelMonitorTaskAdjustment> = {}
): ChannelMonitorTaskAdjustment {
  return {
    channel_id: channelId,
    channel_name: `渠道 ${channelId}`,
    group: 'vip',
    model: 'model-a',
    action: 'updated',
    old_priority: 0,
    new_priority: newPriority,
    old_weight: 0,
    new_weight: newWeight,
    reason: '',
    ...overrides,
  }
}

afterEach(() => {
  localStorage.clear()
})

describe('智能调度执行记录排序与偏好', () => {
  test('执行批次按执行时间倒序并以 ID 处理同一秒记录', () => {
    const ordered = orderChannelMonitorTasksByExecutionTime([
      createTask(1, 'oldest', 100),
      createTask(2, 'newer-low-id', 200),
      createTask(3, 'newer-high-id', 200),
    ])

    expect(ordered.map((task) => task.task_id)).toEqual([
      'newer-high-id',
      'newer-low-id',
      'oldest',
    ])
  })

  test('渠道按路由优先级层和权重倒序且失败记录沿用旧路由值', () => {
    const ordered = orderChannelMonitorSmartScheduleAdjustmentsByRoutingPolicy([
      createAdjustment(1, 90, 100),
      createAdjustment(2, 100, 20),
      createAdjustment(3, 100, 80),
      createAdjustment(4, 120, 100, {
        action: 'failed',
        old_priority: 100,
        old_weight: 50,
      }),
    ])

    expect(ordered.map((adjustment) => adjustment.channel_id)).toEqual([
      3, 4, 2, 1,
    ])
  })

  test('模型仅从历史分组中选择并按当前配置顺序排列', () => {
    expect(
      orderChannelMonitorSmartScheduleModels(
        ['legacy-b', 'shared-model', 'legacy-a'],
        ['current-only', 'shared-model']
      )
    ).toEqual(['shared-model', 'legacy-a', 'legacy-b'])
  })

  test('分组和模型偏好按版本化结构保存并恢复', () => {
    saveChannelMonitorSmartScheduleExecutionSelection({
      group: 'vip',
      model: 'model-b',
    })

    expect(localStorage.getItem(STORAGE_KEY)).toBe(
      JSON.stringify({ group: 'vip', model: 'model-b' })
    )
    expect(loadChannelMonitorSmartScheduleExecutionSelection()).toEqual({
      group: 'vip',
      model: 'model-b',
    })
  })

  test('损坏的浏览器偏好回退为空选择', () => {
    localStorage.setItem(STORAGE_KEY, '{bad json')

    expect(loadChannelMonitorSmartScheduleExecutionSelection()).toEqual({
      group: '',
      model: '',
    })
  })
})
