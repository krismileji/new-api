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

import { formatTimestampToDate } from '@/lib/format'

import { ChannelMonitorRealtimeStatus } from '../channel-monitor-realtime-status'

describe('channel monitor realtime status', () => {
  test('shows degraded and pending queue state with the data cutoff', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorRealtimeStatus
        metadata={{
          data_cutoff_at: 1_752_777_840,
          processed_at: 1_752_777_845,
          event_watermark: 42,
          queue_depth: 6,
          realtime_degraded: true,
        }}
      />
    )

    assert.ok(markup.includes('实时数据已降级'))
    assert.ok(markup.includes('队列待处理 6'))
    assert.ok(
      markup.includes(`数据截至 ${formatTimestampToDate(1_752_777_840)}`)
    )
    assert.ok(markup.includes('事件水位 42'))
  })

  test('states when no request event has been projected yet', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorRealtimeStatus
        metadata={{
          data_cutoff_at: 0,
          processed_at: 0,
          event_watermark: 0,
          queue_depth: 0,
          realtime_degraded: false,
        }}
      />
    )

    assert.ok(markup.includes('数据截至 暂无已处理事件'))
    assert.equal(markup.includes('实时数据已降级'), false)
    assert.equal(markup.includes('队列待处理'), false)
  })
})
