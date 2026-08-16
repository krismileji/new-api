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

import { ChannelMonitorPerformanceCoverageAlert } from '../channel-monitor-performance-coverage-alert'

const incompleteCoverage = {
  aggregation_enabled: true,
  aggregated_from: 1_752_776_000,
  aggregated_through: 1_752_777_800,
  window_start: 1_752_774_200,
  window_complete: false,
}

describe('channel monitor performance coverage alert', () => {
  test('warns that visible metrics may be low when the requested window is incomplete', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={incompleteCoverage}
        rangeLabel='近60分钟'
      />
    )

    assert.ok(markup.includes('近60分钟统计窗口数据尚未覆盖完整'))
    assert.ok(markup.includes('当前请求数、成功率和性能数据可能偏低'))
    assert.ok(markup.includes('接口未返回具体故障原因'))
  })

  test('lists every reported degradation reason with backlog details', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={incompleteCoverage}
        metadata={{
          data_cutoff_at: 1_752_777_800,
          processed_at: 1_752_777_810,
          event_watermark: 42,
          queue_depth: 3,
          redis_status: 'available',
          redis_available: true,
          redis_consumer_running: true,
          pending_count: 3,
          oldest_pending_at: 1_752_777_755,
          consumer_lag_seconds: 45,
          degraded_reasons: [
            'event_backlog',
            'publisher_unavailable',
            'marker_release_failure',
          ],
          realtime_degraded: true,
        }}
        rangeLabel='近60分钟'
      />
    )

    assert.ok(markup.includes('其中 3 条已交付但尚未确认'))
    assert.ok(markup.includes('当前延迟 45 秒'))
    assert.ok(markup.includes('最近一次实时事件发布失败'))
    assert.ok(markup.includes('聚合副作用标记释放失败'))
  })

  test('stays hidden after the requested window is fully covered', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorPerformanceCoverageAlert
        coverage={{ ...incompleteCoverage, window_complete: true }}
        rangeLabel='近60分钟'
      />
    )

    assert.equal(markup, '')
  })
})
