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

import type { ChannelMonitorSmartScheduleRoute } from '../../types'
import {
  channelMonitorSmartScheduleRouteParticipates,
  channelMonitorSmartScheduleRouteKey,
  compareChannelMonitorSmartScheduleGroupsByRatio,
  compareChannelMonitorSmartScheduleRoutesByAttention,
  compareChannelMonitorSmartScheduleRoutesByPool,
  getChannelMonitorSmartSchedulePoolStatus,
  placeChannelMonitorSmartScheduleRoutes,
} from '../smart-schedule-summary'

const normalPool = {
  routeCount: 3,
  participatingCount: 3,
  activeCount: 3,
  degradedCount: 0,
  probingCount: 0,
  explorationCount: 0,
}

function createRoute(
  channelId: number,
  group: string,
  model: string,
  priority: number,
  weight: number
): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: channelId,
    channel_name: `渠道 ${channelId}`,
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 100,
    group,
    model,
    enabled: true,
    priority,
    weight,
    state: {
      id: channelId,
      channel_id: channelId,
      group,
      model,
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: 0.8,
      last_schedule_priority: priority,
      last_schedule_weight: weight,
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
    },
  }
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
      '稳定性降级'
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

describe('smart schedule route placement', () => {
  test('keeps uninitialized participation out of candidates even when it is not excluded', () => {
    const uninitialized = createRoute(1, 'vip', 'model-a', 100, 100)
    uninitialized.state.participation_set = false
    const active = createRoute(2, 'vip', 'model-a', 80, 100)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      uninitialized,
      active,
    ])

    assert.equal(
      channelMonitorSmartScheduleRouteParticipates(uninitialized),
      false
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(uninitialized))?.role,
      'excluded'
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(active)),
      {
        role: 'primary',
        estimatedShare: 1,
        topPriority: 80,
        candidateCount: 1,
      }
    )
  })

  test('calculates traffic only inside the highest priority layer of each group-model pool', () => {
    const primary = createRoute(1, 'vip', 'model-a', 100, 80)
    const candidate = createRoute(2, 'vip', 'model-a', 100, 20)
    const standby = createRoute(3, 'vip', 'model-a', 90, 100)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      standby,
      candidate,
      primary,
    ])

    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(primary)),
      {
        role: 'primary',
        estimatedShare: 0.8,
        topPriority: 100,
        candidateCount: 2,
      }
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(candidate)),
      {
        role: 'candidate',
        estimatedShare: 0.2,
        topPriority: 100,
        candidateCount: 2,
      }
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(standby)),
      {
        role: 'standby',
        estimatedShare: null,
        topPriority: 100,
        candidateCount: 2,
      }
    )
  })

  test('isolates shares by group and model and splits zero-weight candidates evenly', () => {
    const vipFirst = createRoute(1, 'vip', 'model-a', 100, 0)
    const vipSecond = createRoute(2, 'vip', 'model-a', 100, 0)
    const standard = createRoute(3, 'standard', 'model-a', 70, 1)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      vipFirst,
      vipSecond,
      standard,
    ])

    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(vipFirst))
        ?.estimatedShare,
      0.5
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(vipSecond))
        ?.estimatedShare,
      0.5
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(standard))
        ?.estimatedShare,
      1
    )
  })

  test('keeps participation and availability separate from routing candidates', () => {
    const excluded = createRoute(1, 'vip', 'model-a', 100, 100)
    excluded.state.excluded = true
    const disabled = createRoute(2, 'vip', 'model-a', 100, 100)
    disabled.channel_status = 2
    const active = createRoute(3, 'vip', 'model-a', 80, 50)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      excluded,
      disabled,
      active,
    ])

    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(excluded))?.role,
      'excluded'
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(disabled))?.role,
      'unavailable'
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(active))?.role,
      'primary'
    )
  })
})

describe('smart schedule route ordering', () => {
  test('orders groups by ratio ascending and then by group name', () => {
    const groups = ['standard', 'premium', 'default', 'vip'].sort(
      (first, second) =>
        compareChannelMonitorSmartScheduleGroupsByRatio(first, second, {
          premium: 1.5,
          default: 1,
          standard: 1,
          vip: 0.5,
        })
    )

    assert.deepEqual(groups, ['vip', 'default', 'standard', 'premium'])
  })

  test('shows protection and failure states before active routing roles', () => {
    const degraded = createRoute(1, 'vip', 'model-a', 100, 100)
    degraded.state.stability_state = 'degraded'
    const probing = createRoute(2, 'vip', 'model-a', 100, 90)
    probing.state.stability_state = 'probing'
    const failed = createRoute(3, 'vip', 'model-a', 100, 80)
    failed.state.last_schedule_status = 'failed'
    const primary = createRoute(4, 'vip', 'model-a', 120, 70)
    const candidate = createRoute(5, 'vip', 'model-a', 120, 60)
    const standby = createRoute(6, 'vip', 'model-a', 110, 100)
    const unavailable = createRoute(7, 'vip', 'model-a', 130, 100)
    unavailable.channel_status = 2
    const excluded = createRoute(8, 'vip', 'model-a', 140, 100)
    excluded.state.excluded = true
    const routes = [
      excluded,
      standby,
      candidate,
      unavailable,
      failed,
      primary,
      probing,
      degraded,
    ]
    const placements = placeChannelMonitorSmartScheduleRoutes(routes)

    const sorted = [...routes].sort((first, second) =>
      compareChannelMonitorSmartScheduleRoutesByAttention(
        first,
        second,
        placements
      )
    )

    assert.deepEqual(
      sorted.map((route) => route.channel_id),
      [1, 2, 3, 4, 5, 6, 7, 8]
    )
  })

  test('groups detail rows by group and model before applying attention order', () => {
    const aModelAHealthy = createRoute(1, 'a-group', 'model-a', 80, 50)
    const aModelADegraded = createRoute(2, 'a-group', 'model-a', 70, 40)
    aModelADegraded.state.stability_state = 'degraded'
    const aModelB = createRoute(3, 'a-group', 'model-b', 100, 100)
    const bModelADegraded = createRoute(4, 'b-group', 'model-a', 100, 100)
    bModelADegraded.state.stability_state = 'degraded'
    const routes = [bModelADegraded, aModelB, aModelAHealthy, aModelADegraded]
    const placements = placeChannelMonitorSmartScheduleRoutes(routes)

    const sorted = [...routes].sort((first, second) =>
      compareChannelMonitorSmartScheduleRoutesByPool(first, second, placements)
    )

    assert.deepEqual(
      sorted.map((route) => route.channel_id),
      [2, 1, 3, 4]
    )
  })

  test('orders route groups by ratio before model and attention state', () => {
    const lowRatioHealthy = createRoute(1, 'vip', 'model-b', 80, 50)
    const normalRatioDegraded = createRoute(2, 'default', 'model-a', 100, 100)
    normalRatioDegraded.state.stability_state = 'degraded'
    const highRatio = createRoute(3, 'premium', 'model-a', 100, 100)
    const routes = [highRatio, normalRatioDegraded, lowRatioHealthy]
    const placements = placeChannelMonitorSmartScheduleRoutes(routes)

    const sorted = [...routes].sort((first, second) =>
      compareChannelMonitorSmartScheduleRoutesByPool(
        first,
        second,
        placements,
        { vip: 0.5, default: 1, premium: 1.5 }
      )
    )

    assert.deepEqual(
      sorted.map((route) => route.group),
      ['vip', 'default', 'premium']
    )
  })
})
