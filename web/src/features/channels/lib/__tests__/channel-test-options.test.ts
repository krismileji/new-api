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

import { api } from '@/lib/api'

import { CHANNEL_TEST_DEFAULTS, handleTestChannel } from '../channel-actions'

describe('channel test request options', () => {
  test('uses automatic endpoint detection as the shared dialog default', async () => {
    const originalAdapter = api.defaults.adapter
    let requestParams: unknown

    api.defaults.adapter = (config) => {
      requestParams = config.params
      return Promise.resolve({
        data: { success: true, message: '', time: 0 },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      })
    }

    try {
      await handleTestChannel(7, {
        testModel: 'gpt-4o-mini',
        endpointType: CHANNEL_TEST_DEFAULTS.endpointType,
        stream: CHANNEL_TEST_DEFAULTS.stream,
        recordSample: CHANNEL_TEST_DEFAULTS.recordSample,
        silent: true,
      })

      assert.equal(CHANNEL_TEST_DEFAULTS.recordSample, true)
      assert.deepEqual(requestParams, {
        model: 'gpt-4o-mini',
        endpoint_type: 'auto',
        stream: true,
        record_sample: true,
      })
    } finally {
      api.defaults.adapter = originalAdapter
    }
  })

  test('preserves automatic endpoint selection and an explicitly disabled stream', async () => {
    const originalAdapter = api.defaults.adapter
    let requestParams: unknown

    api.defaults.adapter = (config) => {
      requestParams = config.params
      return Promise.resolve({
        data: { success: true, message: '', time: 0 },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      })
    }

    try {
      await handleTestChannel(7, {
        channelName: 'test channel',
        testModel: 'gpt-4o-mini',
        endpointType: 'auto',
        stream: false,
        recordSample: false,
        silent: true,
      })

      assert.deepEqual(requestParams, {
        model: 'gpt-4o-mini',
        endpoint_type: 'auto',
        stream: false,
        record_sample: false,
      })
    } finally {
      api.defaults.adapter = originalAdapter
    }
  })

  test('returns complete stream and usage metrics to callers', async () => {
    const originalAdapter = api.defaults.adapter
    let completion:
      | {
          success: boolean
          responseTime?: number
          firstTokenMs?: number | null
          inputTokens?: number
          cacheWriteTokens?: number
          sampleRecorded?: boolean
        }
      | undefined

    api.defaults.adapter = (config) =>
      Promise.resolve({
        data: {
          success: true,
          message: '',
          time: 1.2,
          data: {
            response_time: 1200,
            first_token_ms: 180,
            tokens_per_second: 24.5,
            usage_available: true,
            input_tokens: 20,
            output_tokens: 16,
            total_tokens: 36,
            cached_tokens: 8,
            cache_write_tokens: 4,
            reasoning_tokens: 3,
            smart_schedule_sample_recorded: true,
            smart_schedule_sample_message: '已计入渠道 + 模型共享样本',
          },
        },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      })

    try {
      await handleTestChannel(
        7,
        { testModel: 'gpt-4o-mini', stream: true, silent: true },
        (success, responseTime, _error, _errorCode, response) => {
          completion = {
            success,
            responseTime,
            firstTokenMs: response?.data?.first_token_ms,
            inputTokens: response?.data?.input_tokens,
            cacheWriteTokens: response?.data?.cache_write_tokens,
            sampleRecorded: response?.data?.smart_schedule_sample_recorded,
          }
        }
      )
    } finally {
      api.defaults.adapter = originalAdapter
    }

    assert.deepEqual(completion, {
      success: true,
      responseTime: 1200,
      firstTokenMs: 180,
      inputTokens: 20,
      cacheWriteTokens: 4,
      sampleRecorded: true,
    })
  })
})
