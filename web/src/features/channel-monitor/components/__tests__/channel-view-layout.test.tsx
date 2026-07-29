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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { formatTimestampToDate } from '@/lib/format'

import { formatChannelMonitorCost, formatMonitorRatio } from '../../lib/format'
import { placeChannelMonitorSmartScheduleRoutes } from '../../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSuccessSummary,
} from '../../types'
import { ChannelMonitorChannelView } from '../channel-monitor-channel-view'

const noop = () => {}
const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true
const testI18n = createInstance()

await testI18n.init({
  lng: 'zhCN',
  resources: { zhCN: { translation: {} } },
})

function createChannel(overrides: Partial<ChannelMonitorItem> = {}) {
  return {
    id: 7,
    name: '测试渠道',
    type: 1,
    status: 1,
    status_reason: '',
    priority: 0,
    weight: 0,
    base_url: 'https://example.com',
    models: 'test-model',
    test_model: 'test-model',
    groups: ['default'],
    ratio: 1.25,
    previous_ratio: 1,
    cost_ratio: 1.25,
    previous_cost_ratio: 1,
    conversion_factor: 1,
    remark: '',
    channel_remark: '',
    updated_time: 1_752_777_845,
    updated_by: 1,
    updated_by_username: '系统自动更新',
    last_fetch_status: 'succeeded',
    last_fetch_error: '',
    last_fetch_time: 1_752_777_845,
    consecutive_failures: 0,
    upstream_balance: 42.5,
    last_balance_time: 1_752_691_445,
    last_balance_error: '',
    today_cost_cny: 1.23456,
    today_cost_configured: true,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    upstream: {
      type: 'new_api',
      base_url: 'https://upstream.example.com',
      group: 'default',
      auth_type: 'api_key',
      user_id: 0,
      has_access_token: true,
      account: '',
      has_password: false,
      single_channel_action: 'update_group_ratio',
      multiple_channels_action: 'disable_channel',
      balance_warning_threshold: null,
      balance_auto_disable_threshold: null,
      ratio_sync_enabled: true,
      balance_sync_enabled: true,
      cost_conversion: { mode: 'none' },
    },
    ...overrides,
  } satisfies ChannelMonitorItem
}

function createSmartScheduleRoute(): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: 7,
    channel_name: '测试渠道',
    channel_status: 1,
    channel_priority: 100,
    channel_weight: 100,
    group: 'default',
    model: 'test-model',
    enabled: true,
    priority: 80,
    weight: 60,
    state: {
      id: 1,
      channel_id: 7,
      group: 'default',
      model: 'test-model',
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

function renderView(
  channel: ChannelMonitorItem,
  groupRatios: Record<string, number> = { default: 1 },
  groupCoefficients: Record<string, number> = { default: 1 },
  successByChannel: Map<number, ChannelMonitorSuccessSummary> = new Map(),
  smartScheduleRoutes: ChannelMonitorSmartScheduleRoute[] = []
) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={testI18n}>
      <ChannelMonitorChannelView
        channels={[channel]}
        groupRatios={groupRatios}
        groupCoefficients={groupCoefficients}
        performanceByChannel={new Map()}
        successByChannel={successByChannel}
        successMetricsAvailable={successByChannel.size > 0}
        performanceRangeLabel='24 小时'
        performanceLoading={false}
        performanceError={false}
        smartScheduleRoutesByChannel={
          new Map([[channel.id, smartScheduleRoutes]])
        }
        effectiveSmartScheduleRoutesByChannel={
          new Map([[channel.id, smartScheduleRoutes]])
        }
        smartSchedulePlacements={placeChannelMonitorSmartScheduleRoutes(
          smartScheduleRoutes
        )}
        smartScheduleUpdatePending={false}
        onUpdateSmartSchedule={noop}
        onOpenSmartSchedule={noop}
        onClearSmartScheduleStability={noop}
        onFetchUpstreamBalance={noop}
        onFetchUpstreamRatio={noop}
        onToggleStatus={noop}
        onTestConnection={noop}
        onEditConcurrency={noop}
        onEditGroups={noop}
        onConfigureUpstream={noop}
        onViewHistory={noop}
        onOpenCostHistory={noop}
        onOpenSuccessDetail={noop}
        fetchingBalanceChannelId={null}
        fetchingRatioChannelId={null}
        updatingStatusChannelId={null}
      />
    </I18nextProvider>
  )
}

function getTableCells(markup: string) {
  return markup.match(/<td\b[\s\S]*?<\/td>/g) ?? []
}

function getTableHeaders(markup: string) {
  return markup.match(/<th\b[\s\S]*?<\/th>/g) ?? []
}

describe('channel monitor channel view timestamps', () => {
  test('sizes columns from content while preserving balance and action minimums', () => {
    const markup = renderView(createChannel())
    const cells = getTableCells(markup)
    const headers = getTableHeaders(markup)

    assert.match(markup, /table-auto/)
    assert.match(markup, /w-max/)
    assert.match(markup, /min-w-full/)
    assert.doesNotMatch(markup, /<colgroup>/)
    assert.match(headers[1] ?? '', /min-w-\[224px\]/)
    assert.match(headers[8] ?? '', /min-w-\[112px\]/)
    assert.match(cells[1] ?? '', /min-w-\[224px\]/)
    assert.match(cells[8] ?? '', /min-w-\[112px\]/)
  })

  test('shows ratio, group, and update time without updater attribution', () => {
    const channel = createChannel()
    const markup = renderView(channel)
    const cells = getTableCells(markup)

    assert.equal(cells.length, 9)
    assert.doesNotMatch(markup, /<th\b[^>]*>更新时间<\/th>/)
    assert.doesNotMatch(markup, /<th\b[^>]*>倍率更新状态<\/th>/)
    assert.doesNotMatch(markup, /<th\b[^>]*>今日成本<\/th>/)
    assert.ok(markup.indexOf('上游余额') < markup.indexOf('成本倍率'))
    assert.ok(
      cells[1]?.includes(
        `更新：${formatTimestampToDate(channel.last_balance_time)}`
      )
    )
    assert.ok(
      cells[2]?.includes(`更新：${formatTimestampToDate(channel.updated_time)}`)
    )
    assert.ok(cells[2]?.includes('上游分组：default'))
    assert.equal(cells[2]?.includes(channel.updated_by_username), false)
  })

  test('places upstream refresh actions before their metric text with compact spacing', () => {
    const channel = createChannel()
    const markup = renderView(channel)
    const cells = getTableCells(markup)
    const headers = getTableHeaders(markup)
    const balanceCell = cells[1] ?? ''
    const ratioCell = cells[2] ?? ''

    assert.ok(
      balanceCell.indexOf('aria-label="更新上游余额"') <
        balanceCell.indexOf('42.5')
    )
    assert.ok(
      ratioCell.indexOf('aria-label="更新上游倍率"') <
        ratioCell.indexOf(formatMonitorRatio(channel.cost_ratio))
    )
    assert.match(balanceCell, /grid-cols-\[24px_max-content\]/)
    assert.match(ratioCell, /grid-cols-\[24px_max-content\]/)
    assert.match(balanceCell, /w-max/)
    assert.match(ratioCell, /w-max/)
    assert.match(balanceCell, /col-start-2/)
    assert.match(ratioCell, /col-start-2/)
    assert.ok(headers[1]?.includes('pl-[34px]'))
    assert.ok(headers[2]?.includes('pl-[34px]'))
  })

  test('uses muted text only when ratio sync is disabled', () => {
    const channel = createChannel()
    assert.ok(channel.upstream)
    const enabledCells = getTableCells(renderView(channel))
    const cells = getTableCells(
      renderView({
        ...channel,
        conversion_factor: 0.5,
        upstream: {
          ...channel.upstream,
          ratio_sync_enabled: false,
        },
      })
    )

    assert.equal(cells[2]?.includes('换算'), false)
    assert.equal(cells[2]?.includes('倍率同步已关闭'), false)
    assert.match(
      enabledCells[2] ?? '',
      /class="font-mono text-base font-semibold"/
    )
    assert.match(
      cells[2] ?? '',
      /class="font-mono text-base font-semibold text-muted-foreground"/
    )
    assert.ok(cells[2]?.includes('上游分组：default'))
  })

  test('shows complete cell values and preserves their hover details', () => {
    const channel = createChannel()
    assert.ok(channel.upstream)
    const longChannelName = '这是一个用于验证省略展示的特别长渠道名称'
    const longUpstreamGroup = '这是一个用于验证省略展示的特别长上游分组名称'
    const longGroup = '这是一个用于验证省略展示的特别长关联分组名称'
    const cells = getTableCells(
      renderView({
        ...channel,
        name: longChannelName,
        groups: [longGroup],
        last_fetch_status: 'failed',
        consecutive_failures: 942,
        upstream: {
          ...channel.upstream,
          group: longUpstreamGroup,
        },
      })
    )
    const balanceTimestamp = formatTimestampToDate(channel.last_balance_time)
    const ratioTimestamp = formatTimestampToDate(channel.updated_time)

    assert.equal(cells.length, 9)
    assert.ok(cells[0]?.includes(`title="${longChannelName}"`))
    assert.ok(cells[1]?.includes(`title="更新：${balanceTimestamp}"`))
    assert.ok(cells[2]?.includes(`title="更新：${ratioTimestamp}"`))
    assert.ok(cells[2]?.includes(`title="上游分组：${longUpstreamGroup}"`))
    assert.ok(cells[3]?.includes(`title="${longGroup}"`))
    assert.doesNotMatch(cells[0] ?? '', /truncate/)
    assert.doesNotMatch(cells[1] ?? '', /truncate/)
    assert.ok(
      cells[2]?.includes(
        `whitespace-nowrap" title="上游分组：${longUpstreamGroup}`
      )
    )
    assert.doesNotMatch(cells[3] ?? '', /truncate/)
  })

  test('stacks related groups and orders them from lowest ratio to highest', () => {
    const cells = getTableCells(
      renderView(
        createChannel({ groups: ['premium', 'default', 'basic'] }),
        { premium: 2, default: 1, basic: 0.5 },
        { premium: 1, default: 1, basic: 1 }
      )
    )
    const groupCell = cells[3] ?? ''
    const basicIndex = groupCell.indexOf('title="basic"')
    const defaultIndex = groupCell.indexOf('title="default"')
    const premiumIndex = groupCell.indexOf('title="premium"')

    assert.match(groupCell, /flex-col/)
    assert.ok(basicIndex >= 0)
    assert.ok(basicIndex < defaultIndex)
    assert.ok(defaultIndex < premiumIndex)
  })

  test('does not show update metadata before either metric has been updated', () => {
    const markup = renderView(
      createChannel({
        updated_time: 0,
        updated_by_username: '',
        last_balance_time: 0,
      })
    )
    const cells = getTableCells(markup)

    assert.equal(cells[1]?.includes('更新：'), false)
    assert.equal(cells[2]?.includes('更新：'), false)
  })

  test('places the cost amount between upstream balance and update time without a prefix', () => {
    const channel = createChannel()
    const cells = getTableCells(renderView(channel))
    const balanceCell = cells[1] ?? ''

    const costText = formatChannelMonitorCost(1.23456)

    assert.doesNotMatch(balanceCell, />\s*今日成本\s*</)
    assert.ok(balanceCell.indexOf('42.5') < balanceCell.indexOf(costText))
    assert.ok(
      balanceCell.indexOf(costText) <
        balanceCell.indexOf(
          `更新：${formatTimestampToDate(channel.last_balance_time)}`
        )
    )
    assert.ok(balanceCell.includes(costText))
    assert.equal(balanceCell.includes('不完整'), false)
    assert.match(balanceCell, /<button\b/)
    assert.ok(balanceCell.includes('查看渠道 测试渠道 的今日成本详情'))
  })

  test('shows an explicit state when cost conversion is not configured', () => {
    const cells = getTableCells(
      renderView(createChannel({ today_cost_configured: false }))
    )

    assert.doesNotMatch(cells[1] ?? '', />\s*今日成本\s*</)
    assert.ok(cells[1]?.includes('未配置'))
    assert.match(cells[1] ?? '', /<button\b/)
    assert.ok(cells[1]?.includes('查看渠道 测试渠道 的今日成本详情'))
  })

  test('shows the low-balance warning badge immediately after the balance', () => {
    const channel = createChannel()
    assert.ok(channel.upstream)
    const cells = getTableCells(
      renderView(
        createChannel({
          upstream_balance: 4.5,
          upstream: {
            ...channel.upstream,
            balance_warning_threshold: 5,
          },
        })
      )
    )
    const balanceCell = cells[1] ?? ''

    assert.match(balanceCell, /4\.5[\s\S]*data-slot="badge"[\s\S]*低于预警值/)
  })

  test('keeps zero visible without exposing unresolved settlements', () => {
    const cells = getTableCells(
      renderView(
        createChannel({
          today_cost_cny: 0,
          today_cost_complete: false,
          today_cost_unresolved_count: 2,
        })
      )
    )

    assert.ok(cells[1]?.includes(formatChannelMonitorCost(0)))
    assert.equal(cells[1]?.includes('不完整'), false)
    assert.equal(cells[1]?.includes('未确认'), false)
  })

  test('shows channel concurrency limit as active over configured limit', () => {
    const cells = getTableCells(
      renderView(
        createChannel({
          concurrency_limit: 8,
          concurrency_active: 3,
        })
      )
    )

    assert.ok(cells[6]?.includes('3/8'))
    assert.ok(cells[6]?.includes('当前/上限'))
  })

  test('shows cache hit rate on the third line of the success rate cell', () => {
    const successByChannel = new Map<number, ChannelMonitorSuccessSummary>([
      [
        7,
        {
          actual_success_count: 9,
          actual_failure_count: 1,
          actual_sample_count: 10,
          actual_success_rate: 0.9,
          final_success_count: 9,
          final_failure_count: 1,
          final_sample_count: 10,
          final_success_rate: 0.9,
          cache_hit_count: 1,
          cache_sample_count: 2,
          cache_hit_rate: 0.5,
        },
      ],
    ])
    const cells = getTableCells(
      renderView(
        createChannel(),
        { default: 1 },
        { default: 1 },
        successByChannel
      )
    )
    const successCell = cells[5] ?? ''

    assert.match(successCell, /90%[\s\S]*9 \/ 10 次[\s\S]*缓存率[\s\S]*50%/)
    assert.ok(successCell.includes('缓存命中 1 / 2 次'))
  })

  test('shows unlimited concurrency and exposes the edit action', () => {
    const markup = renderView(createChannel({ concurrency_limit: 0 }))
    const cells = getTableCells(markup)

    assert.ok(cells[6]?.includes('不限'))
    assert.ok(markup.includes('aria-label="设置并发限制"'))
  })

  test('places group-model smart scheduling after the concurrency column', () => {
    const markup = renderView(
      createChannel(),
      { default: 1 },
      { default: 1 },
      new Map(),
      [createSmartScheduleRoute()]
    )
    const headers = getTableHeaders(markup)
    const cells = getTableCells(markup)
    const smartScheduleCell = cells[7] ?? ''

    assert.equal(cells.length, 9)
    assert.ok(headers[6]?.includes('并发限制'))
    assert.ok(headers[7]?.includes('智能调度'))
    assert.ok(smartScheduleCell.includes('1/1 路由参与'))
    assert.match(smartScheduleCell, /default \/ test-model[\s\S]*P80 \/ W60/)
    assert.ok(smartScheduleCell.includes('预计 100.0%'))
    assert.ok(smartScheduleCell.includes('查看 测试渠道 的智能调度详情'))
  })

  test('disables every channel participation switch while an update is pending', async () => {
    const primaryChannel = createChannel({ id: 7, name: '主渠道' })
    const standbyChannel = createChannel({ id: 8, name: '备用渠道' })
    const primaryRoute = {
      ...createSmartScheduleRoute(),
      channel_name: primaryChannel.name,
    }
    const standbyRoute = {
      ...createSmartScheduleRoute(),
      channel_id: standbyChannel.id,
      channel_name: standbyChannel.name,
      state: {
        ...createSmartScheduleRoute().state,
        id: 2,
        channel_id: standbyChannel.id,
      },
    }
    const routesByChannel = new Map([
      [primaryChannel.id, [primaryRoute]],
      [standbyChannel.id, [standbyRoute]],
    ])
    const placements = placeChannelMonitorSmartScheduleRoutes([
      primaryRoute,
      standbyRoute,
    ])
    const updates: Array<{ channelId: number; excluded: boolean }> = []
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    const renderChannelView = (pending: boolean) => (
      <I18nextProvider i18n={testI18n}>
        <ChannelMonitorChannelView
          channels={[primaryChannel, standbyChannel]}
          groupRatios={{ default: 1 }}
          groupCoefficients={{ default: 1 }}
          performanceByChannel={new Map()}
          successByChannel={new Map()}
          successMetricsAvailable={false}
          performanceRangeLabel='24 小时'
          performanceLoading={false}
          performanceError={false}
          smartScheduleRoutesByChannel={routesByChannel}
          effectiveSmartScheduleRoutesByChannel={routesByChannel}
          smartSchedulePlacements={placements}
          smartScheduleUpdatePending={pending}
          onUpdateSmartSchedule={(channelId, excluded) =>
            updates.push({ channelId, excluded })
          }
          onOpenSmartSchedule={noop}
          onClearSmartScheduleStability={noop}
          onFetchUpstreamBalance={noop}
          onFetchUpstreamRatio={noop}
          onToggleStatus={noop}
          onTestConnection={noop}
          onEditConcurrency={noop}
          onEditGroups={noop}
          onConfigureUpstream={noop}
          onViewHistory={noop}
          onOpenCostHistory={noop}
          onOpenSuccessDetail={noop}
          fetchingBalanceChannelId={null}
          fetchingRatioChannelId={null}
          updatingStatusChannelId={null}
        />
      </I18nextProvider>
    )

    await act(async () => {
      root.render(renderChannelView(false))
    })

    const primarySwitch = container.querySelector<HTMLElement>(
      '[aria-label="取消 主渠道 的智能调度参与"]'
    )
    const standbySwitch = container.querySelector<HTMLElement>(
      '[aria-label="取消 备用渠道 的智能调度参与"]'
    )
    assert.ok(primarySwitch)
    assert.ok(standbySwitch)
    assert.equal(primarySwitch.hasAttribute('data-disabled'), false)
    assert.equal(standbySwitch.hasAttribute('data-disabled'), false)

    await act(async () => {
      standbySwitch.click()
    })
    assert.deepEqual(updates, [
      { channelId: standbyChannel.id, excluded: true },
    ])

    await act(async () => {
      root.render(renderChannelView(true))
    })
    assert.equal(primarySwitch.hasAttribute('data-disabled'), true)
    assert.equal(standbySwitch.hasAttribute('data-disabled'), true)

    await act(async () => {
      primarySwitch.click()
    })
    assert.deepEqual(updates, [
      { channelId: standbyChannel.id, excluded: true },
    ])

    await act(async () => root.unmount())
    container.remove()
  })

  test('keeps manual upstream ratio editing out of the operation column', () => {
    const cells = getTableCells(renderView(createChannel()))
    const operationCell = cells[8] ?? ''

    assert.equal(operationCell.includes('aria-label="修改渠道倍率"'), false)
    assert.equal(operationCell.includes('aria-label="记录渠道倍率"'), false)
  })

  test('exposes the system disable reason from the status badge', () => {
    const markup = renderView(
      createChannel({
        status: 3,
        status_reason: '渠道监控：上游倍率或余额更新失败',
      })
    )

    assert.ok(markup.includes('系统禁用'))
    assert.ok(
      markup.includes(
        'aria-label="系统禁用，原因：渠道监控：上游倍率或余额更新失败"'
      )
    )
    assert.ok(markup.includes('tabindex="0"'))
  })
})

after(() => {
  domWindow.close()
})
