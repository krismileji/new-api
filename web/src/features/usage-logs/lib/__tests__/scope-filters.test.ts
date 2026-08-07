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

import { buildApiParams, buildBaseParams } from '../utils'

const searchParams = {
  channel: '12',
  username: 'alice',
  model: 'gpt-test',
  startTime: 1_000,
  endTime: 2_000,
}

describe('usage log scope filters', () => {
  test('keeps administrator-only filters in the complete view', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams,
      scope: 'all',
    })

    assert.equal(params.channel, 12)
    assert.equal(params.username, 'alice')
  })

  test('keeps the user filter but removes the channel in user-visible view', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams,
      scope: 'user-visible',
    })

    assert.equal(params.channel, undefined)
    assert.equal(params.username, 'alice')
  })

  test('removes aggregate filters from the self view', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams,
      scope: 'self',
    })

    assert.equal(params.channel, undefined)
    assert.equal(params.username, undefined)
  })

  test('sends task channel filters only for the complete view', () => {
    const complete = buildBaseParams({
      page: 1,
      pageSize: 20,
      searchParams,
      scope: 'all',
    })
    const userVisible = buildBaseParams({
      page: 1,
      pageSize: 20,
      searchParams,
      scope: 'user-visible',
    })

    assert.equal(complete.channel_id, '12')
    assert.equal(userVisible.channel_id, undefined)
  })
})
