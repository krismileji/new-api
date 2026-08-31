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

import { afterEach, describe, test } from 'vitest'

import { api, serializeRequestParams } from '../http-client'

const originalAdapter = api.defaults.adapter

describe('HTTP GET request coordination', () => {
  afterEach(() => {
    api.defaults.adapter = originalAdapter
  })

  test('uses a stable parameter key when equivalent objects have different key order', async () => {
    let requestCount = 0
    api.defaults.adapter = async (config) => {
      requestCount += 1
      return {
        config,
        data: { success: true },
        headers: {},
        request: {},
        status: 200,
        statusText: 'OK',
      }
    }

    const first = api.get('/api/channel_monitor/performance', {
      params: {
        minutes: 15,
        filters: { model: 'gpt-4o', channel_id: 7 },
        ignored: undefined,
      },
    })
    const second = api.get('/api/channel_monitor/performance', {
      params: {
        ignored: undefined,
        filters: { channel_id: 7, model: 'gpt-4o' },
        minutes: 15,
      },
    })

    assert.strictEqual(first, second)
    await Promise.all([first, second])
    assert.equal(requestCount, 1)
  })

  test('removes completed requests so a later GET is executed again', async () => {
    let requestCount = 0
    api.defaults.adapter = async (config) => {
      requestCount += 1
      return {
        config,
        data: { success: true },
        headers: {},
        request: {},
        status: 200,
        statusText: 'OK',
      }
    }

    await api.get('/api/channel_monitor/', {
      params: { page: 1, include_disabled: false },
    })
    await api.get('/api/channel_monitor/', {
      params: { include_disabled: false, page: 1 },
    })

    assert.equal(requestCount, 2)
  })

  test('normalizes absent params to the same key as an empty params object', () => {
    assert.equal(serializeRequestParams(undefined), '{}')
    assert.equal(serializeRequestParams(null), '{}')
    assert.equal(serializeRequestParams({}), '{}')
    assert.equal(
      serializeRequestParams({ b: 2, a: 1, omitted: undefined }),
      '{"a":1,"b":2}'
    )
  })
})
