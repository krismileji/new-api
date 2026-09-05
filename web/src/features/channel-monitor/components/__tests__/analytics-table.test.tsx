/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
import { test } from 'vitest'

import { ChannelMonitorAnalyticsTable } from '../channel-monitor-analytics-table'

const summary = {
  actual_success_count: 9,
  actual_failure_count: 1,
  actual_sample_count: 10,
  actual_success_rate: 0.9,
  final_success_count: 9,
  final_failure_count: 1,
  final_sample_count: 10,
  final_success_rate: 0.9,
  cache_hit_count: 4,
  cache_sample_count: 8,
  cache_hit_rate: 0.5,
  cache_read_tokens: 40,
  input_tokens: 100,
  cache_utilization_rate: 0.4,
  cache_write_request_count: 2,
}

test('renders channel model rows with both physical channel and model columns', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorAnalyticsTable
      metric='success'
      groupBy='channel_model'
      channels={new Map([[7, { name: '渠道 A', remark: '主渠道' }]])}
      items={[
        { ...summary, key: '7:gpt-a', channel_id: 7, model_name: 'gpt-a' },
      ]}
    />
  )

  assert.match(markup, /渠道 A/)
  assert.match(markup, /主渠道/)
  assert.match(markup, /gpt-a/)
  assert.match(markup, /10/)
  assert.match(markup, /90\.0%/)
  assert.match(markup, /40\.0%/)
})

test('renders the user name and ID for channel drill-down rows', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorAnalyticsTable
      metric='success'
      groupBy='user'
      channels={new Map()}
      items={[
        {
          ...summary,
          key: '31',
          user_id: 31,
          user_name: 'alice',
          user_display_name: 'Alice',
        },
      ]}
    />
  )

  assert.match(markup, /Alice/)
  assert.match(markup, /ID 31/)
  assert.match(markup, /alice/)
})

test('keeps long lists in a horizontally scrollable bounded table', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorAnalyticsTable
      metric='success'
      groupBy='api_key'
      channels={new Map()}
      items={[
        { ...summary, key: '201', api_key_id: 201, api_key_name: '生产 Key' },
      ]}
    />
  )

  assert.match(markup, /overflow-x-auto/)
  assert.match(markup, /min-w-\[42rem\]/)
  assert.match(markup, /生产 Key/)
})

test('renders API Key channel and model details together', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorAnalyticsTable
      metric='cost'
      groupBy='api_key_channel_model'
      channels={new Map([[7, { name: '渠道 A', remark: '主渠道' }]])}
      items={[
        {
          ...summary,
          key: '201:7:model-a',
          api_key_id: 201,
          api_key_name: '生产 Key',
          channel_id: 7,
          model_name: 'model-a',
          cost_nano_cny: 123,
        },
      ]}
    />
  )

  assert.match(markup, /生产 Key/)
  assert.match(markup, /渠道 A/)
  assert.match(markup, /model-a/)
})
