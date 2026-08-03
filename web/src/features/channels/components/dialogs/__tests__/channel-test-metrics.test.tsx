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

import type { ChannelTestResponse } from '../../../types'
import { parseChannelTestMetrics } from '../channel-test-metric-values'
import { ChannelTestMetrics } from '../channel-test-metrics'

describe('channel test metrics', () => {
  test('parses complete usage and preserves authoritative zero values', () => {
    const response: ChannelTestResponse = {
      success: true,
      data: {
        response_time: 1234,
        first_token_ms: 187,
        tokens_per_second: 24.5,
        usage_available: true,
        input_tokens: 120,
        output_tokens: 36,
        total_tokens: 156,
        cached_tokens: 80,
        cache_write_tokens: 0,
        reasoning_tokens: 8,
        smart_schedule_sample_recorded: true,
        smart_schedule_sample_message: '已计入渠道 + 模型共享样本',
      },
    }

    assert.deepEqual(parseChannelTestMetrics(response), {
      responseTime: 1234,
      firstTokenMs: 187,
      tokensPerSecond: 24.5,
      usageAvailable: true,
      inputTokens: 120,
      outputTokens: 36,
      totalTokens: 156,
      cachedTokens: 80,
      cacheWriteTokens: 0,
      reasoningTokens: 8,
      smartScheduleSampleRecorded: true,
      smartScheduleSampleMessage: '已计入渠道 + 模型共享样本',
    })
  })

  test('does not present estimated token values as upstream usage', () => {
    const response: ChannelTestResponse = {
      success: true,
      data: {
        usage_available: false,
        input_tokens: 12,
        output_tokens: 0,
      },
    }

    const metrics = parseChannelTestMetrics(response, 900)

    assert.equal(metrics.responseTime, 900)
    assert.equal(metrics.usageAvailable, false)
    assert.equal(metrics.inputTokens, undefined)
    assert.equal(metrics.outputTokens, undefined)
  })

  test('renders performance, cache and token details together', () => {
    const markup = renderToStaticMarkup(
      <ChannelTestMetrics
        metrics={{
          responseTime: 1234,
          firstTokenMs: 187,
          tokensPerSecond: 24.5,
          usageAvailable: true,
          inputTokens: 120,
          outputTokens: 36,
          totalTokens: 156,
          cachedTokens: 80,
          cacheWriteTokens: 12,
          reasoningTokens: 8,
          smartScheduleSampleRecorded: true,
          smartScheduleSampleMessage: '已计入渠道 + 模型共享样本',
        }}
      />
    )

    for (const label of [
      '总耗时',
      '首字时间',
      'TPS',
      '输入 Token',
      '输出 Token',
      '缓存读取',
      '缓存写入',
      '推理 Token',
      '总 Token',
    ]) {
      assert.ok(markup.includes(label), `${label} should be visible`)
    }
    for (const value of ['1.23 s', '187 ms', '24.50', '120', '36', '156']) {
      assert.ok(markup.includes(value), `${value} should be visible`)
    }
    assert.ok(markup.includes('已计入渠道样本'))
    assert.ok(markup.includes('title="已计入渠道 + 模型共享样本"'))
  })
})
