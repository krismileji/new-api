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

import {
  getLogsViewTypeFilters,
  getLogsViewCapabilities,
  isLogsViewScope,
  normalizeLogsViewType,
  resolveLogsViewScope,
} from '../scope'

describe('usage log view scope', () => {
  test('downgrades every administrator-only scope for a regular user', () => {
    assert.equal(resolveLogsViewScope('all', false), 'self')
    assert.equal(resolveLogsViewScope('user-visible', false), 'self')
    assert.equal(resolveLogsViewScope('self', false), 'self')
  })

  test('keeps an administrator requested scope unchanged', () => {
    assert.equal(resolveLogsViewScope('all', true), 'all')
    assert.equal(resolveLogsViewScope('user-visible', true), 'user-visible')
    assert.equal(resolveLogsViewScope('self', true), 'self')
  })

  test('separates user aggregation from administrator diagnostics', () => {
    assert.deepEqual(getLogsViewCapabilities('all'), {
      isAdminView: true,
      isAllUsersView: true,
      showUserColumn: true,
      showChannelColumn: true,
    })
    assert.deepEqual(getLogsViewCapabilities('user-visible'), {
      isAdminView: false,
      isAllUsersView: true,
      showUserColumn: true,
      showChannelColumn: true,
    })
    assert.deepEqual(getLogsViewCapabilities('self'), {
      isAdminView: false,
      isAllUsersView: false,
      showUserColumn: false,
      showChannelColumn: false,
    })
  })

  test('accepts only supported scope values', () => {
    assert.equal(isLogsViewScope('all'), true)
    assert.equal(isLogsViewScope('user-visible'), true)
    assert.equal(isLogsViewScope('self'), true)
    assert.equal(isLogsViewScope('admin'), false)
  })

  test('limits user-facing type filters to final request outcomes', () => {
    assert.deepEqual(
      getLogsViewTypeFilters('user-visible').map((type) => type.value),
      ['0', '2', '5']
    )
    assert.deepEqual(
      getLogsViewTypeFilters('self').map((type) => type.value),
      ['0', '2', '5']
    )
    assert.equal(getLogsViewTypeFilters('all').length, 8)
  })

  test('clears an unsupported type when switching to a user-facing scope', () => {
    assert.equal(normalizeLogsViewType('user-visible', ['1']), '0')
    assert.equal(normalizeLogsViewType('self', ['6']), '0')
    assert.equal(normalizeLogsViewType('user-visible', ['5']), '5')
    assert.equal(normalizeLogsViewType('all', ['1']), '1')
  })
})
