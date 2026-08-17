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
import { describe, test } from 'vitest'

import { renderToStaticMarkup } from 'react-dom/server'

import type { ChannelMonitorSmartScheduleRoute } from '../../types'
import { ChannelMonitorSmartScheduleRouteStatus } from '../channel-monitor-smart-schedule-route-details'
import { ChannelMonitorSmartScheduleRouteState } from '../channel-monitor-smart-schedule-route-state'

function createProtectedRoute(
  stabilityState: 'degraded' | 'probing'
): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: 7,
    channel_name: '测试渠道',
    channel_status: 2,
    channel_priority: 100,
    channel_weight: 100,
    group: 'vip',
    model: 'model-a',
    enabled: false,
    priority: 0,
    weight: 0,
    shared_samples: {
      id: 0,
      channel_id: 7,
      model: 'model-a',
      window_start: 0,
      observation_since: 0,
      recovery_success_count: 0,
      recovery_success_at: 0,
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
    state: {
      id: 1,
      channel_id: 7,
      group: 'vip',
      model: 'model-a',
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: null,
      last_schedule_priority: 0,
      last_schedule_weight: 0,
      last_schedule_time: 1_752_777_845,
      stability_state: stabilityState,
      stability_until: 1_752_777_845,
      stability_since: 1_752_700_000,
      stability_saved_priority: 95,
      stability_saved_weight: 70,
      runtime_protection_until: 0,
      base_rank: 1,
      base_priority: 95,
      base_weight: 70,
      temporary_traffic_kind: '',
      temporary_traffic_since: 0,
      temporary_traffic_target_percent: 0,
      rolling_stability_score: null,
      rolling_stability_sample_count: 0,
      rolling_stability_slow_count: 0,
      rolling_stability_allowed_slow_count: 0,
      rolling_stability_updated_at: 0,
      sampling_debt: 0,
      sampling_candidate: false,
      sampling_order: '',
      last_sampling_at: 0,
      manual_primary_until: 0,
      manual_primary_allow_stability_degrade: false,
    },
  }
}

describe('smart schedule route protection state', () => {
  test('shows nonparticipation before stale pause and protection state', () => {
    const route = createProtectedRoute('degraded')
    route.channel_status = 1
    route.enabled = true
    route.traffic_paused_until = 4_102_444_800
    route.state.excluded = true
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteState
        route={route}
        onProtectedStatusClick={() => {}}
      />
    )

    assert.ok(markup.includes('未参与'))
    assert.equal(markup.includes('流量已暂停'), false)
    assert.equal(markup.includes('稳定性降级'), false)
    assert.equal(markup.includes('aria-label="解除 '), false)
  })

  test('shows channel disabled instead of stale stability degradation', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteState
        route={createProtectedRoute('degraded')}
        onProtectedStatusClick={() => {}}
      />
    )

    assert.ok(markup.includes('渠道禁用'))
    assert.equal(markup.includes('>稳定性降级</'), false)
    assert.ok(
      markup.includes('aria-label="解除 测试渠道 vip model-a 的稳定性降级保护"')
    )
    assert.equal(markup.includes('未参与'), false)
  })

  test('shows route disabled instead of stale stability probing', () => {
    const route = createProtectedRoute('probing')
    route.channel_status = 1
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteState
        route={route}
        onProtectedStatusClick={() => {}}
      />
    )

    assert.ok(markup.includes('路由禁用'))
    assert.equal(markup.includes('>稳定性试放</'), false)
    assert.ok(
      markup.includes('aria-label="解除 测试渠道 vip model-a 的稳定性试放"')
    )
  })

  test('keeps stability protection clickable while the route is available', () => {
    const route = createProtectedRoute('degraded')
    route.channel_status = 1
    route.enabled = true
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteState
        route={route}
        onProtectedStatusClick={() => {}}
      />
    )

    assert.ok(markup.includes('稳定性降级'))
    assert.match(
      markup,
      /<button[^>]*data-slot="badge"[^>]*aria-label="解除 测试渠道 vip model-a 的稳定性降级保护"[^>]*>/
    )
    assert.equal(markup.includes('渠道禁用'), false)
  })

  test('shows traffic paused instead of stale stability degradation', () => {
    const route = createProtectedRoute('degraded')
    route.channel_status = 1
    route.enabled = true
    route.traffic_paused_until = 4_102_444_800
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteStatus
        route={route}
        placement={undefined}
        onClearProtection={() => {}}
      />
    )

    assert.ok(markup.includes('流量已暂停'))
    assert.equal(markup.includes('>稳定性降级</'), false)
    assert.equal(markup.includes('不可调度'), false)
  })

  test('shows whether a rate-limited route is bypassed or used as the final fallback', () => {
    const route = createProtectedRoute('degraded')
    route.channel_status = 1
    route.enabled = true
    route.rate_limit_cooldown_until = 4_102_444_800
    const bypassedMarkup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteStatus
        route={route}
        placement={undefined}
        onClearProtection={() => {}}
      />
    )
    const fallbackMarkup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteStatus
        route={route}
        placement={{
          role: 'rate_limited',
          estimatedShare: 1,
          topPriority: 100,
          candidateCount: 1,
          actualPrimaryChannelId: route.channel_id,
          scoringWinnerChannelId: 0,
          actualHighestPriority: 100,
          actualTopLayerChannelIds: [route.channel_id],
          isActualPrimary: true,
          isScoringWinner: false,
          isActualTopLayer: true,
        }}
        onClearProtection={() => {}}
      />
    )

    assert.ok(bypassedMarkup.includes('429 冷却'))
    assert.equal(bypassedMarkup.includes('兜底中'), false)
    assert.ok(fallbackMarkup.includes('429 冷却 · 兜底中'))
    assert.equal(fallbackMarkup.includes('>稳定性降级</'), false)
  })

  test('keeps exploration clearing available while the channel is disabled', () => {
    const route = createProtectedRoute('degraded')
    route.state.stability_state = ''
    route.state.temporary_traffic_kind = 'insufficient_samples'
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteState
        route={route}
        onProtectedStatusClick={() => {}}
      />
    )

    assert.ok(markup.includes('渠道禁用'))
    assert.equal(markup.includes('>统一探索采样<'), false)
    assert.ok(
      markup.includes('aria-label="解除 测试渠道 vip model-a 的统一探索采样"')
    )
  })

  test('keeps exploration traffic clickable while the route is available', () => {
    const route = createProtectedRoute('degraded')
    route.channel_status = 1
    route.enabled = true
    route.state.stability_state = ''
    route.state.temporary_traffic_kind = 'insufficient_samples'
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleRouteStatus
        route={route}
        placement={undefined}
        onClearProtection={() => {}}
      />
    )

    assert.ok(markup.includes('统一探索采样'))
    assert.ok(markup.includes('解除 测试渠道 vip model-a 的统一探索采样'))
    assert.match(
      markup,
      /<button[^>]*data-slot="badge"[^>]*aria-label="解除 测试渠道 vip model-a 的统一探索采样"[^>]*>/
    )
    assert.equal(markup.includes('路由禁用'), false)
  })
})
