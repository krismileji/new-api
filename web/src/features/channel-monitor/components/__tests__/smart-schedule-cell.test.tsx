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

import { renderToStaticMarkup } from 'react-dom/server'

import { placeChannelMonitorSmartScheduleRoutes } from '../../lib/smart-schedule-summary'
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteState,
} from '../../types'
import { ChannelMonitorSmartScheduleCell } from '../channel-monitor-smart-schedule-cell'

const noop = () => {}

type RouteOverrides = Omit<
  Partial<ChannelMonitorSmartScheduleRoute>,
  'state'
> & {
  state?: Partial<ChannelMonitorSmartScheduleRouteState>
}

function createRoute(
  overrides: RouteOverrides = {}
): ChannelMonitorSmartScheduleRoute {
  const defaultState: ChannelMonitorSmartScheduleRouteState = {
    id: 1,
    channel_id: 7,
    group: 'default',
    model: 'model-a',
    participation_set: true,
    excluded: false,
    last_schedule_status: 'succeeded',
    last_schedule_error: '',
    last_schedule_score: 0.8,
    last_schedule_priority: 80,
    last_schedule_weight: 60,
    last_schedule_time: 1_752_777_845,
    stability_state: '',
    stability_until: 0,
    stability_since: 0,
    stability_saved_priority: 0,
    stability_saved_weight: 0,
    exploration_active: false,
    exploration_since: 0,
    exploration_saved_priority: 0,
    exploration_saved_weight: 0,
    probe_window_start: 0,
    probe_last_time: 0,
    probe_last_success: false,
    probe_last_error: '',
    probe_sample_count: 0,
    probe_success_count: 0,
    probe_failure_duration_sample_count: 0,
    probe_average_failure_duration_ms: null,
    probe_first_token_sample_count: 0,
    probe_average_first_token_ms: null,
    probe_tps_sample_count: 0,
    probe_average_tps: null,
  }
  return {
    channel_id: 7,
    channel_name: '测试渠道',
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 100,
    group: 'default',
    model: 'model-a',
    enabled: true,
    priority: 80,
    weight: 60,
    ...overrides,
    state: { ...defaultState, ...overrides.state },
  }
}

function renderCell(
  routes: ChannelMonitorSmartScheduleRoute[],
  pending = false,
  groupRatios: Readonly<Record<string, number>> = {
    default: 1,
    vip: 0.5,
  }
) {
  return renderToStaticMarkup(
    <ChannelMonitorSmartScheduleCell
      routes={routes}
      groupRatios={groupRatios}
      placements={placeChannelMonitorSmartScheduleRoutes(routes)}
      pending={pending}
      onUpdate={noop}
      onOpen={noop}
      onClearStability={noop}
    />
  )
}

describe('channel monitor smart schedule cell status', () => {
  test('shows an explicit empty state when the channel has no routes', () => {
    const markup = renderCell([])

    assert.ok(markup.includes('暂无路由'))
  })

  test('summarizes participation and highlights group-model routing roles', () => {
    const markup = renderCell([
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
    ])

    assert.ok(markup.includes('2/3 路由参与'))
    assert.ok(markup.includes('2 可调度'))
    assert.match(markup, /vip \/ model-c[\s\S]*P100 \/ W100/)
    assert.ok(markup.includes('主候选'))
    assert.ok(markup.includes('预计 100.0%'))
    assert.ok(markup.includes('还有 1 条分组模型路由'))
    assert.equal(markup.includes('部分参与'), false)
    assert.equal(markup.includes('渠道禁用'), false)
    assert.ok(markup.includes('查看 测试渠道 的智能调度详情'))
    assert.ok(markup.includes('role="switch"'))
    assert.ok(markup.indexOf('role="switch"') < markup.indexOf('2/3 路由参与'))
    assert.ok(
      markup.indexOf('vip / model-c') < markup.indexOf('default / model-a')
    )
  })

  test('makes protected route text the manual recovery entry point', () => {
    const markup = renderCell([
      createRoute({ state: { stability_state: 'degraded' } }),
      createRoute({
        model: 'model-b',
        state: { model: 'model-b', stability_state: 'probing' },
      }),
    ])

    assert.ok(markup.includes('稳定性降级'))
    assert.ok(markup.includes('稳定性试放'))
    assert.ok(markup.includes('查看 default model-a 的智能调度详情'))
    assert.ok(markup.includes('的稳定性降级保护'))
    assert.ok(markup.includes('的稳定性试放保护'))
    const protectedBadges = markup.match(/<button[^>]*data-slot="badge"[^>]*>/g)
    assert.equal(protectedBadges?.length, 2)
    assert.equal(markup.includes('手动解除'), false)
  })

  test('keeps participation configurable when the channel is disabled', () => {
    const markup = renderCell([createRoute({ channel_status: 2 })])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.equal(markup.includes('渠道禁用'), false)
    assert.ok(markup.includes('不可调度'))
    assert.ok(switchElement)
    assert.equal(switchElement.includes('aria-disabled="true"'), false)
  })

  test('shows an uninitialized route as not participating', () => {
    const markup = renderCell([
      createRoute({ state: { participation_set: false, excluded: false } }),
    ])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(markup.includes('0/1 路由参与'))
    assert.ok(markup.includes('0 可调度'))
    assert.ok(markup.includes('未参与'))
    assert.ok(switchElement.includes('aria-checked="false"'))
  })

  test('disables participation while a participation update is pending', () => {
    const markup = renderCell([createRoute()], true)
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(switchElement.includes('aria-disabled="true"'))
  })
})
