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

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { formatTimestampToDate } from '@/lib/format'

import { formatChannelMonitorCost, formatMonitorRatio } from '../../lib/format'
import type { ChannelMonitorItem } from '../../types'
import { ChannelMonitorChannelView } from '../channel-monitor-channel-view'

const noop = () => {}
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
    smart_schedule_excluded: false,
    last_schedule_status: '',
    last_schedule_error: '',
    last_schedule_score: null,
    last_schedule_priority: 0,
    last_schedule_weight: 0,
    last_schedule_time: 0,
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

function renderView(channel: ChannelMonitorItem) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={testI18n}>
      <ChannelMonitorChannelView
        channels={[channel]}
        groupRatios={{ default: 1 }}
        groupCoefficients={{ default: 1 }}
        performanceByChannel={new Map()}
        successByChannel={new Map()}
        successMetricsAvailable={false}
        performanceRangeLabel='24 小时'
        performanceLoading={false}
        performanceError={false}
        onFetchUpstreamBalance={noop}
        onFetchUpstreamRatio={noop}
        onToggleStatus={noop}
        onTestConnection={noop}
        onEditRatio={noop}
        onEditGroups={noop}
        onConfigureUpstream={noop}
        onViewHistory={noop}
        onOpenCostHistory={noop}
        onOpenSuccessDetail={noop}
        onUpdateSmartSchedule={noop}
        smartScheduleEnabled={false}
        fetchingBalanceChannelId={null}
        fetchingRatioChannelId={null}
        updatingStatusChannelId={null}
        updatingSmartScheduleChannelId={null}
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
  test('shows ratio, group, and update time without updater attribution', () => {
    const channel = createChannel()
    const markup = renderView(channel)
    const cells = getTableCells(markup)

    assert.equal(cells.length, 9)
    assert.doesNotMatch(markup, /<th\b[^>]*>更新时间<\/th>/)
    assert.ok(markup.indexOf('上游余额') < markup.indexOf('今日成本'))
    assert.ok(markup.indexOf('今日成本') < markup.indexOf('成本倍率'))
    assert.ok(
      cells[1]?.includes(
        `更新：${formatTimestampToDate(channel.last_balance_time)}`
      )
    )
    assert.ok(
      cells[3]?.includes(`更新：${formatTimestampToDate(channel.updated_time)}`)
    )
    assert.ok(cells[3]?.includes('上游分组：default'))
    assert.equal(cells[3]?.includes(channel.updated_by_username), false)
  })

  test('places upstream refresh actions before their metric text with compact spacing', () => {
    const channel = createChannel()
    const markup = renderView(channel)
    const cells = getTableCells(markup)
    const headers = getTableHeaders(markup)
    const balanceCell = cells[1] ?? ''
    const ratioCell = cells[3] ?? ''

    assert.ok(
      balanceCell.indexOf('aria-label="更新上游余额"') <
        balanceCell.indexOf('42.5')
    )
    assert.ok(
      ratioCell.indexOf('aria-label="更新上游倍率"') <
        ratioCell.indexOf(formatMonitorRatio(channel.cost_ratio))
    )
    assert.match(balanceCell, /grid-cols-\[24px_minmax\(0,1fr\)\]/)
    assert.match(ratioCell, /grid-cols-\[24px_minmax\(0,1fr\)\]/)
    assert.match(balanceCell, /col-start-2/)
    assert.match(ratioCell, /col-start-2/)
    assert.ok(headers[1]?.includes('pl-[34px]'))
    assert.ok(headers[3]?.includes('pl-[34px]'))
  })

  test('does not show conversion or sync status details in the ratio cell', () => {
    const channel = createChannel()
    assert.ok(channel.upstream)
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

    assert.equal(cells[3]?.includes('换算'), false)
    assert.equal(cells[3]?.includes('倍率同步已关闭'), false)
    assert.ok(cells[3]?.includes('上游分组：default'))
  })

  test('truncates long cell metadata and exposes the full values on hover', () => {
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
    const fetchTimestamp = formatTimestampToDate(channel.last_fetch_time)

    assert.equal(cells.length, 9)
    assert.ok(cells[0]?.includes(`title="${longChannelName}"`))
    assert.ok(cells[1]?.includes(`title="更新：${balanceTimestamp}"`))
    assert.ok(cells[3]?.includes(`title="更新：${ratioTimestamp}"`))
    assert.ok(cells[3]?.includes(`title="上游分组：${longUpstreamGroup}"`))
    assert.ok(cells[4]?.includes('title="连续失败 942 次"'))
    assert.ok(cells[4]?.includes(`title="最后尝试：${fetchTimestamp}"`))
    assert.ok(cells[5]?.includes(`title="${longGroup}"`))
    assert.match(cells[1] ?? '', /truncate/)
    assert.match(cells[3] ?? '', /truncate/)
    assert.match(cells[4] ?? '', /truncate/)
    assert.match(cells[5] ?? '', /truncate/)
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
    assert.equal(cells[3]?.includes('更新：'), false)
  })

  test('shows the settled amount in the column after upstream balance', () => {
    const channel = createChannel()
    const cells = getTableCells(renderView(channel))

    assert.ok(cells[2]?.includes(formatChannelMonitorCost(1.23456)))
    assert.equal(cells[2]?.includes('不完整'), false)
    assert.match(cells[2] ?? '', /<button\b/)
    assert.ok(cells[2]?.includes('查看渠道 测试渠道 的今日成本详情'))
  })

  test('shows an explicit state when cost conversion is not configured', () => {
    const cells = getTableCells(
      renderView(createChannel({ today_cost_configured: false }))
    )

    assert.ok(cells[2]?.includes('未配置'))
    assert.match(cells[2] ?? '', /<button\b/)
    assert.ok(cells[2]?.includes('查看渠道 测试渠道 的今日成本详情'))
  })

  test('keeps zero visible and marks totals with unresolved settlements', () => {
    const cells = getTableCells(
      renderView(
        createChannel({
          today_cost_cny: 0,
          today_cost_complete: false,
          today_cost_unresolved_count: 2,
        })
      )
    )

    assert.ok(cells[2]?.includes(formatChannelMonitorCost(0)))
    assert.ok(cells[2]?.includes('不完整'))
    assert.ok(cells[2]?.includes('今日有 2 次成本未确认'))
  })
})
