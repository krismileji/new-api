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

import type { ChannelMonitorCostOverview } from '../../types'
import { CostHistoryData } from '../channel-monitor-cost-history-dialog'

function createOverview(): ChannelMonitorCostOverview {
  return {
    days: 30,
    generated_at: 1_752_777_845,
    today_cost_cny: 1.2,
    yesterday_cost_cny: 0.8,
    total_cost_cny: 2,
    coverage: {
      included_channel_count: 1,
      unresolved_channel_count: 0,
      free_group_channel_count: 0,
    },
    items: [
      {
        date: '2026-07-23',
        start_at: 1_752_681_600,
        cost_cny: 1.2,
        unresolved_count: 0,
      },
    ],
    chart_items: [
      {
        date: '2026-07-23',
        start_at: 1_752_681_600,
        cost_cny: 1.2,
        unresolved_count: 0,
      },
    ],
    item_total: 10,
    item_page: 1,
    item_page_size: 7,
    item_page_count: 2,
    channels: [],
    api_keys: [
      {
        id: 1,
        api_key_id: 7,
        api_key_name: '测试 API Key',
        api_key: '',
        channels: [],
        cost_cny: 1.2,
        settled_count: 1,
        unresolved_count: 0,
      },
    ],
  }
}

describe('channel monitor cost history dialog layout', () => {
  test('separates API Key costs into a dedicated view', () => {
    const markup = renderToStaticMarkup(
      <CostHistoryData overview={createOverview()} />
    )

    assert.ok(markup.includes('成本趋势'))
    assert.ok(markup.includes('API Key 明细'))
    assert.ok(markup.includes('日期第 1 / 2 页'))
  })
})
