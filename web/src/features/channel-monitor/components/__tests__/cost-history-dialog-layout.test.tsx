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
import { describe, test, vi } from 'vitest'

import { getChannelMonitorChartEventDate } from '../../lib/daily-chart'
import type { ChannelMonitorCostOverview } from '../../types'
import { ChannelMonitorChannelCostTable } from '../channel-monitor-channel-cost-table'
import { CostHistoryData } from '../channel-monitor-cost-history-dialog'

vi.mock('@visactor/react-vchart', () => ({
  VChart: () => null,
}))

function createOverview(): ChannelMonitorCostOverview {
  return {
    days: 30,
    generated_at: 1_752_777_845,
    data_cutoff_at: 1_752_777_840,
    processed_at: 1_752_777_845,
    event_watermark: 42,
    queue_depth: 0,
    realtime_degraded: false,
    today_cost_cny: 1.2,
    today_probe_cost_cny: 0.3,
    today_model_detection_cost_cny: 0.2,
    yesterday_cost_cny: 0.8,
    yesterday_probe_cost_cny: 0.1,
    yesterday_model_detection_cost_cny: 0.05,
    total_cost_cny: 2,
    total_probe_cost_cny: 0.4,
    total_model_detection_cost_cny: 0.25,
    detail_date: '2026-07-23',
    coverage: {
      included_channel_count: 1,
      unresolved_channel_count: 0,
      missing_cost_config_channel_count: 0,
      free_group_channel_count: 0,
      settled_count: 1,
      unresolved_count: 0,
    },
    items: [
      {
        date: '2026-07-23',
        start_at: 1_752_681_600,
        cost_cny: 1.2,
        probe_cost_cny: 0.3,
        model_detection_cost_cny: 0.2,
        settled_count: 1,
        unresolved_count: 0,
      },
    ],
    chart_items: [
      {
        date: '2026-07-23',
        start_at: 1_752_681_600,
        cost_cny: 1.2,
        probe_cost_cny: 0.3,
        model_detection_cost_cny: 0.2,
        settled_count: 1,
        unresolved_count: 0,
      },
    ],
    item_total: 10,
    item_page: 1,
    item_page_size: 7,
    item_page_count: 2,
    channels: [
      {
        channel_id: 1,
        channel_name: '渠道一',
        channel_remark: '主力线路',
        status: 1,
        cost_ratio: 0.5,
        cost_cny: 2,
        probe_cost_cny: 0.5,
        model_detection_cost_cny: 0.25,
        settled_count: 1,
        unresolved_count: 0,
      },
    ],
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
  test('places channel and API Key costs in tabs beside the cost trend', () => {
    const markup = renderToStaticMarkup(
      <CostHistoryData overview={createOverview()} />
    )

    const trendIndex = markup.indexOf('成本趋势')
    const channelIndex = markup.indexOf('渠道汇总')
    const apiKeyIndex = markup.indexOf('API Key 明细')
    assert.ok(trendIndex >= 0)
    assert.ok(channelIndex > trendIndex)
    assert.ok(apiKeyIndex > channelIndex)
    assert.ok(markup.includes('grid-cols-3'))
    assert.ok(markup.includes('日期第 1 / 2 页'))
  })

  test('shares one selected-date control across all three cost views', () => {
    const markup = renderToStaticMarkup(
      <CostHistoryData overview={createOverview()} />
    )

    assert.equal(markup.match(/aria-label="成本明细日期"/g)?.length, 1)
    assert.ok(markup.includes('2026-07-23'))
    assert.ok(markup.includes('data-selected-date="true"'))
    assert.equal(markup.match(/aria-label="每日成本柱状图"/g)?.length, 1)
  })

  test('reads the selected date from a clicked chart bar', () => {
    assert.equal(
      getChannelMonitorChartEventDate({ datum: { date: '2026-07-23' } }),
      '2026-07-23'
    )
    assert.equal(
      getChannelMonitorChartEventDate({ datum: { cost: 1.2 } }),
      null
    )
    assert.equal(getChannelMonitorChartEventDate({}), null)
  })

  test('shows channel remarks, status, and cost ratios in the channel view', () => {
    const overview = createOverview()
    overview.channels = [
      ...overview.channels.map((channel) => ({
        ...channel,
        unresolved_count: 2,
      })),
      {
        channel_id: 2,
        channel_name: '仅未确认渠道',
        channel_remark: '成本待解析',
        status: 1,
        cost_ratio: null,
        cost_cny: 0,
        settled_count: 0,
        unresolved_count: 3,
      },
    ]
    const markup = renderToStaticMarkup(
      <ChannelMonitorChannelCostTable
        items={overview.channels}
        detailDate={overview.detail_date}
      />
    )

    assert.ok(markup.includes('渠道一'))
    assert.ok(markup.includes('主力线路'))
    assert.ok(markup.includes('启用'))
    assert.ok(markup.includes('成本倍率'))
    assert.ok(markup.includes('0.5'))
    assert.ok(markup.includes('探测成本'))
    assert.ok(markup.includes('0.5000'))
    assert.ok(markup.includes('模型检测成本'))
    assert.ok(markup.includes('0.2500'))
    assert.ok(markup.includes('仅未确认渠道'))
    assert.ok(markup.includes('成本待解析'))
    assert.ok(markup.includes('aria-label="已结算 0"'))
    assert.ok(markup.includes('aria-label="未解析 3"'))
    assert.ok(markup.includes('aria-label="解析率 0%"'))
    assert.ok(markup.includes('配置缺失'))
    assert.ok(markup.includes('min-w-[960px]'))
    assert.ok(markup.includes('grid-cols-3'))
    assert.ok(markup.includes('按渠道排序'))
    assert.ok(markup.includes('按已结算成本排序'))
    assert.ok(markup.includes('按结算排序'))
    assert.ok(markup.includes('按未解析排序'))
    assert.ok(markup.includes('按解析率排序'))
  })

  test('shows unresolved attempts and resolution rate in the trend view', () => {
    const overview = createOverview()
    overview.coverage.unresolved_channel_count = 2
    overview.coverage.unresolved_count = 2
    overview.coverage.missing_cost_config_channel_count = 1
    overview.items = overview.items.map((item) => ({
      ...item,
      unresolved_count: 2,
    }))
    overview.chart_items = overview.chart_items.map((item) => ({
      ...item,
      unresolved_count: 2,
    }))

    const markup = renderToStaticMarkup(<CostHistoryData overview={overview} />)

    assert.ok(markup.includes('已结算请求 1 · 未解析请求 2 · 解析率 33.3%'))
    assert.ok(markup.includes('当前金额不包含 2 次未解析的上游请求尝试'))
    assert.ok(markup.includes('其中 1 个渠道缺少有效成本配置'))
    assert.ok(markup.includes('已结算成本'))
    assert.ok(markup.includes('探测成本'))
    assert.ok(markup.includes('模型检测成本'))
    assert.ok(markup.includes('0.3000'))
    assert.ok(markup.includes('未解析'))
  })

  test('orders cost channels by today cost descending by default', () => {
    const overview = createOverview()
    overview.channels = [
      {
        channel_id: 4,
        channel_name: '禁用高倍率',
        channel_remark: '禁用备注',
        status: 2,
        cost_ratio: 2,
        cost_cny: 4,
        settled_count: 1,
        unresolved_count: 0,
      },
      {
        channel_id: 3,
        channel_name: '启用高倍率',
        channel_remark: '启用高倍率备注',
        status: 1,
        cost_ratio: 1.5,
        cost_cny: 3,
        settled_count: 1,
        unresolved_count: 0,
      },
      {
        channel_id: 2,
        channel_name: '启用低倍率',
        channel_remark: '启用低倍率备注',
        status: 1,
        cost_ratio: 0.2,
        cost_cny: 2,
        settled_count: 1,
        unresolved_count: 0,
      },
      {
        channel_id: 5,
        channel_name: '禁用低倍率',
        channel_remark: '禁用低倍率备注',
        status: 3,
        cost_ratio: 0.1,
        cost_cny: 1,
        settled_count: 1,
        unresolved_count: 0,
      },
    ]

    const markup = renderToStaticMarkup(
      <ChannelMonitorChannelCostTable
        items={overview.channels}
        detailDate={overview.detail_date}
      />
    )

    const orderedNames = [
      '禁用高倍率',
      '启用高倍率',
      '启用低倍率',
      '禁用低倍率',
    ]
    let previousIndex = -1
    for (const name of orderedNames) {
      const index = markup.indexOf(name)
      assert.ok(index > previousIndex, `${name} should be ordered`)
      previousIndex = index
    }
    assert.ok(markup.includes('启用低倍率备注'))
    assert.ok(markup.includes('0.2'))
  })
})
