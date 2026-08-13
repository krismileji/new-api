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
import { spawnSync } from 'node:child_process'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { renderToStaticMarkup } from 'react-dom/server'

import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteState,
} from '../../types'
import { ChannelMonitorSmartScheduleCell } from '../channel-monitor-smart-schedule-cell'
import { createSmartScheduleCellRoute as createRoute } from './smart-schedule-cell-test-data'

const noop = () => {}

function renderCell(
  routes: ChannelMonitorSmartScheduleRoute[],
  pending = false,
  selectedGroupModel: Pick<
    ChannelMonitorSmartScheduleRoute,
    'group' | 'model'
  > | null = { group: 'default', model: 'model-a' }
) {
  return renderToStaticMarkup(
    <ChannelMonitorSmartScheduleCell
      channelName='测试渠道'
      routes={routes}
      selectedGroupModel={selectedGroupModel}
      pending={pending}
      onUpdate={noop}
    />
  )
}

describe('channel monitor smart schedule cell status', () => {
  test('shows fixed priority and weight placeholders when the channel has no routes', () => {
    const markup = renderCell([])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.match(markup, /优先级[\s\S]*—[\s\S]*权重[\s\S]*—/)
    assert.ok(switchElement.includes('aria-disabled="true"'))
  })

  test('shows only the selected group-model priority and weight', () => {
    const markup = renderCell(
      [
        createRoute(),
        createRoute({
          model: 'model-b',
          priority: 90,
          weight: 50,
          state: { model: 'model-b', excluded: true },
        }),
        createRoute({
          group: 'vip',
          model: 'model-c',
          priority: 100,
          weight: 100,
          state: { group: 'vip', model: 'model-c' },
        }),
      ],
      false,
      { group: 'default', model: 'model-b' }
    )

    assert.match(markup, /优先级[\s\S]*90[\s\S]*权重[\s\S]*50/)
    assert.equal(markup.includes('路由参与'), false)
    assert.equal(markup.includes('可调度'), false)
    assert.equal(markup.includes('主候选'), false)
    assert.equal(markup.includes('预计'), false)
    assert.equal(markup.includes('还有'), false)
    assert.equal(markup.includes('智能调度详情'), false)
    assert.ok(markup.includes('role="switch"'))
    assert.ok(markup.indexOf('role="switch"') < markup.indexOf('优先级'))
  })

  test('keeps the compact summary at exactly three visible lines', () => {
    const markup = renderCell([createRoute()])

    assert.equal((markup.match(/data-smart-schedule-line=/g) ?? []).length, 3)
    assert.match(markup, /优先级[\s\S]*80[\s\S]*权重[\s\S]*60/)
    assert.ok(markup.includes('常规调度'))
  })

  test('shows active special traffic and stability states for the selected route', () => {
    const cases: Array<{
      state: Partial<ChannelMonitorSmartScheduleRouteState>
      expected: string
    }> = [
      { state: { stability_state: 'degraded' }, expected: '稳定性降级' },
      { state: { stability_state: 'probing' }, expected: '稳定性释放' },
      {
        state: {
          temporary_traffic_kind: 'insufficient_samples',
          temporary_traffic_target_percent: 3,
        },
        expected: '统一探索采样 3%',
      },
      {
        state: {
          temporary_traffic_kind: 'adaptive_sampling',
          temporary_traffic_target_percent: 1.5,
        },
        expected: '自适应备援采样 1.5%',
      },
    ]

    for (const item of cases) {
      assert.ok(
        renderCell([createRoute({ state: item.state })]).includes(item.expected)
      )
    }
  })

  test('shows a route traffic pause instead of stale scheduling protection', () => {
    const markup = renderCell([
      createRoute({
        traffic_paused_until: 4_102_444_800,
        state: {
          stability_state: 'degraded',
          stability_until: 4_102_444_800,
          temporary_traffic_kind: 'insufficient_samples',
          temporary_traffic_target_percent: 3,
        },
      }),
    ])

    assert.ok(markup.includes('流量已暂停'))
    assert.equal(markup.includes('>稳定性降级</'), false)
    assert.equal(markup.includes('探索流量 3%'), false)
    assert.match(markup, /优先级[\s\S]*80[\s\S]*权重[\s\S]*60/)
  })

  test('shows nonparticipation before stale pause and protection state', () => {
    const markup = renderCell([
      createRoute({
        traffic_paused_until: 4_102_444_800,
        state: {
          participation_set: false,
          excluded: false,
          stability_state: 'degraded',
          stability_since: 1_752_700_000,
          stability_until: 4_102_444_800,
          temporary_traffic_kind: 'insufficient_samples',
          temporary_traffic_since: 1_752_700_000,
          temporary_traffic_target_percent: 3,
          manual_primary_until: 4_102_444_800,
        },
      }),
    ])

    assert.ok(markup.includes('查看当前调度状态详情：未参与调度'))
    assert.equal(markup.includes('流量已暂停'), false)
    assert.equal(markup.includes('稳定性降级'), false)
    assert.equal(markup.includes('统一探索采样'), false)
    assert.equal(markup.includes('固定主渠道'), false)
    assert.equal(markup.includes('aria-label="解除 '), false)
  })

  test('opens clearing from degradation, release, and exploration states only', () => {
    const fixturePath = fileURLToPath(
      new URL('./smart-schedule-cell-protection.fixture.tsx', import.meta.url)
    )
    const execution = spawnSync(process.execPath, [fixturePath], {
      cwd: process.cwd(),
      encoding: 'utf8',
    })

    assert.equal(
      execution.status,
      0,
      execution.stderr || execution.stdout || '调度保护解除交互测试失败'
    )
  })

  test('shows fixed intent together with stability degradation', () => {
    const markup = renderCell([
      createRoute({
        state: {
          stability_state: 'degraded',
          stability_since: 1_752_700_000,
          stability_until: 4_102_444_800,
          manual_primary_until: 4_102_444_800,
          manual_primary_allow_stability_degrade: true,
        },
      }),
    ])

    assert.ok(markup.includes('稳定性降级'))
    assert.ok(markup.includes('固定主渠道'))
    assert.ok(markup.includes('查看当前调度状态详情'))
  })

  test('shows break-even fallback without disabling manual fixed intent', () => {
    const fallback = createRoute({
      economic_role: 'break_even_fallback',
      cost_ratio: 1,
      group_ratio: 1,
      gross_margin: 0,
    })
    const fallbackMarkup = renderCell([fallback])

    assert.ok(fallbackMarkup.includes('保本兜底'))

    const fixedMarkup = renderCell([
      createRoute({
        economic_role: 'break_even_fallback',
        state: { manual_primary_until: 4_102_444_800 },
      }),
    ])
    assert.ok(fixedMarkup.includes('保本兜底 · 已手动固定'))
  })

  test('opens the selected route status details from the third line', () => {
    const fixturePath = fileURLToPath(
      new URL('./smart-schedule-cell-interaction.fixture.tsx', import.meta.url)
    )
    const execution = spawnSync(process.execPath, [fixturePath], {
      cwd: process.cwd(),
      encoding: 'utf8',
    })

    assert.equal(
      execution.status,
      0,
      execution.stderr || execution.stdout || '调度状态详情交互测试失败'
    )
  })

  test('does not leak a different group-model state into the selected route', () => {
    const markup = renderCell([
      createRoute(),
      createRoute({
        model: 'model-b',
        state: { model: 'model-b', stability_state: 'degraded' },
      }),
    ])

    assert.equal(markup.includes('稳定性降级'), false)
    assert.ok(markup.includes('常规调度'))
  })

  test('keeps participation configurable when the channel is disabled', () => {
    const markup = renderCell([
      createRoute({
        channel_status: 2,
        state: {
          stability_state: 'degraded',
          stability_until: 4_102_444_800,
          temporary_traffic_kind: 'insufficient_samples',
          temporary_traffic_target_percent: 3,
          manual_primary_until: 4_102_444_800,
        },
      }),
    ])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(markup.includes('渠道禁用'))
    assert.ok(markup.includes('查看当前调度状态详情：渠道禁用'))
    assert.ok(
      markup.includes(
        'aria-label="解除 测试渠道 default model-a 的稳定性降级保护"'
      )
    )
    assert.equal(markup.includes('>稳定性降级</'), false)
    assert.equal(markup.includes('固定主渠道'), false)
    assert.equal(markup.includes('探索流量 3%'), false)
    assert.equal(markup.includes('不可调度'), false)
    assert.match(markup, /优先级[\s\S]*80[\s\S]*权重[\s\S]*60/)
    assert.ok(switchElement)
    assert.equal(switchElement.includes('aria-disabled="true"'), false)
  })

  test('shows only route disabled when an ability is unavailable', () => {
    const markup = renderCell([
      createRoute({
        enabled: false,
        state: {
          stability_state: 'probing',
          temporary_traffic_kind: 'adaptive_sampling',
          temporary_traffic_target_percent: 1.5,
        },
      }),
    ])

    assert.ok(markup.includes('查看当前调度状态详情：路由禁用'))
    assert.ok(
      markup.includes('aria-label="解除 测试渠道 default model-a 的稳定性释放"')
    )
    assert.equal(markup.includes('>稳定性释放</'), false)
    assert.equal(markup.includes('自适应备援采样 1.5%'), false)
  })

  test('shows placeholders when the selected group-model is absent', () => {
    const markup = renderCell([createRoute()], false, {
      group: 'vip',
      model: 'model-b',
    })

    assert.match(markup, /优先级[\s\S]*—[\s\S]*权重[\s\S]*—/)
  })

  test('shows an uninitialized route as not participating', () => {
    const markup = renderCell([
      createRoute({ state: { participation_set: false, excluded: false } }),
    ])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(switchElement.includes('aria-checked="false"'))
    assert.ok(markup.includes('未参与 0/1'))
  })

  test('shows partial participation without hiding the bulk-toggle behavior', () => {
    const markup = renderCell([
      createRoute(),
      createRoute({
        model: 'model-b',
        state: { model: 'model-b', excluded: true },
      }),
    ])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(markup.includes('部分 1/2'))
    assert.ok(switchElement.includes('aria-checked="true"'))
    assert.ok(
      switchElement.includes(
        'aria-label="取消 测试渠道 全部路由的智能调度参与"'
      )
    )
  })

  test('reveals the nonparticipating models from the partial badge', () => {
    const fixturePath = fileURLToPath(
      new URL(
        './smart-schedule-cell-partial-tooltip.fixture.tsx',
        import.meta.url
      )
    )
    const execution = spawnSync(process.execPath, [fixturePath], {
      cwd: process.cwd(),
      encoding: 'utf8',
    })

    assert.equal(
      execution.status,
      0,
      execution.stderr ||
        execution.stdout ||
        '部分参与模型 Tooltip 交互测试失败'
    )
  })

  test('disables participation while a participation update is pending', () => {
    const markup = renderCell([createRoute()], true)
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(switchElement.includes('aria-disabled="true"'))
  })
})
