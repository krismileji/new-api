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

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionDetectorState,
  ChannelModelDetectionHealth,
  ChannelModelDetectionOutcomeCode,
} from '../../types-model-detection'
import { domWindow } from './test-dom'

const { renderToStaticMarkup } = await import('react-dom/server')
const { createInstance } = await import('i18next')
const { I18nextProvider } = await import('react-i18next')
const { ChannelModelDetectionCard } =
  await import('../channel-model-detection-card')

const testI18n = createInstance()
await testI18n.init({
  lng: 'zh',
  resources: {
    zh: {
      translation: {
        '{{count}} requests cannot be estimated yet': '{{count}} 次暂无法估算',
        'Pending verification': '待核实',
        'Pending verification {{cost}}': '待核实 {{cost}}',
      },
    },
  },
})

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
    recent_window: [
      {
        started_at: 1_753_999_680,
        result: 'success' as const,
        detection_count: 1,
        success: 1,
        attention: 0,
        unhealthy: 0,
        failed: 0,
        running: 0,
        inactive: 0,
      },
      {
        started_at: 1_753_999_740,
        result: 'unhealthy' as const,
        detection_count: 1,
        success: 0,
        attention: 0,
        unhealthy: 1,
        failed: 0,
        running: 0,
        inactive: 0,
      },
      {
        started_at: 1_753_999_800,
        result: 'attention' as const,
        detection_count: 1,
        success: 0,
        attention: 1,
        unhealthy: 0,
        failed: 0,
        running: 0,
        inactive: 0,
      },
      {
        started_at: 1_753_999_860,
        result: 'failed' as const,
        detection_count: 1,
        success: 0,
        attention: 0,
        unhealthy: 0,
        failed: 1,
        running: 0,
        inactive: 0,
      },
      {
        started_at: 1_753_999_920,
        result: 'inactive' as const,
        detection_count: 1,
        success: 0,
        attention: 0,
        unhealthy: 0,
        failed: 0,
        running: 0,
        inactive: 1,
      },
      {
        started_at: 1_753_999_980,
        result: '' as const,
        detection_count: 0,
        success: 0,
        attention: 0,
        unhealthy: 0,
        failed: 0,
        running: 0,
        inactive: 0,
      },
    ],
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
    cost_ratio: null,
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
    today_model_detection_cost: createCost({
      settled_cost_nano_cny: 51_360_000,
      settled_cost_cny: '0.051360000',
    }),
    today_model_detection_cost_cny: 0.05136,
  }
}

function renderCard(
  channel: ChannelModelDetectionChannel,
  detectorState: ChannelModelDetectionDetectorState = 'available'
) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={testI18n}>
      <ChannelModelDetectionCard
        channel={channel}
        detectorState={detectorState}
        scheduledPreset='medium'
        scheduleEnabled
        displayValue={6}
        displayUnit='minute'
        nextBatchAt={1_754_086_400}
        serverNow={1_754_000_100}
        onOpenHistory={noop}
        onOpenConfig={noop}
        onOpenManualRun={noop}
        onCancelRun={noop}
        onToggleSchedule={noop}
      />
    </I18nextProvider>
  )
}

describe('模型检测渠道卡片', () => {
  test('固定高度且主体和图标命令均具备键盘入口', () => {
    const html = renderCard(createChannel('healthy'))

    assert.match(html, /h-\[28rem\]/)
    assert.match(html, /min-w-0/)
    assert.match(html, /overflow-y-auto/)
    assert.match(html, /aria-label="打开 .* 模型检测记录"/)
    assert.match(html, /aria-label="选择档位并立即检测"/)
    assert.match(html, /aria-label="配置模型检测目标"/)
    assert.match(html, /aria-label="禁用渠道"/)
    assert.match(html, /aria-label="退出统一定时检测"/)
    assert.match(html, /申报 Sol/)
    assert.match(html, /手动 · 中档/)
    assert.match(html, /已结算成本 ¥0\.025680000 · 额度 12,840/)
    assert.match(html, /最近模型检测成本/)
    assert.match(html, /今日模型检测成本/)
    assert.match(html, /¥0\.0257/)
    assert.match(html, /¥0\.0514/)
    assert.match(
      html,
      /gpt-5\.6-with-a-very-long-provider-alias.*检测进度 64 \/ 64/
    )
    assert.match(html, /data-slot=progress-track\]\]:h-2\.5/)
    assert.match(html, /data-slot=progress-track\]\]:rounded-sm/)
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

    const unchecked = renderCard(createChannel('healthy'), 'unknown')
    assert.doesNotMatch(unchecked, /检测器不可用/)
    assert.doesNotMatch(unchecked, /aria-label="检测器当前不可用"/)
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
    assert.match(html, /data-slot="model-detection-run-progress"/)
    assert.match(html, /当前轮次 · 启动待确认/)
    assert.match(html, /0 \/ 202 · 0%/)
    assert.match(html, /aria-label=".*当前轮次进度 0 \/ 202（0%）"/)
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
    assert.match(
      domWindow.document.body.textContent ?? '',
      /当前轮次 · 取消中12 \/ 64 · 19%/
    )
    assert.ok(cancelButton)
    assert.equal(cancelButton.disabled, true)
    assert.doesNotMatch(
      domWindow.document.body.textContent ?? '',
      /检测到异常证据/
    )
  })

  test('Juice 通过但强指向其他型号时按模型异常展示', () => {
    const channel = createChannel('healthy')
    const latest = channel.targets[0]?.latest
    if (!latest) throw new Error('测试目标缺少最新执行')
    Object.assign(latest, {
      title_cn: 'Juice 通过；指纹强烈指向 Luna',
      fingerprint_verdict_state: 'strong_match',
      fingerprint_model: 'gpt-5.6-luna',
      fingerprint_claim_mismatch: true,
    })

    domWindow.document.body.innerHTML = renderCard(channel)
    const target = domWindow.document.querySelector(
      '[data-slot="model-detection-target"]'
    )
    assert.ok(target)
    const outcome = target.children.item(1)
    assert.ok(outcome)
    assert.match(outcome.className, /text-destructive/)
    assert.doesNotMatch(outcome.className, /text-success/)
    assert.match(outcome.textContent ?? '', /强烈指向 Luna/)
  })

  test('上一周期超时的取消结果按警告展示', () => {
    const channel = createChannel('attention')
    const latest = channel.targets[0]?.latest
    if (!latest) throw new Error('测试目标缺少最新执行')
    Object.assign(latest, {
      status: 'canceled',
      error_code: 'schedule_timeout',
      outcome_code: '',
      title_cn: '',
    })

    domWindow.document.body.innerHTML = renderCard(channel)
    const target = domWindow.document.querySelector(
      '[data-slot="model-detection-target"]'
    )
    assert.ok(target)
    const outcome = target.children.item(1)
    assert.ok(outcome)
    assert.match(outcome.className, /text-warning/)
    assert.doesNotMatch(outcome.className, /text-muted-foreground/)
    assert.match(outcome.textContent ?? '', /周期超时警告/)
  })

  test('每个目标按配置的时间范围渲染状态格并区分结果语义', () => {
    domWindow.document.body.innerHTML = renderCard(createChannel('healthy'))
    const buckets = [
      ...domWindow.document.querySelectorAll(
        '[data-slot="model-detection-bucket"]'
      ),
    ]

    assert.equal(buckets.length, 6)
    assert.equal(
      buckets.filter((bucket) =>
        bucket.getAttribute('aria-label')?.includes('已开启但未执行')
      ).length,
      1
    )

    const success = buckets.find((bucket) =>
      bucket.className.includes('bg-success')
    )
    const conflict = buckets.find((bucket) =>
      bucket.className.includes('bg-destructive')
    )
    const attention = buckets.find(
      (bucket) =>
        bucket.className.includes('bg-warning') &&
        !bucket.className.includes('bg-warning/70')
    )
    const failed = buckets.find((bucket) =>
      bucket.className.includes('bg-warning/70')
    )
    const canceled = buckets.find((bucket) =>
      bucket.className.includes('bg-muted-foreground/70')
    )

    assert.match(success?.className ?? '', /bg-success/)
    assert.match(conflict?.className ?? '', /bg-destructive/)
    assert.match(attention?.className ?? '', /bg-warning/)
    assert.match(failed?.className ?? '', /bg-warning\/70/)
    assert.match(canceled?.className ?? '', /bg-muted-foreground\/70/)
    assert.match(
      domWindow.document.body.innerHTML,
      /aria-label=".*近 6 分钟模型检测结果"/
    )
    assert.match(
      domWindow.document.body.innerHTML,
      /检测 1 · 正常 1 · 关注 0 · 异常 0 · 执行失败 0 · 进行中 0 · 跳过 0/
    )
  })

  test('统一定时检测已开启但时间格未执行时使用灰色状态', () => {
    const channel = createChannel('healthy')
    const target = channel.targets[0]
    if (!target) throw new Error('测试渠道缺少模型检测目标')
    target.recent_window = target.recent_window.slice(-1)

    const enabledHtml = renderCard(channel)
    assert.match(
      enabledHtml,
      /data-model-detection-bucket-state="not-executed"/
    )
    assert.match(enabledHtml, /bg-muted-foreground\/35/)
    assert.match(enabledHtml, /定时检测已开启但未执行/)

    if (!channel.config) throw new Error('测试渠道缺少模型检测配置')
    channel.config.schedule_enabled = false
    const pausedHtml = renderCard(channel)
    assert.doesNotMatch(
      pausedHtml,
      /data-model-detection-bucket-state="not-executed"/
    )
    assert.match(
      pausedHtml,
      /data-model-detection-bucket-state="not-scheduled"/
    )
    assert.match(pausedHtml, /定时检测未开启/)
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
    assert.match(html, /成本待核实 · 1 次请求/)
    assert.doesNotMatch(html, /已结算成本 ¥0\.000000000/)
  })

  test('未决成本进入最近与今日摘要但保持待核实语义', () => {
    const channel = createChannel('healthy')
    const unresolvedCost = createCost({
      settled_quota: 0,
      cost_basis_quota: 0,
      settled_cost_nano_cny: 0,
      settled_cost_cny: '0.000000000',
      settled_request_count: 0,
      unresolved_cost_nano_cny: 759_054_000,
      unresolved_cost_cny: '0.759054000',
      unresolved_request_count: 49,
      status: 'unresolved',
    })
    channel.latest_run_cost = unresolvedCost
    channel.today_model_detection_cost = unresolvedCost
    channel.today_model_detection_cost_cny = 0.759054

    const html = renderCard(channel)

    assert.equal((html.match(/>待核实</g) ?? []).length, 2)
    assert.doesNotMatch(html, /0\.7591/)
    assert.doesNotMatch(html, /最近模型检测成本<\/dt><dd[^>]*>-<\/dd>/)
  })
})
