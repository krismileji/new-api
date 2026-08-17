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

import { channelStatusProbeConfigSchema } from '../status-probe-schema'

describe('状态探测配置校验', () => {
  test('计入智能调度样本时拒绝小于 60 秒的间隔', () => {
    const result = channelStatusProbeConfigSchema.safeParse({
      enabled: true,
      models: ['gpt-4.1'],
      intervalSeconds: 30,
      displayValue: 60,
      displayUnit: 'minute',
      recordSample: true,
    })

    assert.equal(result.success, false)
    if (!result.success) {
      assert.equal(result.error.issues[0]?.path.join('.'), 'intervalSeconds')
    }
  })

  test('拒绝通配模型和超过 20 个模型的配置', () => {
    const wildcard = channelStatusProbeConfigSchema.safeParse({
      enabled: true,
      models: ['gpt-*'],
      intervalSeconds: 300,
      displayValue: 60,
      displayUnit: 'minute',
      recordSample: false,
    })
    const tooMany = channelStatusProbeConfigSchema.safeParse({
      enabled: true,
      models: Array.from({ length: 21 }, (_, index) => `model-${index}`),
      intervalSeconds: 300,
      displayValue: 60,
      displayUnit: 'minute',
      recordSample: false,
    })

    assert.equal(wildcard.success, false)
    assert.equal(tooMany.success, false)
  })

  test('按单位限制状态展示范围并把总范围限制在 30 天内', () => {
    const result = channelStatusProbeConfigSchema.safeParse({
      enabled: true,
      models: ['gpt-4.1'],
      intervalSeconds: 300,
      displayValue: 31,
      displayUnit: 'day',
      recordSample: false,
    })

    assert.equal(result.success, false)

    assert.equal(
      channelStatusProbeConfigSchema.safeParse({
        enabled: true,
        models: ['gpt-4.1'],
        intervalSeconds: 300,
        displayValue: 24,
        displayUnit: 'hour',
        recordSample: false,
      }).success,
      true
    )
  })
})
