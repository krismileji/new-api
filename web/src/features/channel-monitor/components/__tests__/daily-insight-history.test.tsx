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
  test('shows range and date selectors with a success/cache chart', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorDailyInsightHistory
        kind='success-cache'
        days={30}
        selectedDate='2026-07-23'
        items={items}
        loading={false}
        onDaysChange={() => {}}
        onDateChange={() => {}}
      />
    )

    assert.ok(markup.includes('aria-label="成功率与缓存率统计范围"'))
    assert.ok(markup.includes('aria-label="成功率与缓存率明细日期"'))
    assert.ok(markup.includes('aria-label="每日成功率与缓存率柱状图"'))
    assert.ok(markup.includes('2026-07-23'))
  })

  test('shows the same date controls with a cache-write chart', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorDailyInsightHistory
        kind='cache-write'
        days={7}
        selectedDate='2026-07-22'
        items={items}
        loading={false}
        onDaysChange={() => {}}
        onDateChange={() => {}}
      />
    )

    assert.ok(markup.includes('aria-label="缓存写统计范围"'))
    assert.ok(markup.includes('aria-label="缓存写明细日期"'))
    assert.ok(markup.includes('aria-label="每日缓存写请求柱状图"'))
  })

  test('keeps the date controls visible while history is loading', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorDailyInsightHistory
        kind='success-cache'
        days={30}
        selectedDate='2026-07-23'
        items={[]}
        loading
        onDaysChange={() => {}}
        onDateChange={() => {}}
      />
    )

    assert.ok(markup.includes('aria-label="成功率与缓存率统计范围"'))
    assert.ok(markup.includes('data-slot="skeleton"'))
  })
})
