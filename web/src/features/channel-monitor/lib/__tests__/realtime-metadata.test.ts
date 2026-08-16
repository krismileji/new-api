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

import { mergeChannelMonitorRealtimeMetadata } from '../realtime-metadata'

describe('channel monitor realtime metadata', () => {
  test('uses the oldest cutoff and most severe queue state across page snapshots', () => {
    const merged = mergeChannelMonitorRealtimeMetadata([
      {
        data_cutoff_at: 200,
        processed_at: 210,
        event_watermark: 20,
        queue_depth: 1,
        realtime_degraded: false,
      },
      {
        data_cutoff_at: 180,
        processed_at: 190,
        event_watermark: 18,
        queue_depth: 7,
        realtime_degraded: true,
      },
    ])

    assert.deepEqual(merged, {
      data_cutoff_at: 180,
      processed_at: 190,
      event_watermark: 18,
      queue_depth: 7,
      realtime_degraded: true,
    })
  })

  test('ignores missing snapshots and preserves zero metadata', () => {
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

  test('keeps Redis failure state regardless of snapshot order', () => {
    const unavailable = {
      data_cutoff_at: 180,
      processed_at: 190,
      event_watermark: 18,
      queue_depth: 1,
      redis_status: 'unavailable' as const,
      redis_available: false,
      redis_consumer_running: false,
      marker_release_failure_count: 2,
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
      marker_release_failure_active: false,
      stream_trim_failure_count: 3,
      stream_trim_failure_active: true,
      realtime_degraded: false,
    }

    for (const snapshots of [
      [unavailable, available],
      [available, unavailable],
    ]) {
      const merged = mergeChannelMonitorRealtimeMetadata(snapshots)
      assert.equal(merged?.redis_status, 'unavailable')
      assert.equal(merged?.redis_available, false)
      assert.equal(merged?.redis_consumer_running, false)
      assert.equal(merged?.marker_release_failure_count, 2)
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
