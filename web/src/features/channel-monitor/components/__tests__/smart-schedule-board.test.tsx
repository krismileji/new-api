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

const ADAPTIVE_SAMPLING_POLICY = {
  adaptive_sampling_enabled: true,
  adaptive_sampling_base_percent: 3,
  adaptive_sampling_max_percent: 30,
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
    current_window_score: 0.75,
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
      runtime_protection_until: 0,
      base_rank: 1,
      base_priority: overrides.priority ?? 100,
      base_weight: overrides.weight ?? 100,
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
      ...overrides.state,
    },
  }
}

function createResult(): ChannelMonitorSmartScheduleRouteResult {
  return {
    generated_at: 1_752_777_845,
    performance_window_minutes: 60,
    stability_window_minutes: 120,
    sample_scope: 'channel_model',
    enabled: true,
    metric_coverage: {
      aggregation_enabled: true,
      aggregated_from: 1_752_770_400,
      aggregated_through: 1_752_777_840,
      performance_window_start: 1_752_774_240,
      stability_window_start: 1_752_770_640,
      performance_window_complete: true,
      stability_window_complete: true,
      configured_retention_days: 120,
      required_retention_minutes: 120,
      configured_retention_sufficient: true,
    },
    routes: [
      createRoute(1, {
        channel_name: '高速渠道',
        weight: 75,
        shared_samples: {
          id: 1,
          channel_id: 1,
          model: 'model-fast',
          window_start: 1_752_700_000,
          observation_since: 0,
          recovery_success_count: 0,
          recovery_success_at: 0,
          last_time: 1_752_777_845,
          last_success: true,
          last_error: '',
          sample_count: 5,
          success_count: 5,
          failure_duration_sample_count: 0,
          average_failure_duration_ms: null,
          first_token_sample_count: 5,
          average_first_token_ms: 420,
          tps_sample_count: 5,
          average_tps: 18.5,
        },
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
        priority: 0,
        weight: 0,
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
    sample_items: [
      {
        channel_id: 1,
        model: 'model-fast',
        performance_window: {
          id: 1,
          channel_id: 1,
          model: 'model-fast',
          window_start: 1_752_700_000,
          observation_since: 0,
          recovery_success_count: 0,
          recovery_success_at: 0,
          last_time: 1_752_777_845,
          last_success: true,
          last_error: '',
          sample_count: 5,
          success_count: 5,
          failure_duration_sample_count: 0,
          average_failure_duration_ms: null,
          first_token_sample_count: 5,
          average_first_token_ms: 420,
          tps_sample_count: 5,
          average_tps: 18.5,
        },
        stability_window: {
          id: 1,
          channel_id: 1,
          model: 'model-fast',
          window_start: 1_752_750_000,
          observation_since: 0,
          recovery_success_count: 0,
          recovery_success_at: 0,
          last_time: 1_752_777_845,
          last_success: true,
          last_error: '',
          sample_count: 2,
          success_count: 2,
          failure_duration_sample_count: 0,
          average_failure_duration_ms: null,
          first_token_sample_count: 2,
          average_first_token_ms: 400,
          tps_sample_count: 2,
          average_tps: 20,
        },
      },
    ],
    business_performance_items: [
      {
        channel_id: 1,
        group: 'vip',
        model: 'model-fast',
        group_count: 2,
        sample_count: 15,
        first_token_sample_count: 13,
        first_token_duration_sample_count: 13,
        tps_sample_count: 11,
        average_first_token_ms: 405,
        first_token_p50_ms: 375,
        first_token_p95_ms: 750,
        winsorized_average_first_token_ms: 400,
        average_tps: 27,
        last_used_time: 1_752_777_800,
      },
    ],
    performance_items: [
      {
        channel_id: 1,
        group: 'vip',
        model: 'model-fast',
        group_count: 2,
        sample_count: 20,
        first_token_sample_count: 18,
        first_token_duration_sample_count: 18,
        tps_sample_count: 16,
        average_first_token_ms: 410,
        first_token_p50_ms: 380,
        first_token_p95_ms: 760,
        winsorized_average_first_token_ms: 405,
        average_tps: 24.5,
        last_used_time: 1_752_777_845,
      },
    ],
    stability_metrics_available: true,
    stability_items: [
      {
        channel_id: 1,
        group: 'vip',
        model: 'model-fast',
        group_count: 2,
        success_count: 24,
        failure_count: 1,
        final_failure_count: 0,
        retry_failure_count: 1,
        sample_count: 25,
        success_rate: 0.96,
        stability_score: 0.98,
        average_retry_failure_duration_ms: 500,
        retry_failure_duration_buckets: [],
        jitter_available: true,
        first_token_p50_ms: 380,
        first_token_p95_ms: 760,
        jitter_threshold_ms: 11_750,
        jitter_sample_count: 18,
        jitter_slow_count: 0,
        jitter_allowed_count: 1,
        jitter_penalty: 0,
      },
    ],
  }
}

function renderBoard(
  options: {
    result?: ChannelMonitorSmartScheduleRouteResult
    groupRatios?: Readonly<Record<string, number>>
    channels?: ChannelMonitorItem[]
    groupPolicies?: ChannelMonitorSmartScheduleGroupPolicy[]
    selection?: { group: string; model: string }
    isError?: boolean
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
              ...ADAPTIVE_SAMPLING_POLICY,
              group: 'vip',
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
              models: [],
              model_order: [],
              min_samples: 5,
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
            },
            {
              ...ADAPTIVE_SAMPLING_POLICY,
              group: 'default',
              strategy: 'ratio',
              stability_enabled: false,
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
              apply_mode: 'weight',
              adaptive_sampling_enabled: false,
              models: ['model-standard'],
              model_order: [],
              min_samples: 5,
              recovery_stability_score: 95,
              fast_failure_penalty_percent: 40,
              fast_failure_seconds: 1,
              slow_failure_seconds: 10,
              cooldown_minutes: 30,
              sample_mode: 'probe',
              sampling_order: 'ratio',
              exploration_traffic_percent: 3,
              exploration_max_prompt_tokens: 50_000,
              probe_interval_minutes: 15,
            },
          ]
        }
        groupRatios={options.groupRatios ?? { default: 1, vip: 0.5 }}
        intervalMinutes={10}
        isLoading={false}
        isError={options.isError ?? false}
        onOpenHistory={noop}
        onOpenSettings={noop}
        selection={options.selection}
      />
    </QueryClientProvider>
  )
}

describe('channel monitor smart schedule board', () => {
  test('shows the compact operating overview with a dense route table', () => {
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
    assert.ok(markup.includes('智能调度记录'))
    assert.ok(markup.includes('调度设置'))
    assert.ok(markup.includes('立即调度'))
    assert.ok(markup.includes('<table'))
    assert.ok(markup.includes('data-schedule-route-list="desktop-table"'))
    assert.equal(markup.includes('全部路由'), false)
  })

  test('orders group navigation by ratio and shows only the selected model pool', () => {
    const markup = renderBoard()

    assert.ok(markup.indexOf('vip') < markup.indexOf('default'))
    assert.match(markup, /vip[\s\S]*x0\.5[\s\S]*1 池/)
    assert.match(markup, /default[\s\S]*x1[\s\S]*1 池/)
    assert.ok(markup.includes('1. 选择分组'))
    assert.ok(markup.includes('选择模型'))
    assert.ok(markup.includes('· vip'))
    assert.ok(markup.includes('aria-label="vip 分组下的智能调度模型"'))
    assert.ok(markup.includes('model-fast'))
    assert.equal(markup.includes('model-standard'), false)
    assert.equal(markup.includes('测试 vip model-fast 调度池模型'), false)
    assert.ok(markup.includes('实际最高层 P100 · 2 条'))
    assert.ok(markup.includes('评分第一'))
    assert.ok(markup.includes('实际主渠道'))
    assert.ok(markup.includes('基础排名'))
    assert.ok(markup.includes('基础 P / W'))
    assert.ok(markup.includes('当前 P / W'))
    assert.ok(markup.includes('当前采样'))
    assert.ok(markup.includes('采样顺序 基础 P/W'))
    assert.ok(markup.includes('未切换原因'))
    assert.ok(markup.includes('主线路'))
    assert.ok(markup.includes('成本倍率'))
    assert.ok(markup.includes('探索流量 3%'))
    assert.ok(markup.includes('≤ 50K Token'))
    assert.ok(markup.includes('预计流量'))
    assert.ok(markup.includes('title="当前 75.0 · 最近 90.0"'))
    assert.ok(markup.includes('75.0%'))
    assert.ok(markup.includes('25.0%'))
    assert.ok(markup.includes('transition-[width]'))
    assert.ok(markup.includes('窗口数据 / 测试样本'))
    assert.ok(markup.includes('稳定 25 次 · 稳定分 98.0'))
    assert.ok(markup.includes('性能 20 次（业务 15 + 测试 5）'))
    assert.ok(markup.includes('P50 380 ms'))
    assert.ok(markup.includes('TPS 24.50'))
    assert.ok(markup.includes('其中测试/探测 2 次'))
    assert.ok(markup.includes('搜索渠道名称、ID 或备注'))
    assert.ok(markup.includes('按状态筛选'))
    assert.ok(markup.includes('按渠道排序'))
    assert.ok(markup.includes('查看 高速渠道 的调度详情'))
    assert.ok(markup.indexOf('主线路') < markup.indexOf('备用供应商'))
  })

  test('restores a persisted group and model selection', () => {
    const markup = renderBoard({
      selection: { group: 'default', model: 'model-standard' },
    })

    assert.ok(markup.includes('· default'))
    assert.ok(markup.includes('model-standard'))
    assert.ok(markup.includes('默认分组渠道'))
    assert.equal(markup.includes('高速渠道'), false)
  })

  test('keeps the last schedule result visible after a background refresh fails', () => {
    const markup = renderBoard({ isError: true })

    assert.ok(markup.includes('刷新失败，显示上次结果'))
    assert.ok(markup.includes('高速渠道'))
    assert.equal(markup.includes('智能调度加载失败'), false)
  })

  test('warns when the persisted minute coverage does not fill a schedule window', () => {
    const result = createResult()
    assert.ok(result.metric_coverage)
    result.metric_coverage = {
      ...result.metric_coverage,
      aggregated_from: 1_752_776_000,
      performance_window_complete: false,
      stability_window_complete: true,
      configured_retention_days: 1,
      configured_retention_sufficient: false,
    }

    const markup = renderBoard({ result })

    assert.ok(markup.includes('调度窗口数据尚未覆盖完整'))
    assert.ok(markup.includes('性能窗口覆盖不足'))
    assert.equal(markup.includes('稳定性窗口覆盖不足'), false)
    assert.ok(markup.includes('后台正在分批补齐分钟汇总'))
    assert.ok(markup.includes('保留配置短于最长调度窗口'))
  })

  test('keeps dozens of routes in a fixed-header scroll region', () => {
    const result = createResult()
    result.routes = Array.from({ length: 36 }, (_, index) =>
      createRoute(index + 1, {
        channel_name: `生产渠道 ${String(index + 1).padStart(2, '0')}`,
        weight: 100 - index,
      })
    )
    const channels = result.routes.map((route, index) =>
      createChannel(
        route.channel_id,
        route.channel_name,
        0.5 + index / 100,
        `线路备注 ${index + 1}`
      )
    )

    const markup = renderBoard({ result, channels })

    assert.ok(markup.includes('data-schedule-scroll-region="true"'))
    assert.ok(markup.includes('sticky top-0'))
    assert.equal((markup.match(/data-schedule-route-row=/g) ?? []).length, 36)
    assert.ok(markup.includes('共 36 条渠道'))
    assert.equal(markup.includes('备用与未参与路由'), false)
  })

  test('uses ascending cost ratio as the default pool channel order', () => {
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

    assert.ok(markup.indexOf('禁用线路') < markup.indexOf('启用线路'))
  })

  test('keeps the default pool channel order ascending by cost ratio', () => {
    const result = createResult()
    result.routes = [
      createRoute(11, { channel_name: '主渠道', priority: 100 }),
      createRoute(12, { channel_name: '低成本第 3 名', priority: 80 }),
      createRoute(13, { channel_name: '第 2 名渠道', priority: 90 }),
    ]

    const markup = renderBoard({
      result,
      channels: [
        createChannel(11, '主渠道', 1, '主线路'),
        createChannel(12, '低成本第 3 名', 0.7, '基础排名第 3'),
        createChannel(13, '第 2 名渠道', 0.9, '基础排名第 2'),
      ],
    })

    assert.ok(markup.indexOf('低成本第 3 名') < markup.indexOf('第 2 名渠道'))
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
      ...ADAPTIVE_SAMPLING_POLICY,
      group: 'vip',
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
      models: [],
      model_order: ['model-zeta', 'model-beta'],
      min_samples: 5,
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
    } satisfies ChannelMonitorSmartScheduleGroupPolicy

    const markup = renderBoard({ result, groupPolicies: [groupPolicy] })

    assert.ok(markup.indexOf('model-zeta') < markup.indexOf('model-beta'))
    assert.ok(markup.indexOf('model-beta') < markup.indexOf('model-alpha'))
    assert.ok(markup.indexOf('model-alpha') < markup.indexOf('model-gamma'))
    assert.ok(markup.includes('Zeta 渠道'))
    assert.equal(markup.includes('Beta 渠道'), false)
    assert.equal(markup.includes('Alpha 渠道'), false)
    assert.equal(markup.includes('Gamma 渠道'), false)
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

  test('keeps protection recovery clickable and secondary routes directly visible', () => {
    const markup = renderBoard()

    assert.match(
      markup,
      /<button[^>]*data-slot="badge"[^>]*aria-label="解除 恢复中渠道 vip model-fast 的稳定性降级保护"[^>]*>/
    )
    assert.ok(markup.includes('备用渠道'))
    assert.ok(markup.includes('备用顺位'))
    assert.ok(markup.includes('未参与渠道'))
    assert.ok(markup.includes('未参与'))
    assert.equal(markup.includes('备用与未参与路由'), false)
  })

  test('turns the fixed-primary route action into a clear action', () => {
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
    assert.ok(markup.includes('取消固定 高速渠道'))
    assert.ok(markup.includes('查看 高速渠道 的调度详情'))
  })
})
