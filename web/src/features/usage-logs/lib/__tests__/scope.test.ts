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
  getLogsViewCapabilities,
  isLogsViewScope,
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

  test('keeps administrator diagnostics in both aggregate scopes', () => {
    assert.deepEqual(getLogsViewCapabilities('all'), {
      isAdminView: true,
      isAllUsersView: true,
      showUserColumn: true,
      showChannelColumn: true,
    })
    assert.deepEqual(getLogsViewCapabilities('user-visible'), {
      isAdminView: true,
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
})
