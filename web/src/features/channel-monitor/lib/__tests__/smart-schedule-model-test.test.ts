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
import { test } from 'node:test'

import type { ChannelMonitorSmartScheduleModelTestResult } from '../../types'
import { mergeChannelMonitorModelTestRetry } from '../smart-schedule-model-test'

function result(
  channelId: number,
  status: 'success' | 'failure' | 'skipped'
): ChannelMonitorSmartScheduleModelTestResult {
  return {
    group: 'vip',
    model: 'cache-model',
    stream: true,
    endpoint_type: 'auto',
    total: 1,
    succeeded: status === 'success' ? 1 : 0,
    failed: status === 'failure' ? 1 : 0,
    skipped: status === 'skipped' ? 1 : 0,
    results: [
      {
        channel_id: channelId,
        channel_name: `渠道 ${channelId}`,
        participates: true,
        available: true,
        status,
        total_ms: 100,
      },
    ],
  }
}

test('replaces one retried channel and recalculates pool result counts', () => {
  const current = result(7, 'failure')
  current.results.push(result(8, 'success').results[0])
  current.total = 2
  current.succeeded = 1

  const merged = mergeChannelMonitorModelTestRetry(
    current,
    result(7, 'success')
  )

  assert.equal(merged.total, 2)
  assert.equal(merged.succeeded, 2)
  assert.equal(merged.failed, 0)
  assert.equal(merged.skipped, 0)
  assert.equal(
    merged.results.find((item) => item.channel_id === 7)?.status,
    'success'
  )
})
