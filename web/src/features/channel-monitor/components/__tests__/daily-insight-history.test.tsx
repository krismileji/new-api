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

import type { ChannelMonitorDailyInsightDay } from '../../types'
import { ChannelMonitorDailyInsightHistory } from '../channel-monitor-daily-insight-history'

const items: ChannelMonitorDailyInsightDay[] = [
  {
    date: '2026-07-22',
    start_at: 1,
    request_count: 10,
    success_rate: 0.9,
    cache_sample_count: 8,
    cache_rate: 0.4,
    cache_write_channel_count: 2,
    cache_write_request_count: 3,
  },
  {
    date: '2026-07-23',
    start_at: 2,
    request_count: 12,
    success_rate: 1,
    cache_sample_count: 10,
    cache_rate: 0.5,
    cache_write_channel_count: 1,
    cache_write_request_count: 4,
  },
]

describe('channel monitor daily insight history', () => {
  test('shows rates and cache-write requests in one shared chart', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorDailyInsightHistory
        days={30}
        selectedDate='2026-07-23'
        items={items}
        loading={false}
        onDaysChange={() => {}}
        onDateChange={() => {}}
      />
    )

    assert.ok(markup.includes('aria-label="请求与缓存统计范围"'))
    assert.ok(markup.includes('aria-label="请求与缓存明细日期"'))
    assert.ok(
      markup.includes('aria-label="每日成功率、缓存率与缓存写请求组合图"')
    )
    assert.equal((markup.match(/role="img"/g) ?? []).length, 1)
    assert.equal(markup.includes('每日缓存写请求柱状图'), false)
    assert.ok(markup.includes('2026-07-23'))
  })

  test('keeps the date controls visible while history is loading', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorDailyInsightHistory
        days={30}
        selectedDate='2026-07-23'
        items={[]}
        loading
        onDaysChange={() => {}}
        onDateChange={() => {}}
      />
    )

    assert.ok(markup.includes('aria-label="请求与缓存统计范围"'))
    assert.equal((markup.match(/data-slot="skeleton"/g) ?? []).length, 1)
  })
})
