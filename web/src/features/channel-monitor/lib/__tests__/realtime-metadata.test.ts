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
})
