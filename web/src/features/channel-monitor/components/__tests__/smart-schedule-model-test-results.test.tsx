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

import type { ChannelMonitorSmartScheduleModelTestResult } from '../../types'
import { ChannelMonitorSmartScheduleModelTestResults } from '../channel-monitor-smart-schedule-model-test-results'

function createResult(
  stream: boolean
): ChannelMonitorSmartScheduleModelTestResult {
  return {
    group: 'vip',
    model: 'cache-model',
    stream,
    endpoint_type: 'auto',
    total: 3,
    succeeded: 1,
    failed: 1,
    skipped: 1,
    results: [
      {
        channel_id: 7,
        channel_name: '缓存主渠道',
        participates: true,
        available: true,
        status: 'success',
        total_ms: 1650,
        first_token_ms: stream ? 812 : undefined,
        tps: stream ? 41.23 : undefined,
      },
      {
        channel_id: 8,
        channel_name: '故障渠道',
        participates: true,
        available: true,
        status: 'failure',
        total_ms: 95,
        error: '上游拒绝请求',
        error_code: 'bad_response',
      },
      {
        channel_id: 9,
        channel_name: '暂停渠道',
        participates: false,
        available: true,
        status: 'skipped',
        total_ms: 0,
        error: '渠道未参与智能调度',
      },
    ],
  }
}

describe('smart schedule model test results', () => {
  test('shows first-token latency and TPS for a streaming pool test', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleModelTestResults
        result={createResult(true)}
        pendingChannelId={null}
        testing={false}
        onRetry={() => {}}
      />
    )

    assert.ok(markup.includes('流式'))
    assert.ok(markup.includes('首字'))
    assert.ok(markup.includes('TPS'))
    assert.ok(markup.includes('812 ms'))
    assert.ok(markup.includes('41.23'))
    assert.ok(markup.includes('正常 1'))
    assert.ok(markup.includes('失败 1'))
    assert.ok(markup.includes('跳过 1'))
    assert.ok(markup.includes('上游拒绝请求'))
    assert.ok(markup.includes('bad_response'))
    assert.ok(markup.includes('渠道未参与智能调度'))
    assert.ok(markup.includes('aria-label="重新测试 缓存主渠道"'))
  })

  test('omits stream-only columns for a non-streaming test', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleModelTestResults
        result={createResult(false)}
        pendingChannelId={null}
        testing={false}
        onRetry={() => {}}
      />
    )

    assert.ok(markup.includes('非流式'))
    assert.equal(markup.includes('<th data-slot="table-head">首字</th>'), false)
    assert.equal(markup.includes('<th data-slot="table-head">TPS</th>'), false)
  })
})
