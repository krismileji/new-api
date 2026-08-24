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

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, test, vi } from 'vitest'

import { CHANNEL_STATUS } from '@/features/channels/constants'

import type {
  ChannelMonitorSuccessSummary,
  ChannelMonitorTodaySuccessResult,
} from '../../types'
import { ChannelMonitorSuccessAPIKeyTable } from '../channel-monitor-success-api-key-table'
import { ChannelMonitorTodaySuccessCard } from '../channel-monitor-today-success-card'
import { ChannelMonitorTodaySuccessDialogContent } from '../channel-monitor-today-success-dialog'

vi.mock('@visactor/react-vchart', () => ({
  VChart: () => null,
}))

const noop = () => {}

function createSummary(
  overrides: Partial<ChannelMonitorSuccessSummary> = {}
): ChannelMonitorSuccessSummary {
  return {
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
    cache_read_tokens: 50,
    input_tokens: 100,
    cache_utilization_rate: 0.5,
    ...overrides,
  }
}

function createResult(
  overrides: Partial<ChannelMonitorTodaySuccessResult> = {}
): ChannelMonitorTodaySuccessResult {
  return {
    days: 1,
    generated_at: 1_752_777_845,
    data_cutoff_at: 1_752_777_840,
    processed_at: 1_752_777_845,
    event_watermark: 42,
    queue_depth: 0,
    realtime_degraded: false,
    day_start: 1_752_681_600,
    detail_date: '2026-07-23',
    success_metrics_available: true,
    cache_write_metrics_available: true,
    summary: createSummary(),
    channel_items: [
      {
        channel_id: 7,
        channel_name: '渠道一',
        channel_remark: '主线路',
        ...createSummary({
          actual_success_count: 4,
          actual_failure_count: 0,
          actual_sample_count: 4,
          actual_success_rate: 1,
          cache_hit_count: 0,
          cache_sample_count: 0,
          cache_hit_rate: 0,
          cache_read_tokens: 0,
          input_tokens: 0,
          cache_utilization_rate: 0,
        }),
      },
      {
        channel_id: 8,
        channel_name: '渠道二',
        channel_remark: '',
        ...createSummary({
          actual_success_count: 3,
          actual_failure_count: 0,
          actual_sample_count: 3,
          actual_success_rate: 1,
          cache_hit_count: 1,
          cache_sample_count: 2,
          cache_hit_rate: 0.5,
          cache_read_tokens: 50,
          input_tokens: 100,
          cache_utilization_rate: 0.5,
        }),
      },
      {
        channel_id: 9,
        channel_name: '渠道三',
        channel_remark: '低倍率线路',
        ...createSummary({
          actual_success_count: 2,
          actual_failure_count: 1,
          actual_sample_count: 3,
          actual_success_rate: 2 / 3,
          cache_hit_count: 0,
          cache_sample_count: 0,
          cache_hit_rate: 0,
          cache_read_tokens: 0,
          input_tokens: 0,
          cache_utilization_rate: 0,
        }),
      },
    ],
    api_key_items: [
      {
        api_key_id: 21,
        api_key_name: '生产 Key',
        ...createSummary({
          actual_success_count: 3,
          actual_failure_count: 1,
          actual_sample_count: 4,
          actual_success_rate: 0.75,
          cache_hit_count: 1,
          cache_sample_count: 2,
          cache_hit_rate: 0.5,
          cache_read_tokens: 50,
          input_tokens: 100,
          cache_utilization_rate: 0.5,
        }),
      },
      {
        api_key_id: 22,
        api_key_name: '高成功率 Key',
        ...createSummary({
          actual_success_count: 3,
          actual_failure_count: 0,
          actual_sample_count: 3,
          actual_success_rate: 1,
          cache_hit_count: 0,
          cache_sample_count: 2,
          cache_hit_rate: 0,
          cache_read_tokens: 0,
          input_tokens: 100,
          cache_utilization_rate: 0,
        }),
      },
      {
        api_key_id: 23,
        api_key_name: '高缓存利用率 Key',
        ...createSummary({
          actual_success_count: 3,
          actual_failure_count: 0,
          actual_sample_count: 3,
          actual_success_rate: 1,
          cache_hit_count: 2,
          cache_sample_count: 2,
          cache_hit_rate: 1,
          cache_read_tokens: 100,
          input_tokens: 100,
          cache_utilization_rate: 1,
        }),
      },
    ],
    cache_write_items: [
      {
        channel_id: 7,
        channel_name: '渠道一',
        channel_remark: '主线路',
        request_count: 4,
      },
      {
        channel_id: 8,
        channel_name: '渠道二',
        channel_remark: '',
        request_count: 3,
      },
    ],
    chart_items: [],
    ...overrides,
  }
}

function createChannels() {
  return [
    {
      id: 7,
      name: '渠道一',
      status: CHANNEL_STATUS.MANUAL_DISABLED,
      status_reason: '',
      cost_ratio: 0.2,
      channel_remark: '主线路',
    },
    {
      id: 8,
      name: '渠道二',
      status: CHANNEL_STATUS.ENABLED,
      status_reason: '',
      cost_ratio: 1.5,
      channel_remark: '',
    },
    {
      id: 9,
      name: '渠道三',
      status: CHANNEL_STATUS.ENABLED,
      status_reason: '',
      cost_ratio: 0.5,
      channel_remark: '低倍率线路',
    },
  ]
}

function renderDialogContent(
  overrides: Partial<
    React.ComponentProps<typeof ChannelMonitorTodaySuccessDialogContent>
  > = {}
) {
  return renderToStaticMarkup(
    <ChannelMonitorTodaySuccessDialogContent
      result={createResult()}
      channels={createChannels()}
      isLoading={false}
      isError={false}
      isFetching={false}
      onRetry={noop}
      {...overrides}
    />
  )
}

function getTableCells(markup: string) {
  return markup.match(/<td\b[\s\S]*?<\/td>/g) ?? []
}

function getTables(markup: string) {
  return markup.match(/<table\b[\s\S]*?<\/table>/g) ?? []
}

describe('channel monitor today success overview', () => {
  test('exposes the whole summary card as a keyboard-focusable button', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={createResult()}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.match(markup, /^<div\b/)
    assert.ok(markup.includes('role="button"'))
    assert.ok(markup.includes('tabindex="0"'))
    assert.ok(markup.includes('今日实时概览'))
    assert.ok(markup.includes('成功率'))
    assert.ok(markup.includes('缓存利用率'))
    assert.ok(markup.includes('90%'))
    assert.ok(markup.includes('50%'))
    assert.ok(markup.includes('10 请求'))
    assert.ok(markup.includes('缓存 50 / 100 tokens'))
    assert.ok(markup.includes('写入 2 个渠道 / 7 次'))
    assert.ok(markup.includes('sm:h-36'))
    assert.ok(markup.includes('缓存口径'))
    assert.ok(markup.includes('选择缓存利用率 API Key'))
    assert.ok(markup.includes('全部 API Key'))
    assert.ok(
      markup.includes(
        'aria-label="查看今日成功率、缓存利用率和缓存写明细，成功率 90%，缓存利用率 50%，缓存利用率口径 全部 API Key，缓存写渠道 2 个，缓存写请求 7 次"'
      )
    )
  })

  test('keeps the detail trigger keyboard-focusable after adding the API Key filter', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={createResult()}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.match(markup, /role="button" tabindex="0"/)
  })

  test('keeps the summary label and values from colliding with the card action', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={createResult()}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.match(
      markup,
      /data-slot="today-success-metrics"[^>]*class="[^"]*grid-cols-2[^"]*gap-3/
    )
    assert.match(
      markup,
      /data-slot="today-success-metric-cache"[^>]*class="[^"]*border-s[^"]*ps-3/
    )
    assert.match(markup, /title="今日实时概览"/)
  })

  test('shows a dash on the card when today has no input tokens', () => {
    const result = createResult({
      summary: createSummary({
        cache_hit_count: 0,
        cache_sample_count: 0,
        cache_hit_rate: 0,
        cache_read_tokens: 0,
        input_tokens: 0,
        cache_utilization_rate: 0,
      }),
    })
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={result}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.match(markup, /data-slot="today-cache-utilization">-[\s\n]*<\/span>/)
    assert.doesNotMatch(markup, />0%<\/span>/)
  })

  test('disables API Key cache-utilization filtering when no API Key metrics exist', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={createResult({ api_key_items: [] })}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.match(markup, /aria-label="选择缓存利用率 API Key"/)
    assert.match(markup, /data-disabled=""[^>]*disabled=""/)
  })

  test('shows unavailable cache-write counts without hiding success metrics', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorTodaySuccessCard
        result={createResult({ cache_write_metrics_available: false })}
        isLoading={false}
        isError={false}
        onOpen={noop}
      />
    )

    assert.ok(markup.includes('90%'))
    assert.ok(markup.includes('50%'))
    assert.ok(markup.includes('缓存写渠道 -，缓存写请求 -'))
  })

  test('shows channel metadata and orders channels by ascending cost ratio', () => {
    const markup = renderDialogContent()
    const tables = getTables(markup)
    const channelCells = getTableCells(tables[0] ?? '')

    assert.equal(tables.length, 1)
    assert.ok(markup.includes('渠道明细'))
    assert.ok(markup.includes('API Key 明细'))
    assert.ok((tables[0] ?? '').includes('备注'))
    assert.ok((tables[0] ?? '').includes('成本倍率'))
    assert.ok((tables[0] ?? '').includes('成功率'))
    assert.ok((tables[0] ?? '').includes('缓存利用率'))
    assert.ok((tables[0] ?? '').includes('写入请求数'))
    assert.ok(markup.includes('按成功率排序'))
    assert.ok(markup.includes('按缓存利用率排序'))
    assert.ok(markup.includes('按写入请求数排序'))
    assert.equal(channelCells.length, 21)

    assert.ok(channelCells[0]?.includes('渠道一'))
    assert.ok(channelCells[0]?.includes('ID 7'))
    assert.ok(channelCells[0]?.includes('手动禁用'))
    assert.ok(channelCells[1]?.includes('主线路'))
    assert.ok(channelCells[2]?.includes('0.2'))
    assert.equal(channelCells[6]?.replaceAll(/<[^>]+>/g, ''), '4')

    assert.ok(channelCells[7]?.includes('渠道三'))
    assert.ok(channelCells[7]?.includes('ID 9'))
    assert.ok(channelCells[8]?.includes('低倍率线路'))
    assert.ok(channelCells[9]?.includes('0.5'))
    assert.equal(channelCells[13]?.replaceAll(/<[^>]+>/g, ''), '0')

    assert.ok(channelCells[14]?.includes('渠道二'))
    assert.ok(channelCells[14]?.includes('ID 8'))
    assert.match(channelCells[15] ?? '', />-<\/span><\/td>/)
    assert.ok(channelCells[16]?.includes('1.5'))
    assert.ok(channelCells[18]?.includes('100%'))
    assert.ok(channelCells[19]?.includes('50%'))
    assert.ok(channelCells[19]?.includes('text-foreground'))
    assert.equal(channelCells[20]?.replaceAll(/<[^>]+>/g, ''), '3')
  })

  test('keeps channel detail columns inside the dialog width', () => {
    const markup = renderDialogContent()
    const channelTable = getTables(markup)[0] ?? ''

    assert.ok(markup.includes('class="rounded-lg border"'))
    assert.ok(channelTable.includes('table-fixed'))
    assert.ok(channelTable.includes('w-full'))
    assert.ok(channelTable.includes('[&amp;_td]:overflow-hidden'))
    assert.ok(channelTable.includes('[&amp;_td]:text-ellipsis'))
    assert.equal(channelTable.includes('min-w-[840px]'), false)
    assert.match(channelTable, /<th[^>]*w-\[19%\]/)
    assert.match(channelTable, /<th[^>]*w-\[22%\]/)
  })

  test('uses the remaining dialog height as the vertical scroll container', () => {
    const markup = renderDialogContent()
    const contentRoot = markup.match(/^<div\b[^>]*>/)?.[0] ?? ''

    assert.ok(contentRoot.includes('min-h-0'))
    assert.ok(contentRoot.includes('flex-1'))
    assert.ok(contentRoot.includes('overflow-y-auto'))
    assert.ok(contentRoot.includes('overscroll-contain'))
  })

  test('keeps channel and API key details from shrinking into each other', () => {
    const markup = renderDialogContent()

    assert.match(
      markup,
      /data-slot="today-success-channel-details"[^>]*class="[^"]*shrink-0/
    )
    assert.ok(markup.includes('role="tablist"'))
    assert.ok(markup.includes('API Key 明细'))
  })

  test('merges cache-write totals and per-channel counts into success details', () => {
    const markup = renderDialogContent()
    const tables = getTables(markup)
    const channelCells = getTableCells(tables[0] ?? '')

    assert.match(markup, /缓存利用率[\s\S]*缓存写渠道[\s\S]*写入请求数/)
    assert.ok(markup.includes('缓存写渠道'))
    assert.ok(markup.includes('2 个'))
    assert.ok(markup.includes('7 次'))
    assert.equal(tables.length, 1)
    assert.ok((tables[0] ?? '').includes('写入请求数'))
    assert.equal(channelCells[6]?.replaceAll(/<[^>]+>/g, ''), '4')
    assert.equal(channelCells[13]?.replaceAll(/<[^>]+>/g, ''), '0')
    assert.equal(channelCells[20]?.replaceAll(/<[^>]+>/g, ''), '3')
    assert.equal(markup.includes('缓存写渠道明细'), false)
    assert.equal(markup.includes('today-cache-write-details'), false)
  })

  test('shows unavailable cache-write values as dashes in the merged summary and table', () => {
    const markup = renderDialogContent({
      result: createResult({ cache_write_metrics_available: false }),
    })
    const channelCells = getTableCells(getTables(markup)[0] ?? '')

    assert.match(markup, /缓存写渠道[\s\S]*>-<\/span>/)
    assert.match(markup, /写入请求数[\s\S]*>-<\/span>/)
    assert.match(channelCells[6] ?? '', />-<\/td>/)
    assert.match(channelCells[13] ?? '', />-<\/td>/)
    assert.match(channelCells[20] ?? '', />-<\/td>/)
  })

  test('keeps API Key details available from the separate tab', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSuccessAPIKeyTable items={createResult().api_key_items} />
    )
    const apiKeyTable = getTables(markup)[0] ?? ''
    const apiKeyCells = getTableCells(apiKeyTable)

    assert.equal(apiKeyCells.length, 12)
    assert.ok(apiKeyTable.includes('w-full'))
    assert.ok(apiKeyTable.includes('table-fixed'))
    assert.ok(apiKeyTable.includes('[&amp;_td]:overflow-hidden'))
    assert.equal(apiKeyTable.includes('min-w-[640px]'), false)
    assert.ok(apiKeyCells[0]?.includes('高缓存利用率 Key'))
    assert.ok(apiKeyCells[2]?.includes('100%'))
    assert.ok(apiKeyCells[3]?.includes('100%'))
    assert.ok(apiKeyCells[4]?.includes('高成功率 Key'))
    assert.ok(apiKeyCells[6]?.includes('100%'))
    assert.ok(apiKeyCells[7]?.includes('0%'))
    assert.ok(apiKeyCells[8]?.includes('生产 Key'))
    assert.ok(apiKeyCells[10]?.includes('75%'))
    assert.ok(apiKeyCells[11]?.includes('50%'))
  })

  test('shows loading placeholders while the daily summary is loading', () => {
    const markup = renderDialogContent({ result: undefined, isLoading: true })

    assert.ok(markup.includes('data-slot="skeleton"'))
    assert.equal(markup.includes('<table'), false)
  })

  test('shows a retry action after the daily summary fails to load', () => {
    const markup = renderDialogContent({ result: undefined, isError: true })

    assert.ok(markup.includes('请求统计加载失败'))
    assert.ok(markup.includes('重新加载'))
    assert.match(markup, /<button\b/)
  })

  test('explains which logs are required when success metrics are unavailable', () => {
    const markup = renderDialogContent({
      result: createResult({ success_metrics_available: false }),
    })

    assert.ok(markup.includes('成功率统计不可用'))
    assert.ok(markup.includes('需要同时开启消费日志和错误日志'))
  })

  test('shows an empty state when today has no channel requests', () => {
    const markup = renderDialogContent({
      result: createResult({
        summary: createSummary({
          actual_success_count: 0,
          actual_failure_count: 0,
          actual_sample_count: 0,
          actual_success_rate: 0,
        }),
        channel_items: [],
      }),
    })

    assert.ok(markup.includes('所选日期暂无请求数据'))
    assert.ok(markup.includes('所选日期尚未记录可统计的请求'))
  })
})
