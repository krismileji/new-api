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
  pending = false
) {
  return renderToStaticMarkup(
    <ChannelMonitorSmartScheduleCell
      routes={routes}
      pending={pending}
      onUpdate={noop}
      onOpen={noop}
    />
  )
}

describe('channel monitor smart schedule cell status', () => {
  test('shows an explicit empty state when the channel has no routes', () => {
    const markup = renderCell([])

    assert.ok(markup.includes('暂无路由'))
  })

  test('summarizes participation and priority-weight ranges by group', () => {
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
    assert.match(markup, /default[\s\S]*P80-90 \/ W50-60/)
    assert.match(markup, /vip[\s\S]*P100 \/ W100/)
    assert.ok(markup.includes('部分参与'))
    assert.ok(markup.includes('查看 测试渠道 的智能调度详情'))
    assert.ok(markup.includes('role="switch"'))
  })

  test('shows route protection counts without a separate clear button', () => {
    const markup = renderCell([
      createRoute({ state: { stability_state: 'degraded' } }),
      createRoute({
        model: 'model-b',
        state: { model: 'model-b', stability_state: 'probing' },
      }),
    ])

    assert.ok(markup.includes('低成功率 1'))
    assert.ok(markup.includes('稳定性试放 1'))
    assert.equal(markup.includes('手动解除'), false)
  })

  test('keeps participation configurable when the channel is disabled', () => {
    const markup = renderCell([createRoute({ channel_status: 2 })])
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(markup.includes('渠道禁用'))
    assert.ok(switchElement)
    assert.equal(switchElement.includes('aria-disabled="true"'), false)
  })

  test('disables participation while a participation update is pending', () => {
    const markup = renderCell([createRoute()], true)
    const switchElement = markup.match(/<[^>]*role="switch"[^>]*>/)?.[0] ?? ''

    assert.ok(switchElement.includes('aria-disabled="true"'))
  })
})
