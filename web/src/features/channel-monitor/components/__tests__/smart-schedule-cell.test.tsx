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

import type { ChannelMonitorItem } from '../../types'
import { ChannelMonitorSmartScheduleCell } from '../channel-monitor-smart-schedule-cell'

const noop = () => {}

function createChannel(overrides: Partial<ChannelMonitorItem>) {
  return {
    id: 7,
    name: '测试渠道',
    type: 1,
    status: 1,
    priority: 100,
    weight: 80,
    base_url: 'https://example.com',
    models: 'test-model',
    test_model: 'test-model',
    groups: ['default'],
    ratio: 1,
    previous_ratio: null,
    cost_ratio: 1,
    previous_cost_ratio: null,
    conversion_factor: 1,
    remark: '',
    channel_remark: '',
    updated_time: 0,
    updated_by: 0,
    updated_by_username: '',
    last_fetch_status: '',
    last_fetch_error: '',
    last_fetch_time: 0,
    consecutive_failures: 0,
    upstream_balance: null,
    last_balance_time: 0,
    last_balance_error: '',
    smart_schedule_excluded: false,
    last_schedule_status: '',
    last_schedule_error: '',
    last_schedule_score: null,
    last_schedule_priority: 0,
    last_schedule_weight: 0,
    last_schedule_time: 0,
    upstream: null,
    ...overrides,
  } satisfies ChannelMonitorItem
}

function renderCell(overrides: Partial<ChannelMonitorItem>) {
  return renderToStaticMarkup(
    <ChannelMonitorSmartScheduleCell
      channel={createChannel(overrides)}
      pending={false}
      onUpdate={noop}
    />
  )
}

describe('channel monitor smart schedule cell status', () => {
  test('places the latest score after participation without a scheduled status row', () => {
    const markup = renderCell({
      last_schedule_status: 'succeeded',
      last_schedule_score: 0.288,
      last_schedule_time: 1_752_777_845,
    })

    assert.match(markup, /参与调度[\s\S]*得分 28\.8/)
    assert.doesNotMatch(markup, /已调度/)
  })

  test('hides the skipped status and stale score for an excluded channel', () => {
    const markup = renderCell({
      smart_schedule_excluded: true,
      last_schedule_status: 'skipped',
      last_schedule_error: '已设为不参与智能调度',
      last_schedule_score: 0.288,
    })

    assert.doesNotMatch(markup, /已跳过/)
    assert.doesNotMatch(markup, /已设为不参与智能调度/)
    assert.doesNotMatch(markup, /得分 28\.8/)
  })

  test('keeps a useful skipped reason for a participating channel', () => {
    const markup = renderCell({
      last_schedule_status: 'skipped',
      last_schedule_error: '渠道不支持已配置的基准模型',
    })

    assert.match(markup, /已跳过/)
    assert.match(markup, /渠道不支持已配置的基准模型/)
  })
})
