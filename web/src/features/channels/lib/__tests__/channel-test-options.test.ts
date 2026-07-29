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
  test('uses streaming Responses as the shared dialog defaults', async () => {
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
        silent: true,
      })

      assert.deepEqual(requestParams, {
        model: 'gpt-4o-mini',
        endpoint_type: 'openai-response',
        stream: true,
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
        silent: true,
      })

      assert.deepEqual(requestParams, {
        model: 'gpt-4o-mini',
        endpoint_type: 'auto',
        stream: false,
      })
    } finally {
      api.defaults.adapter = originalAdapter
    }
  })
})
