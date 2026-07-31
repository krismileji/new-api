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
    manual_primary_until: 0,
    manual_primary_allow_stability_degrade: false,
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
    shared_samples: {
      id: 0,
      channel_id: 7,
      model: 'model-a',
      window_start: 0,
      last_time: 0,
      last_success: false,
      last_error: '',
      sample_count: 0,
      success_count: 0,
      failure_duration_sample_count: 0,
      average_failure_duration_ms: null,
      first_token_sample_count: 0,
      average_first_token_ms: null,
      tps_sample_count: 0,
      average_tps: null,
    },
    ...overrides,
    state: { ...defaultState, ...overrides.state },
  }
}

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

  test('keeps route protection details out of the compact cell', () => {
    const markup = renderCell([
      createRoute({ state: { stability_state: 'degraded' } }),
    ])

    assert.equal(markup.includes('稳定性降级'), false)
    assert.match(markup, /优先级[\s\S]*80[\s\S]*权重[\s\S]*60/)
  })

  test('keeps participation configurable when the channel is disabled', () => {
    const markup = renderCell([createRoute({ channel_status: 2 })])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.equal(markup.includes('渠道禁用'), false)
    assert.equal(markup.includes('不可调度'), false)
    assert.match(markup, /优先级[\s\S]*80[\s\S]*权重[\s\S]*60/)
    assert.ok(switchElement)
    assert.equal(switchElement.includes('aria-disabled="true"'), false)
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
  })

  test('disables participation while a participation update is pending', () => {
    const markup = renderCell([createRoute()], true)
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(switchElement.includes('aria-disabled="true"'))
  })
})
