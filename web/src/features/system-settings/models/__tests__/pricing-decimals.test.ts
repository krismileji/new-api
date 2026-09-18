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
import { expect, test } from 'vitest'

import { buildModelSnapshots } from '../model-pricing-snapshots'
import { formatPricingNumber } from '../pricing-format'

test('价格换算结果以普通小数显示并保留原有浮点误差处理', () => {
  expect(formatPricingNumber(2e-7)).toBe('0.0000002')
  expect(formatPricingNumber(0.1 + 0.2)).toBe('0.3')
  expect(formatPricingNumber('')).toBe('')
})

test('模型价格和各类倍率从设置加载后保留完整小数及显式零值', () => {
  const snapshots = buildModelSnapshots({
    modelPrice: '{"fixed":2e-7}',
    modelRatio: '{"token":3e-7}',
    cacheRatio: '{"token":0}',
    createCacheRatio: '{"token":4e-7}',
    completionRatio: '{"token":5e-7}',
    imageRatio: '{"token":6e-7}',
    audioRatio: '{"token":7e-7}',
    audioCompletionRatio: '{"token":8e-7}',
    billingMode: '{}',
    billingExpr: '{}',
  })

  expect(snapshots.find((row) => row.name === 'fixed')).toMatchObject({
    price: '0.0000002',
    ratio: '',
  })
  expect(snapshots.find((row) => row.name === 'token')).toMatchObject({
    price: '',
    ratio: '0.0000003',
    cacheRatio: '0',
    createCacheRatio: '0.0000004',
    completionRatio: '0.0000005',
    imageRatio: '0.0000006',
    audioRatio: '0.0000007',
    audioCompletionRatio: '0.0000008',
  })
})
