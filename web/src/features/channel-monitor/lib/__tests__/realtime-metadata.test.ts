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

import { mergeChannelMonitorRealtimeMetadata } from '../realtime-metadata'

describe('channel monitor realtime metadata', () => {
  test('uses the oldest cutoff and most severe queue state across query results', () => {
    const merged = mergeChannelMonitorRealtimeMetadata([
      {
        generated_at: 205,
        data_cutoff_at: 200,
        processed_at: 210,
        event_watermark: 20,
        queue_depth: 1,
        writer_queue_depth: 2,
        writer_queue_capacity: 8,
        writer_dropped_events: 1,
        cost_stream_pending_count: 2,
        cost_stream_unread_count: 3,
        cost_outbox_pending_count: 1,
        cost_outbox_oldest_pending_at: 150,
        cost_outbox_retry_count: 4,
        cost_ledger_failed_count: 1,
        cost_publish_failed_count: 0,
        cost_dead_letter_count: 1,
        degraded_reasons: ['cost_dead_letter'],
        realtime_degraded: false,
      },
      {
        generated_at: 185,
        data_cutoff_at: 180,
        processed_at: 190,
        event_watermark: 18,
        queue_depth: 7,
        writer_queue_depth: 5,
        writer_queue_capacity: 8,
        writer_dropped_events: 3,
        cost_stream_pending_count: 1,
        cost_stream_unread_count: 5,
        cost_outbox_pending_count: 6,
        cost_outbox_oldest_pending_at: 140,
        cost_outbox_retry_count: 7,
        cost_ledger_failed_count: 3,
        cost_publish_failed_count: 2,
        cost_dead_letter_count: 0,
        degraded_reasons: ['event_backlog', 'cost_dead_letter'],
        realtime_degraded: true,
      },
    ])

    assert.deepEqual(merged, {
      generated_at: 185,
      data_cutoff_at: 180,
      processed_at: 190,
      event_watermark: 18,
      queue_depth: 7,
      writer_queue_depth: 5,
      writer_queue_capacity: 8,
      writer_dropped_events: 3,
      cost_stream_pending_count: 2,
      cost_stream_unread_count: 5,
      cost_outbox_pending_count: 6,
      cost_outbox_oldest_pending_at: 140,
      cost_outbox_retry_count: 7,
      cost_ledger_failed_count: 3,
      cost_publish_failed_count: 2,
      cost_dead_letter_count: 1,
      degraded_reasons: ['cost_dead_letter', 'event_backlog'],
      realtime_degraded: true,
    })
  })

  test('ignores missing results and preserves zero metadata', () => {
    assert.deepEqual(
      mergeChannelMonitorRealtimeMetadata([
        undefined,
        {
          data_cutoff_at: 0,
          processed_at: 0,
          event_watermark: 0,
          queue_depth: 0,
          realtime_degraded: false,
        },
        {
          data_cutoff_at: 200,
          processed_at: 210,
          event_watermark: 20,
          queue_depth: 0,
          realtime_degraded: false,
        },
      ]),
      {
        data_cutoff_at: 0,
        processed_at: 0,
        event_watermark: 0,
        queue_depth: 0,
        realtime_degraded: false,
      }
    )
  })

  test('keeps Redis failure state regardless of result order', () => {
    const unavailable = {
      data_cutoff_at: 180,
      processed_at: 190,
      event_watermark: 18,
      queue_depth: 1,
      redis_status: 'unavailable' as const,
      redis_available: false,
      redis_consumer_running: false,
      marker_release_failure_count: 2,
      runtime_marker_failure_count: 4,
      schedule_marker_failure_count: 1,
      marker_release_failure_active: true,
      stream_trim_failure_count: 1,
      stream_trim_failure_active: false,
      realtime_degraded: true,
    }
    const available = {
      data_cutoff_at: 200,
      processed_at: 210,
      event_watermark: 20,
      queue_depth: 0,
      redis_status: 'available' as const,
      redis_available: true,
      redis_consumer_running: true,
      marker_release_failure_count: 1,
      runtime_marker_failure_count: 3,
      schedule_marker_failure_count: 5,
      marker_release_failure_active: false,
      stream_trim_failure_count: 3,
      stream_trim_failure_active: true,
      realtime_degraded: false,
    }

    for (const results of [
      [unavailable, available],
      [available, unavailable],
    ]) {
      const merged = mergeChannelMonitorRealtimeMetadata(results)
      assert.equal(merged?.redis_status, 'unavailable')
      assert.equal(merged?.redis_available, false)
      assert.equal(merged?.redis_consumer_running, false)
      assert.equal(merged?.marker_release_failure_count, 2)
      assert.equal(merged?.runtime_marker_failure_count, 4)
      assert.equal(merged?.schedule_marker_failure_count, 5)
      assert.equal(merged?.marker_release_failure_active, true)
      assert.equal(merged?.stream_trim_failure_count, 3)
      assert.equal(merged?.stream_trim_failure_active, true)
      assert.equal(merged?.realtime_degraded, true)
    }
  })

  test('treats active marker and trim failures as degraded', () => {
    const merged = mergeChannelMonitorRealtimeMetadata([
      {
        data_cutoff_at: 200,
        processed_at: 210,
        event_watermark: 20,
        queue_depth: 0,
        marker_release_failure_active: true,
        stream_trim_failure_active: false,
        realtime_degraded: false,
      },
    ])

    assert.equal(merged?.marker_release_failure_active, true)
    assert.equal(merged?.realtime_degraded, true)
  })
})
