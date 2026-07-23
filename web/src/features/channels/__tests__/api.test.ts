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

import { testChannel } from '../api'

describe('channel test API', () => {
  test('sends concurrent tests for the same channel and model independently', async () => {
    const originalAdapter = api.defaults.adapter
    const releaseResponses: Array<() => void> = []
    let adapterCalls = 0
    let markFirstRequestStarted: () => void = () => undefined
    const firstRequestStarted = new Promise<void>((resolve) => {
      markFirstRequestStarted = resolve
    })

    api.defaults.adapter = (config) =>
      new Promise((resolve) => {
        adapterCalls += 1
        if (adapterCalls === 1) markFirstRequestStarted()
        releaseResponses.push(() => {
          resolve({
            data: { success: true, message: '', time: 0 },
            status: 200,
            statusText: 'OK',
            headers: {},
            config,
          })
        })
      })

    const requests = [
      testChannel(7, { model: 'gpt-4o-mini' }),
      testChannel(7, { model: 'gpt-4o-mini' }),
    ]

    await firstRequestStarted
    await new Promise<void>((resolve) => setImmediate(resolve))

    try {
      assert.equal(adapterCalls, 2)
    } finally {
      for (const releaseResponse of releaseResponses) releaseResponse()
      await Promise.allSettled(requests)
      api.defaults.adapter = originalAdapter
    }
  })
})
