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

import type { ChannelMonitorSmartScheduleRoute } from '../../types'
import {
  channelMonitorSmartScheduleRouteParticipates,
  channelMonitorSmartScheduleRouteKey,
  compareChannelMonitorSmartScheduleGroupsByRatio,
  compareChannelMonitorSmartScheduleRoutesByAttention,
  compareChannelMonitorSmartScheduleRoutesByPool,
  getChannelMonitorSmartScheduleDisplayOptions,
  getChannelMonitorSmartSchedulePoolStatus,
  getChannelMonitorSmartScheduleRouteDisplayStatus,
  isChannelMonitorSmartScheduleResultStale,
  placeChannelMonitorSmartScheduleRoutes,
  summarizeChannelMonitorSmartScheduleChannel,
  summarizeChannelMonitorSmartScheduleOverview,
  summarizeChannelMonitorSmartSchedulePools,
} from '../smart-schedule-summary'

test('marks a manual smart schedule snapshot stale after a fixed ten minutes', () => {
  const generatedAt = 1_700_000_000

  assert.equal(
    isChannelMonitorSmartScheduleResultStale(generatedAt, generatedAt + 600),
    false
  )
  assert.equal(
    isChannelMonitorSmartScheduleResultStale(generatedAt, generatedAt + 601),
    true
  )
  assert.equal(isChannelMonitorSmartScheduleResultStale(0, generatedAt), false)
})

const normalPool = {
  routeCount: 3,
  participatingCount: 3,
  activeCount: 3,
  pausedCount: 0,
  degradedCount: 0,
  probingCount: 0,
  insufficientSampleCount: 0,
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
    shared_samples: {
      id: 0,
      channel_id: channelId,
      model,
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
      runtime_protection_until: 0,
      base_rank: 1,
      base_priority: priority,
      base_weight: weight,
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

  test('prioritizes protection states after participation is configured', () => {
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        participatingCount: 1,
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
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        insufficientSampleCount: 1,
      }),
      '统一采样'
    )
  })

  test('reports full and partial route traffic pauses before stale route states', () => {
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        routeCount: 1,
        participatingCount: 1,
        activeCount: 0,
        pausedCount: 1,
        degradedCount: 1,
      }),
      '流量已暂停'
    )
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus({
        ...normalPool,
        activeCount: 2,
        pausedCount: 1,
      }),
      '部分流量暂停'
    )
  })

  test('reports break-even fallback takeover only when it is the actual top layer', () => {
    const normal = createRoute(1, 'vip', 'model-a', 2, 1000)
    const fallback = createRoute(2, 'vip', 'model-a', 1, 1000)
    fallback.economic_role = 'break_even_fallback'

    const normalSummary = summarizeChannelMonitorSmartSchedulePools([
      normal,
      fallback,
    ])[0]
    assert.equal(normalSummary.breakEvenFallbackCount, 1)
    assert.equal(normalSummary.breakEvenFallbackTakingOver, false)
    assert.equal(
      placeChannelMonitorSmartScheduleRoutes([normal, fallback]).get(
        channelMonitorSmartScheduleRouteKey(fallback)
      )?.estimatedShare,
      null
    )

    const fallbackOnlySummary = summarizeChannelMonitorSmartSchedulePools([
      fallback,
    ])[0]
    assert.equal(fallbackOnlySummary.breakEvenFallbackTakingOver, true)
    assert.equal(
      getChannelMonitorSmartSchedulePoolStatus(fallbackOnlySummary),
      '保本兜底接管'
    )
  })

  test('ignores stale alerts from unavailable routes in every summary level', () => {
    const channelDisabled = createRoute(1, 'vip', 'model-a', 100, 100)
    channelDisabled.channel_status = 2
    channelDisabled.state.stability_state = 'degraded'
    channelDisabled.state.temporary_traffic_kind = 'insufficient_samples'
    channelDisabled.state.last_schedule_status = 'failed'
    const routeDisabled = createRoute(1, 'vip', 'model-b', 90, 80)
    routeDisabled.enabled = false
    routeDisabled.state.stability_state = 'probing'
    routeDisabled.state.temporary_traffic_kind = 'adaptive_sampling'
    routeDisabled.state.last_schedule_status = 'failed'
    const routes = [channelDisabled, routeDisabled]

    const channel = summarizeChannelMonitorSmartScheduleChannel(routes)
    const pools = summarizeChannelMonitorSmartSchedulePools(routes)
    const overview = summarizeChannelMonitorSmartScheduleOverview(routes)

    requireSummaryHasNoActiveAlerts(channel)
    assert.equal(channel.groups[0]?.degradedCount, 0)
    assert.equal(channel.groups[0]?.probingCount, 0)
    assert.equal(channel.groups[0]?.insufficientSampleCount, 0)
    assert.equal(pools.length, 2)
    for (const pool of pools) {
      requireSummaryHasNoActiveAlerts(pool)
      assert.equal(
        getChannelMonitorSmartSchedulePoolStatus(pool),
        '当前不可调度'
      )
    }
    requireSummaryHasNoActiveAlerts(overview)
    assert.equal(overview.healthyPoolCount, 0)
  })

  test('ignores stale pause and protection states from nonparticipating routes', () => {
    const excluded = createRoute(1, 'vip', 'model-a', 100, 100)
    excluded.state.excluded = true
    excluded.traffic_paused_until = 4_102_444_800
    excluded.state.stability_state = 'degraded'
    excluded.state.temporary_traffic_kind = 'insufficient_samples'
    excluded.state.last_schedule_status = 'failed'

    const channel = summarizeChannelMonitorSmartScheduleChannel([excluded])
    const pool = summarizeChannelMonitorSmartSchedulePools([excluded])[0]
    const overview = summarizeChannelMonitorSmartScheduleOverview([excluded])

    requireSummaryHasNoActiveAlerts(channel)
    assert.equal(channel.pausedCount, 0)
    requireSummaryHasNoActiveAlerts(pool)
    assert.equal(pool.pausedCount, 0)
    assert.equal(getChannelMonitorSmartSchedulePoolStatus(pool), '未参与调度')
    requireSummaryHasNoActiveAlerts(overview)
    assert.equal(overview.pausedCount, 0)
    assert.equal(overview.healthyPoolCount, 0)
  })
})

describe('smart schedule route placement', () => {
  test('uses the current-window winner while preserving the historical winner', () => {
    const currentWinner = createRoute(14, 'default', 'model-a', 3, 1000)
    const actualPrimary = createRoute(22, 'default', 'model-a', 4, 1000)
    currentWinner.state.last_schedule_score_details = {
      decision: {
        apply_mode: 'priority_weight',
        current_primary_channel_id: 22,
        raw_winner_channel_id: 22,
        selected_primary_channel_id: 22,
        actual_primary_channel_id: 22,
        selected_primary: true,
        manual_primary_channel_id: 0,
        base_rank: 2,
        base_priority: 3,
        base_weight: 1000,
        applied_priority: 3,
        applied_weight: 1000,
        actual_highest_priority: 4,
        actual_top_layer_channel_ids: [22],
        temporary_traffic_kind: '',
        temporary_traffic_target_percent: 0,
        switch_threshold_percent: 3,
        primary_traffic_percent: 100,
        force_reset: false,
        manual_primary: false,
        selection_reason: '上一轮 a-2 得分最高',
        adjustment_reason: '',
        reason: '上一轮 a-2 得分最高',
      },
    } as ChannelMonitorSmartScheduleRoute['state']['last_schedule_score_details']
    const historicalDecision =
      currentWinner.state.last_schedule_score_details?.decision
    assert.ok(historicalDecision)
    currentWinner.current_window_score_details = {
      decision: {
        ...historicalDecision,
        raw_winner_channel_id: 14,
        selected_primary_channel_id: 22,
        selected_primary: false,
        selection_reason: 'md 得分更高，但切换确认健康比例不足',
        reason: 'md 得分更高，但切换确认健康比例不足',
      },
    } as ChannelMonitorSmartScheduleRoute['current_window_score_details']

    const placements = placeChannelMonitorSmartScheduleRoutes([
      currentWinner,
      actualPrimary,
    ])
    const summary = summarizeChannelMonitorSmartSchedulePools([
      currentWinner,
      actualPrimary,
    ])[0]

    assert.equal(summary?.scoringWinnerChannelId, 14)
    assert.equal(summary?.historicalScoringWinnerChannelId, 22)
    assert.equal(summary?.scoringWinnerSource, 'current_window')
    assert.equal(summary?.actualPrimaryChannelId, 22)
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(currentWinner))
        ?.isScoringWinner,
      true
    )
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(actualPrimary))
        ?.isActualPrimary,
      true
    )
  })

  test('keeps a degraded P0/W0 route out of current availability and traffic', () => {
    const degraded = createRoute(1, 'vip', 'model-a', 0, 0)
    degraded.state.stability_state = 'degraded'

    const channel = summarizeChannelMonitorSmartScheduleChannel([degraded])
    const pool = summarizeChannelMonitorSmartSchedulePools([degraded])[0]
    const overview = summarizeChannelMonitorSmartScheduleOverview([degraded])
    const placement = placeChannelMonitorSmartScheduleRoutes([degraded]).get(
      channelMonitorSmartScheduleRouteKey(degraded)
    )

    assert.equal(channel?.activeCount, 0)
    assert.equal(channel?.degradedCount, 1)
    assert.equal(pool.activeCount, 0)
    assert.equal(pool.degradedCount, 1)
    assert.equal(pool.actualHighestPriority, null)
    assert.deepEqual(pool.actualTopLayerChannelIds, [])
    assert.equal(getChannelMonitorSmartSchedulePoolStatus(pool), '稳定性降级')
    assert.equal(placement?.estimatedShare, null)
    assert.equal(placement?.isActualTopLayer, false)
    assert.equal(overview.activeCount, 0)
    assert.equal(overview.degradedCount, 1)
    assert.equal(overview.healthyPoolCount, 0)
  })

  test('removes a paused route from the actual highest layer and gives it zero traffic', () => {
    const paused = createRoute(1, 'vip', 'model-a', 100, 80)
    paused.traffic_paused_until = 4_102_444_800
    paused.state.stability_state = 'degraded'
    paused.state.temporary_traffic_kind = 'insufficient_samples'
    const active = createRoute(2, 'vip', 'model-a', 90, 20)

    const routes = [paused, active]
    const placements = placeChannelMonitorSmartScheduleRoutes(routes)
    const pausedPlacement = placements.get(
      channelMonitorSmartScheduleRouteKey(paused)
    )
    const activePlacement = placements.get(
      channelMonitorSmartScheduleRouteKey(active)
    )
    const summary = summarizeChannelMonitorSmartSchedulePools(routes)[0]

    assert.equal(pausedPlacement?.role, 'paused')
    assert.equal(pausedPlacement?.estimatedShare, 0)
    assert.equal(pausedPlacement?.isActualTopLayer, false)
    assert.equal(
      getChannelMonitorSmartScheduleRouteDisplayStatus(paused, pausedPlacement),
      'paused'
    )
    assert.equal(activePlacement?.role, 'primary')
    assert.equal(activePlacement?.estimatedShare, 1)
    assert.equal(summary.activeCount, 1)
    assert.equal(summary.pausedCount, 1)
    assert.equal(summary.degradedCount, 0)
    assert.equal(summary.insufficientSampleCount, 0)
    assert.equal(summary.actualHighestPriority, 90)
    assert.deepEqual(summary.actualTopLayerChannelIds, [2])
  })

  test('moves expected traffic away from a rate-limited route while another route is available', () => {
    const rateLimited = createRoute(1, 'vip', 'model-a', 100, 80)
    rateLimited.rate_limit_cooldown_until = 4_102_444_800
    const active = createRoute(2, 'vip', 'model-a', 90, 20)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      rateLimited,
      active,
    ])
    const rateLimitedPlacement = placements.get(
      channelMonitorSmartScheduleRouteKey(rateLimited)
    )
    const activePlacement = placements.get(
      channelMonitorSmartScheduleRouteKey(active)
    )

    assert.equal(rateLimitedPlacement?.role, 'rate_limited')
    assert.equal(rateLimitedPlacement?.estimatedShare, 0)
    assert.equal(rateLimitedPlacement?.isActualTopLayer, false)
    assert.equal(
      getChannelMonitorSmartScheduleRouteDisplayStatus(
        rateLimited,
        rateLimitedPlacement
      ),
      'rate_limited'
    )
    assert.equal(activePlacement?.role, 'primary')
    assert.equal(activePlacement?.estimatedShare, 1)
    assert.equal(activePlacement?.actualHighestPriority, 90)
  })

  test('keeps a rate-limited route as the estimated final fallback when it is the only route', () => {
    const rateLimited = createRoute(1, 'vip', 'model-a', 100, 80)
    rateLimited.rate_limit_cooldown_until = 4_102_444_800

    const placement = placeChannelMonitorSmartScheduleRoutes([rateLimited]).get(
      channelMonitorSmartScheduleRouteKey(rateLimited)
    )

    assert.equal(placement?.role, 'rate_limited')
    assert.equal(placement?.estimatedShare, 1)
    assert.equal(placement?.isActualPrimary, true)
    assert.equal(placement?.isActualTopLayer, true)
  })

  test('excludes nonparticipating routing from the actual highest layer', () => {
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
    assert.equal(
      placements.get(channelMonitorSmartScheduleRouteKey(uninitialized))
        ?.estimatedShare,
      0
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(active)),
      {
        role: 'primary',
        estimatedShare: 1,
        topPriority: 80,
        candidateCount: 1,
        actualPrimaryChannelId: 2,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 80,
        actualTopLayerChannelIds: [2],
        isActualPrimary: true,
        isScoringWinner: false,
        isActualTopLayer: true,
      }
    )
  })

  test('calculates traffic only inside the highest priority layer of each group-model pool', () => {
    const primary = createRoute(1, 'vip', 'model-a', 100, 80)
    const candidate = createRoute(2, 'vip', 'model-a', 100, 20)
    const firstBackup = createRoute(3, 'vip', 'model-a', 90, 100)
    const standby = createRoute(4, 'vip', 'model-a', 80, 100)

    const placements = placeChannelMonitorSmartScheduleRoutes([
      standby,
      firstBackup,
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
        actualPrimaryChannelId: 1,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 100,
        actualTopLayerChannelIds: [1, 2],
        isActualPrimary: true,
        isScoringWinner: false,
        isActualTopLayer: true,
      }
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(candidate)),
      {
        role: 'candidate',
        estimatedShare: 0.2,
        topPriority: 100,
        candidateCount: 2,
        actualPrimaryChannelId: 1,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 100,
        actualTopLayerChannelIds: [1, 2],
        isActualPrimary: false,
        isScoringWinner: false,
        isActualTopLayer: true,
      }
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(firstBackup)),
      {
        role: 'backup',
        estimatedShare: null,
        topPriority: 100,
        candidateCount: 2,
        actualPrimaryChannelId: 1,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 100,
        actualTopLayerChannelIds: [1, 2],
        isActualPrimary: false,
        isScoringWinner: false,
        isActualTopLayer: false,
      }
    )
    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(standby)),
      {
        role: 'backup',
        estimatedShare: null,
        topPriority: 100,
        candidateCount: 2,
        actualPrimaryChannelId: 1,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 100,
        actualTopLayerChannelIds: [1, 2],
        isActualPrimary: false,
        isScoringWinner: false,
        isActualTopLayer: false,
      }
    )
  })

  test('ignores an excluded stale decision after a fixed primary takes over', () => {
    const previousPrimary = createRoute(1, 'vip', 'model-a', 100, 1000)
    previousPrimary.state.excluded = true
    const fixedPrimary = createRoute(2, 'vip', 'model-a', 101, 1000)
    fixedPrimary.state.manual_primary_until = 1_900_000_000
    previousPrimary.state.last_schedule_score_details = {
      decision: {
        apply_mode: 'priority_weight',
        current_primary_channel_id: 1,
        raw_winner_channel_id: 1,
        selected_primary_channel_id: 1,
        actual_primary_channel_id: 1,
        selected_primary: true,
        manual_primary_channel_id: 0,
        base_rank: 1,
        base_priority: 100,
        base_weight: 1000,
        applied_priority: 100,
        applied_weight: 1000,
        actual_highest_priority: 100,
        actual_top_layer_channel_ids: [1],
        temporary_traffic_kind: '',
        temporary_traffic_target_percent: 0,
        switch_threshold_percent: 3,
        primary_traffic_percent: 90,
        force_reset: false,
        manual_primary: false,
        selection_reason: '上一轮调度结果',
        adjustment_reason: '',
        reason: '上一轮调度结果',
      },
    } as ChannelMonitorSmartScheduleRoute['state']['last_schedule_score_details']

    const placements = placeChannelMonitorSmartScheduleRoutes([
      previousPrimary,
      fixedPrimary,
    ])

    assert.deepEqual(
      placements.get(channelMonitorSmartScheduleRouteKey(fixedPrimary)),
      {
        role: 'primary',
        estimatedShare: 1,
        topPriority: 101,
        candidateCount: 1,
        actualPrimaryChannelId: 2,
        scoringWinnerChannelId: 0,
        actualHighestPriority: 101,
        actualTopLayerChannelIds: [2],
        isActualPrimary: true,
        isScoringWinner: false,
        isActualTopLayer: true,
      }
    )
    const summaries = summarizeChannelMonitorSmartSchedulePools([
      previousPrimary,
      fixedPrimary,
    ])
    assert.equal(summaries[0]?.actualPrimaryChannelId, 2)
    assert.equal(summaries[0]?.scoringWinnerChannelId, 0)
    assert.equal(summaries[0]?.actualHighestPriority, 101)
    assert.deepEqual(summaries[0]?.actualTopLayerChannelIds, [2])
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

  test('shows an excluded route as zero traffic while keeping disabled routes unavailable', () => {
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
      placements.get(channelMonitorSmartScheduleRouteKey(excluded))
        ?.estimatedShare,
      0
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
  test('orders display options by group ratio then configured model order', () => {
    const options = getChannelMonitorSmartScheduleDisplayOptions(
      [
        createRoute(1, 'default', 'model-b', 100, 100),
        createRoute(2, 'vip', 'model-c', 100, 100),
        createRoute(3, 'vip', 'model-a', 100, 100),
        createRoute(4, 'vip', 'model-a', 90, 50),
        createRoute(5, 'premium', 'model-a', 100, 100),
      ],
      { vip: 0.5, default: 1, premium: 1.5 },
      new Map([['vip', ['model-c', 'model-a']]])
    )

    assert.deepEqual(
      options.map((option) => ({
        group: option.group,
        model: option.model,
        label: option.label,
      })),
      [
        { group: 'vip', model: 'model-c', label: 'vip / model-c' },
        { group: 'vip', model: 'model-a', label: 'vip / model-a' },
        { group: 'default', model: 'model-b', label: 'default / model-b' },
        { group: 'premium', model: 'model-a', label: 'premium / model-a' },
      ]
    )
  })

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
    unavailable.state.stability_state = 'degraded'
    const excluded = createRoute(8, 'vip', 'model-a', 100, 100)
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

function requireSummaryHasNoActiveAlerts(
  summary: {
    activeCount: number
    degradedCount: number
    probingCount: number
    insufficientSampleCount: number
    failedCount: number
  } | null
): asserts summary {
  assert.ok(summary)
  assert.equal(summary.activeCount, 0)
  assert.equal(summary.degradedCount, 0)
  assert.equal(summary.probingCount, 0)
  assert.equal(summary.insufficientSampleCount, 0)
  assert.equal(summary.failedCount, 0)
}
