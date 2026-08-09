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

import type { ReactElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import type {
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorTask,
} from '../../types'
import { ChannelMonitorTaskAdjustmentDetails } from '../channel-monitor-task-adjustment-details'
import {
  ChannelMonitorTaskHistoryEntry,
  ChannelMonitorTaskPolicySummary,
  ChannelMonitorTaskRowDisclosure,
} from '../channel-monitor-task-history-dialog'

const noop = () => {}

const ADAPTIVE_SAMPLING_POLICY = {
  adaptive_sampling_enabled: true,
  adaptive_sampling_base_percent: 3,
  adaptive_sampling_max_percent: 30,
  adaptive_sampling_primary_min_percent: 70,
  adaptive_sampling_error_warning_percent: 5,
  adaptive_sampling_error_critical_percent: 15,
  adaptive_sampling_first_token_warning_seconds: 5,
  adaptive_sampling_first_token_critical_seconds: 10,
  adaptive_sampling_window_seconds: 600,
  adaptive_sampling_first_token_warning_request_percent: 10,
  adaptive_sampling_recover_request_percent: 95,
  adaptive_sampling_switch_confirm_request_percent: 95,
  adaptive_sampling_min_comparable_channels: 2,
} as const

function createGroupPolicy(
  group: string
): ChannelMonitorSmartScheduleGroupPolicy {
  return {
    ...ADAPTIVE_SAMPLING_POLICY,
    group,
    strategy: 'smart',
    stability_enabled: true,
    jitter_enabled: true,
    jitter_tolerance_percent: 5,
    jitter_slow_threshold_seconds: 10,
    scoring: {
      stability_percent: 50,
      primary_traffic_percent: 90,
      primary_switch_threshold_percent: 3,
      smart: {
        cost_ratio_percent: 40,
        first_token_percent: 40,
        tps_percent: 20,
      },
      ratio: {
        cost_ratio_percent: 70,
        first_token_percent: 20,
        tps_percent: 10,
      },
    },
    apply_mode: 'priority_weight',
    models: ['gpt-5'],
    min_samples: 20,
    recovery_stability_score: 95,
    fast_failure_penalty_percent: 40,
    fast_failure_seconds: 1,
    slow_failure_seconds: 10,
    cooldown_minutes: 30,
    sample_mode: 'traffic',
    sampling_order: 'priority_weight',
    exploration_traffic_percent: 3,
    exploration_max_prompt_tokens: 50_000,
    probe_interval_minutes: 10,
  }
}

function createTask(
  overrides: Partial<ChannelMonitorTask> = {}
): ChannelMonitorTask {
  return {
    id: 1,
    task_id: 'schedule-1',
    type: 'channel_smart_schedule',
    status: 'succeeded',
    state: null,
    result: {
      total: 4,
      updated: 1,
      unchanged: 1,
      skipped: 1,
      failed: 1,
      adjustments: [
        {
          channel_id: 7,
          channel_name: '主线路',
          group: 'vip',
          model: 'gpt-5',
          action: 'updated',
          old_priority: 80,
          new_priority: 100,
          old_weight: 20,
          new_weight: 75,
          score: 0.87346,
          previous_effective_time: 1_752_700_000,
          previous_effective_priority: 80,
          previous_effective_weight: 20,
          reason: '根据智能调度评分，在同一分组和模型调度池中调整权重',
        },
        {
          channel_id: 8,
          channel_name: '稳定线路',
          group: 'vip',
          model: 'gpt-5',
          action: 'unchanged',
          old_priority: 90,
          new_priority: 90,
          old_weight: 50,
          new_weight: 50,
          score: 0.65,
          reason: '计算后已是目标值，无需调整',
        },
        {
          channel_id: 9,
          channel_name: '暂停线路',
          group: 'default',
          model: 'gpt-4.1',
          action: 'skipped',
          old_priority: 80,
          new_priority: 80,
          old_weight: 10,
          new_weight: 10,
          reason: '该分组和模型路由未参与智能调度',
        },
        {
          channel_id: 10,
          channel_name: '写入失败线路',
          group: 'default',
          model: 'gpt-4.1',
          action: 'failed',
          old_priority: 80,
          new_priority: 100,
          old_weight: 10,
          new_weight: 60,
          failure_stage: 'write',
          previous_effective_time: 1_752_700_000,
          previous_effective_priority: 80,
          previous_effective_weight: 10,
          reason: '数据库写入失败',
        },
      ],
    },
    error: '',
    created_at: 1_752_777_845,
    updated_at: 1_752_777_846,
    ...overrides,
  }
}

describe('smart schedule task adjustment history', () => {
  test('summarizes explicit group policies instead of a default policy', () => {
    const task = createTask()
    task.result = {
      total: 4,
      updated: 2,
      failed: 0,
      performance_window_minutes: 60,
      stability_window_minutes: 30,
      force_reset: true,
      group_policies: [
        createGroupPolicy('vip'),
        createGroupPolicy('default'),
        createGroupPolicy('batch'),
      ],
    }

    const markup = renderToStaticMarkup(
      <ChannelMonitorTaskPolicySummary task={task} />
    )

    assert.ok(markup.includes('3 个分组策略 · vip、default、batch'))
    assert.ok(markup.includes('性能窗口 60 分钟 · 稳定性评分窗口 30 分钟'))
    assert.ok(markup.includes('强制重算'))
    assert.equal(markup.includes('只调整权重'), false)
  })

  test('renders an empty policy summary for records without group policies', () => {
    const task = createTask()
    task.result = {
      total: 0,
      updated: 0,
      failed: 0,
      performance_window_minutes: 30,
      stability_window_minutes: 15,
    }

    const markup = renderToStaticMarkup(
      <ChannelMonitorTaskPolicySummary task={task} />
    )

    assert.ok(markup.includes('未记录分组策略'))
    assert.ok(markup.includes('性能窗口 30 分钟 · 稳定性评分窗口 15 分钟'))
  })

  test('shows route changes, scores, actions, and full adjustment reasons', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTaskAdjustmentDetails
        task={createTask()}
        id='adjustment-details'
      />
    )

    assert.ok(markup.includes('id="adjustment-details"'))
    assert.ok(markup.includes('role="region"'))
    assert.ok(markup.includes('主线路'))
    assert.ok(markup.includes('ID 7 · vip / gpt-5'))
    assert.match(markup, /优先级[\s\S]*80 →[\s\S]*100/)
    assert.match(markup, /权重[\s\S]*20 →[\s\S]*75/)
    assert.ok(markup.includes('0.8735'))
    assert.ok(markup.includes('已调整'))
    assert.ok(markup.includes('保持'))
    assert.ok(markup.includes('已跳过'))
    assert.ok(markup.includes('失败'))
    assert.ok(markup.includes('调整原因：'))
    assert.ok(markup.includes('数据库写入失败'))
    assert.ok(markup.includes('失败阶段：'))
    assert.ok(markup.includes('结果写入'))
    assert.ok(markup.includes('上一轮生效结果：'))
    assert.ok(markup.includes('本轮失败未覆盖，上一轮结果继续生效'))
  })

  test('shows a clear accessible disclosure and toggles from click', () => {
    let toggleCount = 0
    const collapsed = ChannelMonitorTaskRowDisclosure({
      adjustmentCount: 4,
      truncated: false,
      expanded: false,
      controlsId: 'adjustment-details',
      onToggle: () => {
        toggleCount++
      },
    }) as ReactElement<{ onClick: () => void }>
    const collapsedMarkup = renderToStaticMarkup(collapsed)

    assert.ok(collapsedMarkup.includes('aria-expanded="false"'))
    assert.ok(collapsedMarkup.includes('aria-controls="adjustment-details"'))
    assert.ok(collapsedMarkup.includes('aria-label="查看执行详情，共 4 条"'))
    assert.ok(collapsedMarkup.includes('查看详情'))
    collapsed.props.onClick()
    assert.equal(toggleCount, 1)

    const expandedMarkup = renderToStaticMarkup(
      <ChannelMonitorTaskRowDisclosure
        adjustmentCount={4}
        truncated={false}
        expanded
        controlsId='adjustment-details'
        onToggle={noop}
      />
    )
    assert.ok(expandedMarkup.includes('aria-expanded="true"'))
    assert.ok(expandedMarkup.includes('aria-label="收起执行详情"'))
    assert.ok(expandedMarkup.includes('收起详情'))
    assert.ok(expandedMarkup.includes('rotate-180'))
  })

  test('toggles adjustment details when the expandable summary row is clicked', () => {
    let toggleCount = 0
    const entry = ChannelMonitorTaskHistoryEntry({
      task: createTask(),
      expanded: false,
      onToggleDetails: () => {
        toggleCount++
      },
    }) as ReactElement<{
      children: ReactElement<{
        onClick: (event: {
          target: { closest: (selectors: string) => Element | null }
        }) => void
        onKeyDown: (event: {
          key: string
          target: unknown
          currentTarget: unknown
          preventDefault: () => void
        }) => void
      }>[]
    }>
    const summaryRow = entry.props.children[0]
    const markup = renderToStaticMarkup(
      <table>
        <tbody>
          <ChannelMonitorTaskHistoryEntry
            task={createTask()}
            expanded={false}
            onToggleDetails={noop}
          />
        </tbody>
      </table>
    )

    assert.ok(markup.includes('data-expandable="true"'))
    assert.ok(markup.includes('cursor-pointer'))
    assert.ok(markup.includes('tabindex="0"'))
    assert.ok(markup.includes('aria-expanded="false"'))
    assert.ok(
      markup.includes('aria-controls="channel-monitor-task-details-schedule-1"')
    )
    assert.equal(markup.includes('查看调整明细'), false)

    summaryRow.props.onClick({ target: { closest: () => null } })
    assert.equal(toggleCount, 1)

    summaryRow.props.onClick({
      target: { closest: () => ({}) as Element },
    })
    assert.equal(toggleCount, 1)

    let prevented = false
    summaryRow.props.onKeyDown({
      key: 'Enter',
      target: summaryRow,
      currentTarget: summaryRow,
      preventDefault: () => {
        prevented = true
      },
    })
    assert.equal(prevented, true)
    assert.equal(toggleCount, 2)

    summaryRow.props.onKeyDown({
      key: ' ',
      target: {},
      currentTarget: summaryRow,
      preventDefault: () => {
        throw new Error('child control key event must not be handled by row')
      },
    })
    assert.equal(toggleCount, 2)
  })

  test('shows recorded failures when route adjustments are unavailable', () => {
    const task = createTask()
    task.result = {
      total: 2,
      updated: 1,
      failed: 1,
      failures: [
        {
          channel_id: 12,
          channel_name: '失败渠道',
          group: 'vip',
          model: 'gpt-5',
          failure_stage: 'plan',
          error: '任务执行失败',
        },
      ],
    }
    const markup = renderToStaticMarkup(
      <ChannelMonitorTaskAdjustmentDetails task={task} id='failure-details' />
    )

    assert.ok(markup.includes('本次任务未记录逐路由调整明细'))
    assert.ok(markup.includes('失败渠道（ID 12）'))
    assert.ok(markup.includes('vip / gpt-5'))
    assert.ok(markup.includes('失败阶段：计划计算'))
    assert.ok(markup.includes('任务执行失败'))
  })

  test('distinguishes an empty scheduling scope from missing adjustments', () => {
    const task = createTask()
    task.result = { total: 0, updated: 0, failed: 0 }
    const markup = renderToStaticMarkup(
      <ChannelMonitorTaskAdjustmentDetails task={task} id='empty-details' />
    )

    assert.ok(markup.includes('本次没有符合调度范围的路由'))
    assert.ok(markup.includes('请检查分组策略、模型范围和渠道参与状态'))
  })
})
