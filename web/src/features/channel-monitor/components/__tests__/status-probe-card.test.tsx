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

import { areChannelStatusProbeCardPropsEqual } from '../../lib/status-probe-card-render'
import type { ChannelStatusProbeChannel } from '../../types'
import { ChannelStatusProbeCard } from '../channel-status-probe-card'

const noop = () => {}

function createRecentWindow(
  modelName: string,
  result: 'success' | 'upstream_failure'
) {
  const currentMinute = 1_754_000_000 - (1_754_000_000 % 60)
  return Array.from({ length: 60 }, (_, index) => ({
    started_at: currentMinute - (59 - index) * 60,
    result: index === 59 ? result : ('' as const),
    success: index === 59 && result === 'success' ? 1 : 0,
    upstream_failure: index === 59 && result === 'upstream_failure' ? 1 : 0,
    rate_limited: 0,
    local_failure: 0,
    skipped: 0,
    canceled: 0,
    models: index === 59 ? [modelName] : [],
  }))
}

function createChannel(): ChannelStatusProbeChannel {
  const latestA: NonNullable<ChannelStatusProbeChannel['latest']> = {
    id: 1,
    channel_id: 12,
    model_name: 'gpt-4.1',
    execution_id: 1,
    run_id: 'run-1',
    started_at: 1_754_000_000,
    finished_at: 1_754_000_001,
    result: 'success',
    success: true,
    request_dispatched: true,
    response_time_ms: 1250,
    first_token_ms: 240,
    tps: 42.5,
    settled_cost_nano_cny: 12_000_000,
    error_code: '',
    error_message: '',
    consecutive_successes: 1,
    consecutive_failures: 0,
    last_health_result: 'success',
    last_health_execution_id: 1,
    last_health_finished_at: 1_754_000_001,
    sample_status: 'skipped',
    sample_message: '未开启智能调度样本记录',
    trigger: 'scheduled',
    endpoint: '/v1/responses',
    stream: true,
    created_at: 1_754_000_001,
    updated_at: 1_754_000_001,
  }
  const latestB: NonNullable<ChannelStatusProbeChannel['latest']> = {
    ...latestA,
    id: 2,
    model_name: 'gpt-4.1-mini',
    execution_id: 2,
    run_id: 'run-2',
    finished_at: 1_754_000_002,
    result: 'upstream_failure',
    success: false,
    first_token_ms: 180,
    tps: 55,
    settled_cost_nano_cny: 25_000_000,
    consecutive_successes: 0,
    consecutive_failures: 1,
    last_health_result: 'upstream_failure',
    last_health_execution_id: 2,
    last_health_finished_at: 1_754_000_002,
    updated_at: 1_754_000_002,
  }
  return {
    id: 12,
    name: '低倍率渠道',
    type: 1,
    channel_status: 1,
    remark: '低成本验收渠道',
    groups: ['default', '低成本'],
    cost_ratio: 0.75,
    today_probe_cost_cny: 0.08,
    supported_models: ['gpt-4.1', 'gpt-4.1-mini'],
    allows_custom_model: false,
    config: {
      id: 1,
      channel_id: 12,
      enabled: true,
      models: ['gpt-4.1', 'gpt-4.1-mini'],
      interval_seconds: 300,
      display_value: 60,
      display_unit: 'minute',
      record_sample: false,
      next_run_at: 1_754_000_300,
      manual_request_id: '',
      manual_requested_at: 0,
      revision: 1,
      running_trigger: '',
      running_run_id: '',
      running_started_at: 0,
      created_at: 1_754_000_000,
      updated_at: 1_754_000_000,
    },
    health_status: 'healthy',
    running: false,
    latest: latestB,
    avg_first_token_ms: 240,
    avg_tps: 42.5,
    model_statuses: [
      {
        model_name: 'gpt-4.1',
        health_status: 'healthy',
        latest: latestA,
        recent_window: createRecentWindow('gpt-4.1', 'success'),
        avg_first_token_ms: 240,
        avg_tps: 42.5,
      },
      {
        model_name: 'gpt-4.1-mini',
        health_status: 'unhealthy',
        latest: latestB,
        recent_window: createRecentWindow('gpt-4.1-mini', 'upstream_failure'),
        avg_first_token_ms: null,
        avg_tps: null,
      },
    ],
    configured_model_count: 2,
  }
}

describe('状态探测卡片', () => {
  test('服务端时钟只在下次执行展示文本变化时触发重渲染', () => {
    const props = {
      channel: createChannel(),
      serverNow: 1_754_000_000,
      actionPending: false,
      onOpenHistory: noop,
      onOpenConfig: noop,
      onRun: noop,
      onToggleEnabled: noop,
    }

    assert.equal(
      areChannelStatusProbeCardPropsEqual(props, {
        ...props,
        serverNow: props.serverNow + 1,
      }),
      true
    )
    assert.equal(
      areChannelStatusProbeCardPropsEqual(props, {
        ...props,
        serverNow: props.serverNow + 60,
      }),
      false
    )
    assert.equal(
      areChannelStatusProbeCardPropsEqual(props, {
        ...props,
        actionPending: true,
      }),
      false
    )
  })

  test('每个配置模型固定渲染一条状态带并保留独立操作入口', () => {
    const channel = createChannel()
    const html = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={channel}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.equal(
      (html.match(/data-slot="status-probe-bucket"/g) ?? []).length,
      120
    )
    assert.equal((html.match(/data-window-buckets="60"/g) ?? []).length, 2)
    assert.equal(
      (html.match(/data-slot="status-probe-model"/g) ?? []).length,
      2
    )
    assert.match(html, /h-\[28rem\]/)
    assert.match(html, /class="[^"]*min-h-10[^"]*overflow-y-auto[^"]*"/)
    assert.match(html, /近 60 分钟状态/)
    assert.match(html, /gpt-4.1-mini/)
    assert.match(html, /aria-label="立即检测"/)
    assert.match(html, /aria-label="配置状态探测"/)
    assert.match(html, /aria-label="打开 低倍率渠道 状态探测记录"/)
    assert.match(html, /aria-label="不计入智能调度样本"/)
    assert.match(html, />不计入样本</)
  })

  test('开启样本记录时使用标签展示计入样本', () => {
    const channel = createChannel()
    if (!channel.config) throw new Error('测试渠道缺少状态探测配置')
    channel.config.record_sample = true

    const html = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={channel}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.match(html, /aria-label="计入智能调度样本"/)
    assert.match(html, />计入样本</)
  })

  test('同时展示最近指标、窗口平均值和渠道备注并移除总响应', () => {
    const html = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={createChannel()}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.match(html, /备注：低成本验收渠道/)
    assert.match(html, /最近首字/)
    assert.match(html, />180 ms</)
    assert.match(html, /最近 TPS/)
    assert.match(html, />55\.0</)
    assert.match(html, /最近探测成本/)
    assert.match(html, /0\.0250/)
    assert.match(html, /今日探测成本/)
    assert.match(html, /0\.0800/)
    assert.match(html, /近 60 分钟平均首字/)
    assert.match(html, />240 ms</)
    assert.match(html, /近 60 分钟平均 TPS/)
    assert.match(html, />42\.5</)
    assert.doesNotMatch(html, /总响应/)
    assert.doesNotMatch(html, />1\.25 s</)
  })

  test('成本字段增加后状态列表受限且下次执行信息保留独立底栏', () => {
    const html = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={createChannel()}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.match(
      html,
      /data-slot="card-content" class="[^"]*overflow-hidden[^"]*"/
    )
    assert.match(
      html,
      /data-slot="card-footer" class="[^"]*min-h-11[^"]*justify-between[^"]*"/
    )
    assert.match(html, /下次/)
  })

  test('按配置的数值和单位展示状态条与平均值范围', () => {
    const channel = createChannel()
    if (!channel.config) throw new Error('测试渠道缺少状态探测配置')
    channel.config.display_value = 2
    channel.config.display_unit = 'hour'
    channel.model_statuses = channel.model_statuses.map((modelStatus) => ({
      ...modelStatus,
      recent_window: modelStatus.recent_window.slice(-2),
    }))

    const html = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={channel}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.equal(
      (html.match(/data-slot="status-probe-bucket"/g) ?? []).length,
      4
    )
    assert.equal((html.match(/data-window-buckets="2"/g) ?? []).length, 2)
    assert.match(html, /近 2 小时平均首字/)
    assert.match(html, /近 2 小时状态/)
  })

  test('周期探测已开启但时间格未执行时使用灰色状态', () => {
    const channel = createChannel()
    channel.model_statuses = channel.model_statuses.map((modelStatus) => ({
      ...modelStatus,
      recent_window: modelStatus.recent_window.slice(0, 1),
    }))

    const enabledHtml = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={channel}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.equal(
      (enabledHtml.match(/data-probe-bucket-state="not-executed"/g) ?? [])
        .length,
      2
    )
    assert.match(enabledHtml, /bg-muted-foreground\/35/)
    assert.match(enabledHtml, /周期探测已开启但未执行/)

    if (!channel.config) throw new Error('测试渠道缺少状态探测配置')
    channel.config.enabled = false
    const pausedHtml = renderToStaticMarkup(
      <ChannelStatusProbeCard
        channel={channel}
        serverNow={1_754_000_000}
        actionPending={false}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onRun={noop}
        onToggleEnabled={noop}
      />
    )

    assert.doesNotMatch(pausedHtml, /data-probe-bucket-state="not-executed"/)
    assert.match(pausedHtml, /data-probe-bucket-state="not-scheduled"/)
    assert.match(pausedHtml, /周期探测未开启/)
  })
})
