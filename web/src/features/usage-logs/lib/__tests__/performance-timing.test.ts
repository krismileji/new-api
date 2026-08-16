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

import type { UsageLog } from '../../data/schema'
import { getLogTokensPerSecond } from '../format'

const baseLog = {
  completion_tokens: 120,
  use_time: 4,
} as UsageLog

describe('getLogTokensPerSecond', () => {
  test('uses the canonical backend value for versioned timing data', () => {
    assert.equal(
      getLogTokensPerSecond(baseLog, {
        performance_timing_version: 1,
        tokens_per_second: 18.75,
      }),
      18.75
    )
  })

  test('does not recalculate an unavailable canonical metric', () => {
    assert.equal(
      getLogTokensPerSecond(baseLog, {
        performance_timing_version: 1,
        tokens_per_second: null,
      }),
      null
    )
  })

  test('falls back to legacy integer-second calculation for old logs', () => {
    assert.equal(getLogTokensPerSecond(baseLog, null), 30)
  })
})
