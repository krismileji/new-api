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

import { renderToStaticMarkup } from 'react-dom/server'

import { CHANNEL_STATUS } from '@/features/channels/constants'

import type { ChannelMonitorTodaySuccessResult } from '../../types'
import { ChannelMonitorTodayCacheWriteDialogContent } from '../channel-monitor-today-cache-write-dialog'

const noop = () => {}

function createResult(
  overrides: Partial<ChannelMonitorTodaySuccessResult> = {}
): ChannelMonitorTodaySuccessResult {
  return {
    days: 1,
    generated_at: 1_752_777_845,
    day_start: 1_752_681_600,
    detail_date: '2026-07-23',
    success_metrics_available: true,
    cache_write_metrics_available: true,
    summary: {
      actual_success_count: 0,
      actual_failure_count: 0,
      actual_sample_count: 0,
      actual_success_rate: 0,
      final_success_count: 0,
      final_failure_count: 0,
      final_sample_count: 0,
      final_success_rate: 0,
      cache_hit_count: 0,
      cache_sample_count: 0,
      cache_hit_rate: 0,
    },
    channel_items: [],
    api_key_items: [],
    cache_write_items: [
      {
        channel_id: 7,
        channel_name: '渠道一',
        channel_remark: '停用线路',
        request_count: 2,
      },
      {
        channel_id: 8,
        channel_name: '渠道二',
        channel_remark: '',
        request_count: 1,
      },
      {
        channel_id: 9,
        channel_name: '渠道三',
        channel_remark: '低倍率线路',
        request_count: 4,
      },
      {
        channel_id: 10,
        channel_name: '已删除渠道',
        channel_remark: '历史记录',
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
      status_reason: '手动停用',
      cost_ratio: 0.2,
      channel_remark: '停用线路',
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
    React.ComponentProps<typeof ChannelMonitorTodayCacheWriteDialogContent>
  > = {}
) {
  return renderToStaticMarkup(
    <ChannelMonitorTodayCacheWriteDialogContent
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

describe('channel monitor today cache write overview', () => {
  test('lists enabled channels first and then sorts by ascending cost ratio', () => {
    const markup = renderDialogContent()
    const cells = getTableCells(markup)

    assert.equal(cells.length, 16)
    assert.ok(markup.includes('缓存写渠道'))
    assert.ok(markup.includes('4 个'))
    assert.ok(markup.includes('10 次'))

    assert.ok(cells[0]?.includes('渠道三'))
    assert.ok(cells[1]?.includes('低倍率线路'))
    assert.ok(cells[2]?.includes('0.5'))
    assert.ok(cells[3]?.includes('4'))

    assert.ok(cells[4]?.includes('渠道二'))
    assert.ok(cells[6]?.includes('1.5'))
    assert.ok(cells[7]?.includes('1'))

    assert.ok(cells[8]?.includes('渠道一'))
    assert.ok(cells[8]?.includes('手动禁用'))
    assert.ok(cells[10]?.includes('0.2'))

    assert.ok(cells[12]?.includes('已删除渠道'))
    assert.ok(cells[13]?.includes('历史记录'))
    assert.match(cells[14] ?? '', />-<\/td>/)
  })

  test('uses the remaining dialog height as the vertical scroll container', () => {
    const markup = renderDialogContent()
    const contentRoot = markup.match(/^<div\b[^>]*>/)?.[0] ?? ''

    assert.ok(contentRoot.includes('min-h-0'))
    assert.ok(contentRoot.includes('flex-1'))
    assert.ok(contentRoot.includes('overflow-y-auto'))
    assert.ok(contentRoot.includes('overscroll-contain'))
  })

  test('keeps channel details from shrinking out of view', () => {
    const markup = renderDialogContent()

    assert.match(
      markup,
      /data-slot="today-cache-write-channel-details"[^>]*class="[^"]*shrink-0/
    )
  })

  test('shows an empty state when today has no cache writes', () => {
    const markup = renderDialogContent({
      result: createResult({ cache_write_items: [] }),
    })

    assert.ok(markup.includes('所选日期暂无缓存写'))
    assert.ok(markup.includes('所选日期尚未记录包含缓存写的请求'))
    assert.equal(markup.includes('<table'), false)
  })

  test('explains that consume logs are required when metrics are unavailable', () => {
    const markup = renderDialogContent({
      result: createResult({ cache_write_metrics_available: false }),
    })

    assert.ok(markup.includes('缓存写统计不可用'))
    assert.ok(markup.includes('需要开启消费日志'))
  })

  test('shows loading and retry states without rendering stale rows', () => {
    const loadingMarkup = renderDialogContent({
      result: undefined,
      isLoading: true,
    })
    const errorMarkup = renderDialogContent({
      result: undefined,
      isError: true,
    })

    assert.ok(loadingMarkup.includes('data-slot="skeleton"'))
    assert.equal(loadingMarkup.includes('<table'), false)
    assert.ok(errorMarkup.includes('缓存写统计加载失败'))
    assert.ok(errorMarkup.includes('重新加载'))
  })
})
