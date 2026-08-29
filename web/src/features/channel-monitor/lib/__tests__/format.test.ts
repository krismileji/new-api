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

import { describe, test } from 'vitest'

import { formatMonitorRatio } from '../format'

describe('formatMonitorRatio', () => {
  test('preserves significant decimal digits beyond the legacy precision cap', () => {
    assert.equal(formatMonitorRatio(0.00123456789), '0.00123456789')
    assert.equal(formatMonitorRatio(1.23456789), '1.23456789')
  })

  test('supports fixed minimum precision without rounding away extra digits', () => {
    assert.equal(
      formatMonitorRatio(1.23456789, { minimumFractionDigits: 4 }),
      '1.23456789'
    )
    assert.equal(
      formatMonitorRatio(1, { minimumFractionDigits: 6 }),
      '1.000000'
    )
  })

  test('returns a placeholder for missing or non-finite ratios', () => {
    assert.equal(formatMonitorRatio(null), '-')
    assert.equal(formatMonitorRatio(Number.NaN), '-')
    assert.equal(formatMonitorRatio(Number.POSITIVE_INFINITY), '-')
  })
})
