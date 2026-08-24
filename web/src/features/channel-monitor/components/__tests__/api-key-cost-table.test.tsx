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

import { formatChannelMonitorCost } from '../../lib/format'
import { ChannelMonitorAPIKeyCostTable } from '../channel-monitor-api-key-cost-table'

describe('channel monitor API key cost table', () => {
  test('uses balanced columns and exposes truncated masked values on hover', () => {
    const channelName = '这是一个用于验证列宽与省略展示的特别长渠道名称'
    const channelRemark = '这是一个用于验证备注省略展示的特别长渠道备注'
    const maskedKey = 'sk-a**********lpha'
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable
        items={[
          {
            id: 17,
            api_key_id: 7,
            api_key_name: '主 API Key',
            api_key: maskedKey,
            cost_cny: 12.3456,
            settled_count: 9,
            unresolved_count: 2,
            channels: [
              {
                channel_id: 7,
                channel_name: channelName,
                channel_remark: channelRemark,
                cost_cny: 12.3456,
                settled_count: 9,
                unresolved_count: 2,
              },
              {
                channel_id: 8,
                channel_name: '仅未确认渠道',
                channel_remark: '',
                cost_cny: 0,
                settled_count: 0,
                unresolved_count: 2,
              },
            ],
          },
          {
            id: 18,
            api_key_id: 8,
            api_key_name: '仅未确认 API Key',
            api_key: '',
            cost_cny: 0,
            settled_count: 0,
            unresolved_count: 3,
            channels: [],
          },
        ]}
      />
    )

    assert.match(markup, /table-fixed/)
    assert.ok(markup.includes('class="min-w-0"'))
    assert.equal(markup.includes('min-w-[840px]'), false)
    assert.equal(markup.includes('min-w-[680px]'), false)
    assert.ok(markup.includes('minmax(0,2.2fr)'))
    assert.equal(markup.includes('minmax(14rem,2.2fr)'), false)
    assert.match(markup, /truncate/)
    assert.ok(markup.includes('API Key 成本明细'))
    assert.ok(markup.includes('关联渠道'))
    assert.ok(markup.includes('结算请求'))
    assert.ok(markup.includes('结算成本'))
    assert.ok(markup.includes('主 API Key'))
    assert.ok(markup.includes(`title="${channelName}"`))
    assert.ok(markup.includes(`备注：${channelRemark}`))
    assert.ok(markup.includes(`title="${channelRemark}"`))
    assert.ok(markup.includes(`title="${maskedKey}"`))
    assert.ok(markup.includes(formatChannelMonitorCost(12.3456)))
    assert.ok(markup.includes('2 个渠道'))
    assert.ok(markup.includes('仅未确认渠道'))
    assert.ok(markup.includes('仅未确认 API Key'))
    assert.ok(markup.includes('未归属用户'))
    assert.ok(markup.includes('API Key 数'))
    assert.ok(markup.includes('未解析 2'))
    assert.ok(markup.includes('解析率 81.8%'))
    assert.ok(markup.includes('0%'))
    assert.ok(markup.indexOf('主 API Key') < markup.indexOf('仅未确认 API Key'))
    assert.ok(markup.includes('按用户排序'))
    assert.ok(markup.includes('按结算成本排序'))
  })

  test('explains that API key costs start with newly settled requests', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable items={[]} />
    )

    assert.ok(markup.includes('暂无 API Key 成本尝试'))
    assert.ok(markup.includes('上游请求结算或进入未解析后开始记录'))
  })

  test('renders realtime API key costs when channel details are null', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable
        items={[
          {
            id: 0,
            api_key_id: 7,
            api_key_name: '实时 API Key',
            api_key: '',
            cost_cny: 1,
            settled_count: 1,
            unresolved_count: 0,
            channels: null as unknown as [],
          },
        ]}
      />
    )

    assert.ok(markup.includes('实时 API Key'))
    assert.ok(markup.includes('0 个渠道'))
  })

  test('uses the masked upstream key when historical rows have no stored name', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable
        items={[
          {
            id: 18,
            api_key_id: 0,
            api_key_name: '',
            api_key: 'sk-a**********lpha',
            cost_cny: 1,
            settled_count: 1,
            unresolved_count: 0,
            channels: [
              {
                channel_id: 3,
                channel_name: '渠道三',
                channel_remark: '',
                cost_cny: 1,
                settled_count: 1,
                unresolved_count: 0,
              },
            ],
          },
        ]}
      />
    )

    assert.ok(markup.includes('上游 Key sk-a**********lpha'))
    assert.ok(markup.includes('1 个渠道'))
    assert.ok(markup.includes('渠道三'))
  })

  test('groups API keys by user before rendering the key rows', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable
        items={[
          {
            id: 31,
            api_key_id: 11,
            api_key_name: 'Alice 主 Key',
            api_key: '',
            user_id: 101,
            username: 'alice',
            user_display_name: 'Alice',
            cost_cny: 1,
            settled_count: 1,
            unresolved_count: 0,
            channels: [],
          },
          {
            id: 32,
            api_key_id: 12,
            api_key_name: 'Alice 备用 Key',
            api_key: '',
            user_id: 101,
            username: 'alice',
            user_display_name: 'Alice',
            cost_cny: 2,
            settled_count: 2,
            unresolved_count: 0,
            channels: [],
          },
          {
            id: 33,
            api_key_id: 21,
            api_key_name: 'Bob Key',
            api_key: '',
            user_id: 202,
            username: 'bob',
            user_display_name: 'Bob',
            cost_cny: 3,
            settled_count: 3,
            unresolved_count: 0,
            channels: [],
          },
        ]}
      />
    )

    assert.equal(markup.match(/title="Alice"/g)?.length, 1)
    assert.equal(markup.match(/title="Bob"/g)?.length, 1)
    assert.ok(markup.includes('@alice'))
    assert.ok(markup.includes('@bob'))
    assert.ok(markup.indexOf('Alice 主 Key') < markup.indexOf('Bob Key'))
  })

  test('exposes sorting controls for API key summary metrics', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable
        items={[
          {
            id: 18,
            api_key_id: 1,
            api_key_name: '可排序 Key',
            api_key: '',
            cost_cny: 1,
            settled_count: 1,
            unresolved_count: 0,
            channels: [],
          },
        ]}
      />
    )

    assert.ok(markup.includes('按关联渠道排序'))
    assert.ok(markup.includes('按结算请求排序'))
    assert.ok(markup.includes('按未解析排序'))
    assert.ok(markup.includes('按解析率排序'))
  })
})
