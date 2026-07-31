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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderToStaticMarkup } from 'react-dom/server'

import { CHANNEL_STATUS } from '@/features/channels/constants'

import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../../types'
import { ChannelMonitorSmartScheduleBoard } from '../channel-monitor-smart-schedule-board'

const noop = () => {}

function createChannel(
  id: number,
  name: string,
  costRatio: number,
  remark: string
): ChannelMonitorItem {
  return {
    id,
    name,
    type: 1,
    status: 1,
    status_reason: '',
    priority: 100,
    weight: 100,
    base_url: 'https://example.com',
    models: 'model-fast',
    test_model: 'model-fast',
    groups: ['vip'],
    ratio: costRatio,
    previous_ratio: costRatio,
    cost_ratio: costRatio,
    previous_cost_ratio: costRatio,
    conversion_factor: 1,
    remark,
    channel_remark: remark,
    updated_time: 1_752_777_845,
    updated_by: 1,
    updated_by_username: '系统自动更新',
    last_fetch_status: 'succeeded',
    last_fetch_error: '',
    last_fetch_time: 1_752_777_845,
    consecutive_failures: 0,
    upstream_balance: 100,
    last_balance_time: 1_752_777_845,
    last_balance_error: '',
    today_cost_cny: 0,
    today_cost_configured: false,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    upstream: null,
  }
}

function createRoute(
  channelId: number,
  overrides: Partial<ChannelMonitorSmartScheduleRoute> = {}
): ChannelMonitorSmartScheduleRoute {
  const group = overrides.group ?? 'vip'
  const model = overrides.model ?? 'model-fast'
  return {
    channel_id: channelId,
    channel_name: `渠道 ${channelId}`,
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 100,
    group,
    model,
    enabled: true,
    priority: 100,
    weight: 100,
    ...overrides,
    state: {
      id: channelId,
      channel_id: channelId,
      group,
      model,
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: 0.9,
      last_schedule_priority: overrides.priority ?? 100,
      last_schedule_weight: overrides.weight ?? 100,
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
      ...overrides.state,
    },
  }
}

function createResult(): ChannelMonitorSmartScheduleRouteResult {
  return {
    generated_at: 1_752_777_845,
    range_minutes: 60,
    enabled: true,
    routes: [
      createRoute(1, {
        channel_name: '高速渠道',
        weight: 75,
        state: {
          probe_window_start: 1_752_700_000,
          probe_last_time: 1_752_777_845,
          probe_last_success: true,
          probe_sample_count: 5,
          probe_success_count: 5,
          probe_first_token_sample_count: 5,
          probe_average_first_token_ms: 420,
          probe_tps_sample_count: 5,
          probe_average_tps: 18.5,
        } as ChannelMonitorSmartScheduleRoute['state'],
      }),
      createRoute(2, {
        channel_name: '恢复中渠道',
        weight: 25,
        state: {
          stability_state: 'degraded',
          stability_since: 1_752_700_000,
          stability_until: 1_752_800_000,
          stability_saved_priority: 100,
          stability_saved_weight: 25,
        } as ChannelMonitorSmartScheduleRoute['state'],
      }),
      createRoute(3, {
        channel_name: '备用渠道',
        priority: 90,
        weight: 100,
      }),
      createRoute(4, {
        channel_name: '未参与渠道',
        state: {
          excluded: true,
        } as ChannelMonitorSmartScheduleRoute['state'],
      }),
      createRoute(5, {
        channel_name: '默认分组渠道',
        group: 'default',
        model: 'model-standard',
      }),
    ],
    performance_items: [],
    stability_metrics_available: true,
    stability_items: [],
  }
}

function renderBoard(
  options: {
    result?: ChannelMonitorSmartScheduleRouteResult
    groupRatios?: Readonly<Record<string, number>>
    channels?: ChannelMonitorItem[]
    groupPolicies?: ChannelMonitorSmartScheduleGroupPolicy[]
  } = {}
) {
  const queryClient = new QueryClient()
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <ChannelMonitorSmartScheduleBoard
        active
        result={options.result ?? createResult()}
        channels={
          options.channels ?? [
            createChannel(1, '高速渠道', 0.8, '主线路'),
            createChannel(2, '恢复中渠道', 1.1, '备用供应商'),
            createChannel(3, '备用渠道', 1.2, '低频备用'),
            createChannel(4, '未参与渠道', 1.3, '手动暂停'),
            createChannel(5, '默认分组渠道', 1, '默认线路'),
          ]
        }
        groupPolicies={
          options.groupPolicies ?? [
            {
              group: 'vip',
              strategy: 'smart',
              stability_enabled: true,
              jitter_enabled: true,
              jitter_tolerance_percent: 5,
              jitter_threshold_multiplier: 5,
              jitter_absolute_tolerance_seconds: 10,
              jitter_baseline_minutes: 60,
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
              models: [],
              model_order: [],
              min_samples: 5,
              degrade_stability_score: 90,
              recovery_stability_score: 95,
              fast_failure_penalty_percent: 40,
              fast_failure_seconds: 1,
              slow_failure_seconds: 10,
              cooldown_minutes: 30,
              sample_mode: 'traffic',
              exploration_traffic_percent: 3,
              probe_interval_minutes: 10,
            },
            {
              group: 'default',
              strategy: 'ratio',
              stability_enabled: false,
              jitter_enabled: true,
              jitter_tolerance_percent: 5,
              jitter_threshold_multiplier: 5,
              jitter_absolute_tolerance_seconds: 10,
              jitter_baseline_minutes: 60,
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
              apply_mode: 'weight',
              models: ['model-standard'],
              model_order: [],
              min_samples: 5,
              degrade_stability_score: 90,
              recovery_stability_score: 95,
              fast_failure_penalty_percent: 40,
              fast_failure_seconds: 1,
              slow_failure_seconds: 10,
              cooldown_minutes: 30,
              sample_mode: 'probe',
              exploration_traffic_percent: 3,
              probe_interval_minutes: 15,
            },
          ]
        }
        groupRatios={options.groupRatios ?? { default: 1, vip: 0.5 }}
        intervalMinutes={10}
        isLoading={false}
        isError={false}
        onOpenHistory={noop}
        onOpenSettings={noop}
      />
    </QueryClientProvider>
  )
}

describe('channel monitor smart schedule board', () => {
  test('shows the compact operating overview without bringing back the large route table', () => {
    const markup = renderBoard()

    assert.ok(markup.includes('智能调度运行状态'))
    assert.ok(markup.includes('已启用'))
    assert.ok(markup.includes('每 10 分钟调度'))
    assert.ok(markup.includes('调度池'))
    assert.ok(markup.includes('参与路由'))
    assert.ok(markup.includes('当前可调度'))
    assert.ok(markup.includes('当前调度状态'))
    assert.ok(markup.includes('稳定性降级 1'))
    assert.equal(markup.includes('最近调度失败 0'), false)
    assert.ok(markup.includes('执行记录'))
    assert.ok(markup.includes('调度设置'))
    assert.ok(markup.includes('立即调度'))
    assert.equal(markup.includes('<table'), false)
    assert.equal(markup.includes('全部路由'), false)
  })

  test('orders group navigation by ratio and shows the selected group as model pool cards', () => {
    const markup = renderBoard()

    assert.ok(markup.indexOf('vip') < markup.indexOf('default'))
    assert.match(markup, /vip[\s\S]*x0\.5[\s\S]*1 池/)
    assert.match(markup, /default[\s\S]*x1[\s\S]*1 池/)
    assert.ok(markup.includes('model-fast'))
    assert.ok(markup.includes('测试 vip model-fast 调度池模型'))
    assert.ok(markup.includes('流入层 P100 · 2 条'))
    assert.ok(markup.includes('主线路'))
    assert.ok(markup.includes('成本倍率'))
    assert.ok(markup.includes('探索流量 3%'))
    assert.ok(markup.includes('预计流量'))
    assert.ok(markup.includes('最终得分'))
    assert.ok(markup.includes('75.0%'))
    assert.ok(markup.includes('25.0%'))
    assert.ok(markup.includes('transition-[width]'))
    assert.ok(markup.includes('探测成功'))
    assert.ok(markup.includes('最近探测'))
    assert.ok(markup.includes('2xl:grid-cols-2'))
    assert.ok(markup.indexOf('主线路') < markup.indexOf('备用供应商'))
  })

  test('orders pool channels by enabled status before cost ratio', () => {
    const enabledChannel = createChannel(11, '启用高倍率', 1.5, '启用线路')
    const disabledChannel = {
      ...createChannel(12, '禁用低倍率', 0.5, '禁用线路'),
      status: CHANNEL_STATUS.MANUAL_DISABLED,
    }
    const result = createResult()
    result.routes = [
      createRoute(enabledChannel.id, {
        channel_name: enabledChannel.name,
      }),
      createRoute(disabledChannel.id, {
        channel_name: disabledChannel.name,
        channel_status: CHANNEL_STATUS.MANUAL_DISABLED,
        state: {
          stability_state: 'degraded',
        } as ChannelMonitorSmartScheduleRoute['state'],
      }),
    ]

    const markup = renderBoard({
      result,
      channels: [disabledChannel, enabledChannel],
    })

    assert.ok(markup.indexOf('启用线路') < markup.indexOf('禁用线路'))
  })

  test('orders model pool cards by the configured order before the name fallback', () => {
    const result = createResult()
    result.routes = [
      createRoute(11, { channel_name: 'Alpha 渠道', model: 'model-alpha' }),
      createRoute(12, { channel_name: 'Beta 渠道', model: 'model-beta' }),
      createRoute(13, { channel_name: 'Gamma 渠道', model: 'model-gamma' }),
      createRoute(14, { channel_name: 'Zeta 渠道', model: 'model-zeta' }),
    ]
    const groupPolicy = {
      group: 'vip',
      strategy: 'smart',
      stability_enabled: true,
      jitter_enabled: true,
      jitter_tolerance_percent: 5,
      jitter_threshold_multiplier: 5,
      jitter_absolute_tolerance_seconds: 10,
      jitter_baseline_minutes: 60,
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
      models: [],
      model_order: ['model-zeta', 'model-beta'],
      min_samples: 5,
      degrade_stability_score: 90,
      recovery_stability_score: 95,
      fast_failure_penalty_percent: 40,
      fast_failure_seconds: 1,
      slow_failure_seconds: 10,
      cooldown_minutes: 30,
      sample_mode: 'traffic',
      exploration_traffic_percent: 3,
      probe_interval_minutes: 10,
    } satisfies ChannelMonitorSmartScheduleGroupPolicy

    const markup = renderBoard({ result, groupPolicies: [groupPolicy] })

    assert.ok(markup.indexOf('model-zeta') < markup.indexOf('model-beta'))
    assert.ok(markup.indexOf('model-beta') < markup.indexOf('model-alpha'))
    assert.ok(markup.indexOf('model-alpha') < markup.indexOf('model-gamma'))
  })

  test('opens the lowest-ratio group without prioritizing a group that needs attention', () => {
    const markup = renderBoard({
      groupRatios: { default: 0.1, vip: 0.5 },
    })

    assert.ok(markup.indexOf('default') < markup.indexOf('vip'))
    assert.match(
      markup,
      /<button[^>]*aria-pressed="true"[^>]*>[\s\S]*?<span[^>]*>default<\/span>/
    )
    assert.ok(markup.includes('model-standard'))
    assert.ok(markup.includes('每 15 分钟文本探测'))
    assert.equal(markup.includes('model-fast'), false)
  })

  test('excludes routes outside explicitly configured group and model policies', () => {
    const result = createResult()
    result.routes.push(
      createRoute(6, {
        channel_name: '未配置分组渠道',
        group: 'unconfigured',
        model: 'model-fast',
      }),
      createRoute(7, {
        channel_name: '未配置模型渠道',
        group: 'default',
        model: 'model-other',
      })
    )

    const markup = renderBoard({ result })

    assert.equal(markup.includes('未配置分组渠道'), false)
    assert.equal(markup.includes('未配置模型渠道'), false)
  })

  test('hides stale protection information while smart scheduling is disabled', () => {
    const result = createResult()
    result.enabled = false

    const markup = renderBoard({ result })

    assert.ok(markup.includes('智能调度尚未启用'))
    assert.equal(markup.includes('当前保护与异常'), false)
    assert.equal(markup.includes('稳定性降级 1'), false)
  })

  test('keeps protection recovery clickable and folds secondary routes behind one entry', () => {
    const markup = renderBoard()

    assert.match(
      markup,
      /<button[^>]*data-slot="badge"[^>]*aria-label="解除 恢复中渠道 vip model-fast 的稳定性降级保护"[^>]*>/
    )
    assert.ok(markup.includes('备用与未参与路由'))
    assert.ok(markup.includes('第一备用 1'))
    assert.ok(markup.includes('未参与 1'))
    assert.match(markup, /data-slot="collapsible-trigger"/)
  })

  test('exposes fixed-primary expiry, editing, and clear actions on the route', () => {
    const result = createResult()
    result.routes[0] = createRoute(1, {
      channel_name: '高速渠道',
      state: {
        manual_primary_until: 1_752_800_000,
        manual_primary_allow_stability_degrade: true,
      } as ChannelMonitorSmartScheduleRoute['state'],
    })

    const markup = renderBoard({ result })

    assert.ok(markup.includes('管理员固定至'))
    assert.ok(markup.includes('允许稳定性降级'))
    assert.ok(markup.includes('重新设置 高速渠道 的固定时长'))
    assert.ok(markup.includes('解除 高速渠道 的主渠道固定'))
  })
})
