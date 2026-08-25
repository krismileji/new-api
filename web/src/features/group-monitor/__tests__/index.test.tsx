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

import { GroupMonitorBucketDetails, GroupMonitorContent } from '../index'

describe('group monitor content', () => {
  test('hover details show only first token, TPS, and response time', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 1,
          upstream_failure: 1,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 0,
          first_token_total_ms: 220,
          first_token_sample_count: 1,
          tps_total: 38.5,
          tps_sample_count: 1,
          response_time_total_ms: 1_480,
          response_time_sample_count: 1,
          result: 'success',
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('首字'))
    assert.ok(markup.includes('0.22 秒'))
    assert.ok(markup.includes('TPS'))
    assert.ok(markup.includes('38.5'))
    assert.ok(markup.includes('耗时'))
    assert.ok(markup.includes('1.48 秒'))
    assert.ok(!markup.includes('毫秒'))
    assert.ok(!markup.includes('上游失败'))
    assert.ok(!markup.includes('成功率'))
  })

  test('keeps visible configured groups when monitoring is paused', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorContent
        result={{
          enabled: false,
          server_now: 1_752_777_900,
          data_cutoff_at: 1_752_777_840,
          display_value: 60,
          display_unit: 'minute',
          items: [
            {
              group: 'default',
              initial: 'D',
              status: 'paused',
              probe_model: 'gpt-4.1-mini',
              latest_first_token_ms: 215,
              success_rate: 100,
              last_finished_at: 1_752_777_840,
              recent_window: [
                {
                  started_at: 1_752_777_840,
                  success: 1,
                  upstream_failure: 0,
                  rate_limited: 0,
                  local_failure: 0,
                  unavailable: 0,
                  skipped: 0,
                  timeout: 0,
                  result: 'success',
                },
              ],
            },
          ],
        }}
      />
    )

    assert.ok(markup.includes('default'))
    assert.ok(markup.includes('gpt-4.1-mini'))
    assert.ok(markup.includes('已停用'))
    assert.ok(!markup.includes('分组监控暂未启用'))
    assert.match(markup, /data-slot="group-monitor-bucket"/)
    assert.match(markup, /data-group-monitor-window-value="60"/)
  })

  test('renders a timed out probe as a yellow warning', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 0,
          upstream_failure: 0,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 1,
          result: 'timeout',
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('超时'))
    assert.match(markup, /bg-warning/)
  })

  test('uses the latest execution instead of the bucket aggregate for hover details', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 1,
          upstream_failure: 0,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 1,
          first_token_total_ms: 220,
          first_token_sample_count: 1,
          tps_total: 38.5,
          tps_sample_count: 1,
          response_time_total_ms: 31_040,
          response_time_sample_count: 1,
          result: 'timeout',
          latest_result: 'success',
          latest_first_token_ms: 220,
          latest_tps: 38.5,
          latest_response_time_ms: 1_480,
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('成功'))
    assert.ok(markup.includes('0.22 秒'))
    assert.ok(markup.includes('1.48 秒'))
    assert.ok(!markup.includes('超时'))
  })
})
