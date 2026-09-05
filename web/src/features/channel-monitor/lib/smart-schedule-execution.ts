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
import type { ChannelMonitorTask, ChannelMonitorTaskAdjustment } from '../types'

const FAILURE_STAGE_LABELS: Readonly<Record<string, string>> = {
  plan: '计划计算',
  write: '结果写入',
  configuration_conflict: '配置冲突',
}

const SMART_SCHEDULE_EXECUTION_SELECTION_STORAGE_KEY =
  'channel-monitor:smart-schedule-display:v1'

export type ChannelMonitorSmartScheduleExecutionSelection = {
  group: string
  model: string
}

const EMPTY_SMART_SCHEDULE_EXECUTION_SELECTION = {
  group: '',
  model: '',
} satisfies ChannelMonitorSmartScheduleExecutionSelection

export function loadChannelMonitorSmartScheduleExecutionSelection(): ChannelMonitorSmartScheduleExecutionSelection {
  if (typeof window === 'undefined') {
    return EMPTY_SMART_SCHEDULE_EXECUTION_SELECTION
  }
  try {
    const stored = window.localStorage.getItem(
      SMART_SCHEDULE_EXECUTION_SELECTION_STORAGE_KEY
    )
    if (!stored) return EMPTY_SMART_SCHEDULE_EXECUTION_SELECTION
    const parsed: unknown = JSON.parse(stored)
    if (
      !parsed ||
      typeof parsed !== 'object' ||
      !('group' in parsed) ||
      !('model' in parsed) ||
      typeof parsed.group !== 'string' ||
      typeof parsed.model !== 'string'
    ) {
      return EMPTY_SMART_SCHEDULE_EXECUTION_SELECTION
    }
    return { group: parsed.group, model: parsed.model }
  } catch {
    return EMPTY_SMART_SCHEDULE_EXECUTION_SELECTION
  }
}

export function saveChannelMonitorSmartScheduleExecutionSelection(
  selection: ChannelMonitorSmartScheduleExecutionSelection
) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(
      SMART_SCHEDULE_EXECUTION_SELECTION_STORAGE_KEY,
      JSON.stringify(selection)
    )
  } catch {}
}

export function orderChannelMonitorTasksByExecutionTime(
  tasks: readonly ChannelMonitorTask[]
): ChannelMonitorTask[] {
  return [...tasks].sort(
    (left, right) => right.created_at - left.created_at || right.id - left.id
  )
}

export function orderChannelMonitorSmartScheduleModels(
  availableModels: readonly string[],
  configuredOrder: readonly string[] = []
): string[] {
  const rank = new Map(configuredOrder.map((model, index) => [model, index]))
  return [...new Set(availableModels)].sort((left, right) => {
    const leftRank = rank.get(left)
    const rightRank = rank.get(right)
    if (leftRank !== undefined && rightRank !== undefined) {
      return leftRank - rightRank
    }
    if (leftRank !== undefined) return -1
    if (rightRank !== undefined) return 1
    return left.localeCompare(right)
  })
}

function routingValues(adjustment: ChannelMonitorTaskAdjustment) {
  if (adjustment.action === 'failed') {
    return {
      priority: adjustment.old_priority,
      weight: adjustment.old_weight,
    }
  }
  return {
    priority: adjustment.new_priority,
    weight: adjustment.new_weight,
  }
}

export function orderChannelMonitorSmartScheduleAdjustmentsByRoutingPolicy(
  adjustments: readonly ChannelMonitorTaskAdjustment[]
): ChannelMonitorTaskAdjustment[] {
  return [...adjustments].sort((left, right) => {
    const leftRouting = routingValues(left)
    const rightRouting = routingValues(right)
    if (leftRouting.priority !== rightRouting.priority) {
      return rightRouting.priority - leftRouting.priority
    }
    if (leftRouting.weight !== rightRouting.weight) {
      return rightRouting.weight - leftRouting.weight
    }
    return left.channel_id - right.channel_id
  })
}

export function filterChannelMonitorSmartScheduleAdjustments(
  adjustments: readonly ChannelMonitorTaskAdjustment[]
): ChannelMonitorTaskAdjustment[] {
  return adjustments.filter((adjustment) => adjustment.action !== 'unchanged')
}

export function formatChannelMonitorSmartScheduleFailureStage(stage?: string) {
  if (!stage) return ''
  return FAILURE_STAGE_LABELS[stage] ?? stage
}
