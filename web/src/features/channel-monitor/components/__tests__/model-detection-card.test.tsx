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

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionHealth,
  ChannelModelDetectionOutcomeCode,
} from '../../types-model-detection'
import { domWindow } from './test-dom'

const { renderToStaticMarkup } = await import('react-dom/server')
const { ChannelModelDetectionCard } =
  await import('../channel-model-detection-card')

const noop = () => {}

function createCost(overrides: Partial<ChannelModelDetectionCost> = {}) {
  return {
    currency: 'CNY',
    estimated_quota: 14_000,
    estimated_cost_nano_cny: 28_000_000,
    estimated_cost_cny: '0.028000000',
    cost_estimate_unknown_count: 0,
    settled_quota: 12_840,
    cost_basis_quota: 12_840,
    settled_cost_nano_cny: 25_680_000,
    settled_cost_cny: '0.025680000',
    unresolved_cost_nano_cny: 0,
    unresolved_cost_cny: '0.000000000',
    unresolved_cost_unknown_count: 0,
    settled_request_count: 64,
    unresolved_request_count: 0,
    status: 'settled',
    cost_scope: 'channel_upstream_api',
    ...overrides,
  } satisfies ChannelModelDetectionCost
}

function createChannel(
  health: ChannelModelDetectionHealth,
  outcome:
    | ChannelModelDetectionOutcomeCode
    | '' = 'juice_pass_fingerprint_strong'
): ChannelModelDetectionChannel {
  const target = {
    target_key: 'target-sol',
    request_model:
      'gpt-5.6-with-a-very-long-provider-alias-that-must-not-overflow',
    claimed_model: 'gpt-5.6-sol' as const,
    enabled: true,
    position: 0,
    latest: {
      run_id: 'run-1',
      target_key: 'target-sol',
      status: 'completed' as const,
      request_model:
        'gpt-5.6-with-a-very-long-provider-alias-that-must-not-overflow',
      claimed_model: 'gpt-5.6-sol' as const,
      outcome_code: outcome,
      title_cn:
        outcome === 'possible_non_gpt'
          ? '可能非 GPT'
          : 'Juice 通过；指纹强烈指向 Sol',
      subtitle_cn: '',
      preset: 'medium' as const,
      preset_source: 'manual_selected' as const,
      trigger: 'manual' as const,
      progress: {
        planned: 64,
        logical_completed: 64,
        successful: 64,
        errors: 0,
        cancelled: 0,
        http_attempts: 64,
        retries: 0,
      },
      cost: createCost(),
      started_at: 1_754_000_000,
      finished_at: 1_754_000_060,
      updated_at: 1_754_000_060,
    },
  }
  return {
    id: 12,
    name: '模型检测渠道名称非常长但必须保持单行稳定布局',
    type: 1,
    channel_status: 1,
    remark: '主线路',
    groups: ['default'],
    supported_models: [target.request_model],
    health_status: health,
    config: {
      channel_id: 12,
      schedule_enabled: true,
      revision: 1,
      created_at: 1_754_000_000,
      updated_at: 1_754_000_000,
    },
    active_run: null,
    targets: [target],
    latest_run_cost: target.latest.cost,
    today_model_detection_cost_cny: 0.05136,
  }
}

function renderCard(
  channel: ChannelModelDetectionChannel,
  detectorState: 'available' | 'offline' = 'available'
) {
  return renderToStaticMarkup(
    <ChannelModelDetectionCard
      channel={channel}
      detectorState={detectorState}
      scheduledPreset='medium'
      scheduleEnabled
      nextBatchAt={1_754_086_400}
      serverNow={1_754_000_100}
      onOpenHistory={noop}
      onOpenConfig={noop}
      onOpenManualRun={noop}
      onCancelRun={noop}
      onToggleSchedule={noop}
    />
  )
}

describe('模型检测渠道卡片', () => {
  test('固定高度且主体和图标命令均具备键盘入口', () => {
    const html = renderCard(createChannel('healthy'))

    assert.match(html, /h-\[25rem\]/)
    assert.match(html, /min-w-0/)
    assert.match(html, /overflow-y-auto/)
    assert.match(html, /aria-label="打开 .* 模型检测记录"/)
    assert.match(html, /aria-label="选择档位并立即检测"/)
    assert.match(html, /aria-label="配置模型检测目标"/)
    assert.match(html, /aria-label="退出统一定时检测"/)
    assert.match(html, /申报 Sol/)
    assert.match(html, /手动 · 中档/)
    assert.match(html, /已结算成本 ¥0\.025680000 · 额度 12,840/)
    assert.match(html, /最近模型检测成本/)
    assert.match(html, /今日模型检测成本/)
    assert.match(html, /¥0\.0257/)
    assert.match(html, /¥0\.0514/)
  })

  test('七类聚合状态和离线状态都有文字等价', () => {
    const expected: Array<[ChannelModelDetectionHealth, RegExp]> = [
      ['pending', /待检测/],
      ['running', /检测中/],
      ['healthy', />正常</],
      ['attention', /需关注/],
      ['unhealthy', /检测到异常证据/],
      ['detector_unavailable', /检测器离线/],
      ['stale', /结果已过期/],
    ]

    for (const [health, label] of expected) {
      assert.match(renderCard(createChannel(health)), label)
    }

    const unhealthyOffline = renderCard(createChannel('unhealthy'), 'offline')
    assert.match(unhealthyOffline, /检测到异常证据/)
    assert.match(unhealthyOffline, /检测器不可用/)
    assert.match(unhealthyOffline, /aria-label="检测器当前不可用"/)
    assert.match(unhealthyOffline, /disabled=""/)
  })

  test('基础设施活动状态保留独立文字且不会归类为模型异常', () => {
    const channel = createChannel('running')
    channel.active_run = {
      run_id: 'run-pending-confirmation',
      status: 'submission_unknown',
      trigger: 'manual',
      preset: 'high',
      preset_source: 'manual_selected',
      progress: {
        planned: 202,
        logical_completed: 0,
        successful: 0,
        errors: 0,
        cancelled: 0,
        http_attempts: 0,
        retries: 0,
      },
      queued_at: 1_754_000_000,
      started_at: 0,
      updated_at: 1_754_000_010,
    }
    const html = renderCard(channel)

    assert.match(html, /启动待确认/)
    assert.match(html, /aria-label="取消当前模型检测"/)
    assert.doesNotMatch(html, /检测到异常证据/)
  })

  test('取消中的任务锁定取消按钮且保留独立状态', () => {
    const channel = createChannel('running')
    channel.active_run = {
      run_id: 'run-canceling',
      status: 'canceling',
      trigger: 'manual',
      preset: 'medium',
      preset_source: 'manual_selected',
      progress: {
        planned: 64,
        logical_completed: 12,
        successful: 12,
        errors: 0,
        cancelled: 0,
        http_attempts: 12,
        retries: 0,
      },
      queued_at: 1_754_000_000,
      started_at: 1_754_000_001,
      updated_at: 1_754_000_010,
    }
    domWindow.document.body.innerHTML = renderCard(channel)
    const cancelButton = domWindow.document.querySelector(
      '[aria-label="取消当前模型检测"]'
    ) as HTMLButtonElement | null

    assert.match(domWindow.document.body.textContent ?? '', /取消中/)
    assert.ok(cancelButton)
    assert.equal(cancelButton.disabled, true)
    assert.doesNotMatch(
      domWindow.document.body.textContent ?? '',
      /检测到异常证据/
    )
  })

  test('未配置卡片提供单一配置命令且不嵌套按钮', () => {
    const channel = createChannel('unconfigured')
    channel.config = null
    channel.targets = []
    const html = renderCard(channel)
    domWindow.document.body.innerHTML = html

    assert.match(html, /尚未配置检测目标/)
    assert.match(html, /配置检测目标/)
    for (const button of domWindow.document.querySelectorAll('button')) {
      assert.equal(button.querySelector('button'), null)
    }
  })

  test('未知结论不会误报正常且未知成本不会显示为零', () => {
    const channel = createChannel('attention', 'future_detector_outcome')
    const latest = channel.targets[0]?.latest
    if (!latest) throw new Error('测试目标缺少最新执行')
    latest.cost = createCost({
      settled_quota: 0,
      settled_cost_nano_cny: 0,
      settled_cost_cny: '0.000000000',
      settled_request_count: 0,
      unresolved_cost_nano_cny: null,
      unresolved_cost_cny: null,
      unresolved_cost_unknown_count: 1,
      unresolved_request_count: 1,
      status: 'unresolved',
    })
    const html = renderCard(channel)

    assert.match(html, /检测器返回了新结论，请升级主系统适配/)
    assert.match(html, /暂无法估算 · 1 次请求/)
    assert.doesNotMatch(html, /已结算成本 ¥0\.000000000/)
  })
})
