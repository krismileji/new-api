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

import { formatChannelMonitorCost } from '../../lib/format'
import { ChannelMonitorAPIKeyCostTable } from '../channel-monitor-api-key-cost-table'

describe('channel monitor API key cost table', () => {
  test('uses fixed columns and exposes truncated masked values on hover', () => {
    const channelName = '这是一个用于验证列宽与省略展示的特别长渠道名称'
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
                cost_cny: 12.3456,
                settled_count: 9,
                unresolved_count: 2,
              },
            ],
          },
        ]}
      />
    )

    assert.match(markup, /table-fixed/)
    assert.match(markup, /truncate/)
    assert.ok(markup.includes('主 API Key'))
    assert.ok(markup.includes(`title="${channelName}"`))
    assert.ok(markup.includes(`title="${maskedKey}"`))
    assert.ok(markup.includes(formatChannelMonitorCost(12.3456)))
    assert.ok(markup.includes('title="2 次成本未确认"'))
  })

  test('explains that API key costs start with newly settled requests', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorAPIKeyCostTable items={[]} />
    )

    assert.ok(markup.includes('暂无 API Key 成本'))
    assert.ok(markup.includes('新请求结算后开始记录'))
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
})
