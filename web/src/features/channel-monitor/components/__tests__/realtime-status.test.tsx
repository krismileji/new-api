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
import { describe, test } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { ChannelMonitorRealtimeStatus } from '../channel-monitor-realtime-status'

describe('channel monitor realtime status', () => {
  test('shows degraded and pending queue state with the data cutoff', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorRealtimeStatus
        metadata={{
          generated_at: 1_752_777_845,
          data_cutoff_at: 1_752_777_840,
          processed_at: 1_752_777_845,
          event_watermark: 42,
          queue_depth: 6,
          redis_status: 'available',
          redis_available: true,
          redis_consumer_running: true,
          pending_count: 6,
          cost_queue_pending_count: 3,
          cost_stream_pending_count: 2,
          cost_stream_unread_count: 4,
          cost_outbox_pending_count: 5,
          cost_outbox_oldest_pending_at: 1_752_777_790,
          cost_outbox_retry_count: 7,
          cost_ledger_failed_count: 3,
          cost_publish_failed_count: 1,
          cost_dead_letter_count: 2,
          oldest_pending_at: 1_752_777_800,
          consumer_lag_seconds: 45,
          last_published_at: 1_752_777_830,
          last_processed_at: 1_752_777_845,
          retry_count: 3,
          takeover_count: 2,
          quarantine_count: 1,
          last_quarantined_at: 1_752_777_820,
          marker_release_failure_count: 4,
          marker_release_failure_active: true,
          stream_trim_failure_count: 5,
          stream_trim_failure_active: true,
          realtime_degraded: true,
        }}
      />
    )

    assert.ok(markup.includes('实时数据已降级'))
    assert.ok(
      markup.includes(`查询时间 ${formatTimestampToDate(1_752_777_845)}`)
    )
    assert.ok(markup.includes('Redis 正常'))
    assert.ok(markup.includes('事件处理 运行中'))
    assert.ok(markup.includes('实时事件待处理 6'))
    assert.ok(markup.includes('已聚合成本待写入 3'))
    assert.ok(markup.includes('成本事件未读取 4 / 待确认 2'))
    assert.ok(markup.includes('待记入成本账本 5'))
    assert.ok(markup.includes(formatTimestampToDate(1_752_777_790)))
    assert.ok(markup.includes('最早待记账成本'))
    assert.ok(markup.includes('成本账本写入失败'))
    assert.ok(markup.includes('成本账本写入失败 3 次'))
    assert.ok(markup.includes('成本事件排队失败'))
    assert.ok(markup.includes('成本事件进入异常队列'))
    assert.ok(markup.includes('成本账本写入重试 7 次'))
    assert.ok(markup.includes('成本异常事件 2 条'))
    assert.ok(markup.includes('处理延迟 45 秒'))
    assert.ok(markup.includes('处理重试 3 次'))
    assert.ok(markup.includes('自动接管 2 次'))
    assert.ok(markup.includes('异常隔离 1 条'))
    assert.ok(markup.includes(formatTimestampToDate(1_752_777_820)))
    assert.ok(markup.includes('事件标记清理故障'))
    assert.ok(markup.includes('实时事件清理故障'))
    assert.ok(markup.includes('事件标记清理失败 4 次'))
    assert.ok(markup.includes('实时事件清理失败 5 次'))
    assert.ok(
      markup.includes(`数据截至 ${formatTimestampToDate(1_752_777_840)}`)
    )
    assert.ok(markup.includes('已处理事件序号 42'))
  })

  test('states when no request event has been projected yet', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorRealtimeStatus
        metadata={{
          data_cutoff_at: 0,
          processed_at: 0,
          event_watermark: 0,
          queue_depth: 0,
          redis_status: 'unavailable',
          redis_available: false,
          redis_consumer_running: false,
          pending_count: 0,
          cost_queue_pending_count: 0,
          cost_stream_pending_count: 0,
          cost_stream_unread_count: 0,
          cost_outbox_pending_count: 0,
          cost_ledger_failed_count: 0,
          cost_publish_failed_count: 0,
          cost_dead_letter_count: 0,
          realtime_degraded: false,
        }}
      />
    )

    assert.ok(markup.includes('数据截至 暂无已处理事件'))
    assert.equal(markup.includes('实时数据已降级'), false)
    assert.ok(markup.includes('Redis 故障'))
    assert.ok(markup.includes('事件处理 已停止'))
    assert.equal(markup.includes('实时事件待处理'), false)
    assert.ok(markup.includes('已聚合成本待写入 0'))
    assert.ok(markup.includes('成本事件未读取 0 / 待确认 0'))
    assert.ok(markup.includes('待记入成本账本 0'))
    assert.equal(markup.includes('成本账本写入失败</'), false)
    assert.equal(markup.includes('成本事件排队失败</'), false)
    assert.equal(markup.includes('成本事件进入异常队列'), false)
  })
})
