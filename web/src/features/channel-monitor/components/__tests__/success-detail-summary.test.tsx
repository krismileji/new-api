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

import type { ChannelMonitorSuccessSummary } from '../../types'
import { ChannelMonitorSuccessSummaryCards } from '../channel-monitor-success-detail-dialog'

function createSummary(
  overrides: Partial<ChannelMonitorSuccessSummary> = {}
): ChannelMonitorSuccessSummary {
  return {
    actual_success_count: 9,
    actual_failure_count: 1,
    actual_sample_count: 10,
    actual_success_rate: 0.9,
    final_success_count: 9,
    final_failure_count: 1,
    final_sample_count: 10,
    final_success_rate: 0.9,
    cache_hit_count: 1,
    cache_sample_count: 2,
    cache_hit_rate: 0.5,
    cache_read_tokens: 50,
    input_tokens: 100,
    cache_utilization_rate: 0.5,
    ...overrides,
  }
}

describe('channel monitor success detail summary', () => {
  test('shows cache utilization beside the success summary cards', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSuccessSummaryCards
        summary={createSummary()}
        mode='actual'
      />
    )

    assert.ok(markup.includes('sm:grid-cols-4'))
    assert.match(markup, /真实调用成功率[\s\S]*90%[\s\S]*缓存利用率[\s\S]*50%/)
  })

  test('shows a dash when the success detail has no input tokens', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSuccessSummaryCards
        summary={createSummary({
          cache_hit_count: 0,
          cache_sample_count: 0,
          cache_hit_rate: 0,
          cache_read_tokens: 0,
          input_tokens: 0,
          cache_utilization_rate: 0,
        })}
        mode='actual'
      />
    )

    assert.match(markup, /缓存利用率<\/span><span[^>]*>\s*-\s*<\/span>/)
    assert.doesNotMatch(markup, /缓存利用率[\s\S]*>0%<\/span>/)
  })
})
