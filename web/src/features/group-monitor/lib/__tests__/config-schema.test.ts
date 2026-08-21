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
import { test } from 'vitest'

import { channelGroupMonitorConfigSchema } from '../config-schema'

test('rejects a display window shorter than two probe periods', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    enabled: true,
    groups: [{ groupName: 'default', probeModel: 'gpt-4.1' }],
    intervalSeconds: 300,
    displayValue: 1,
    displayUnit: 'minute',
    revision: 0,
  })

  assert.equal(result.success, false)
  assert.ok(
    !result.success &&
      result.error.issues.some((issue) =>
        issue.message.includes('至少需要覆盖两个探测周期')
      )
  )
})

test('rejects duplicate groups while allowing their configured order', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    enabled: true,
    groups: [
      { groupName: 'vip', probeModel: 'gpt-4.1' },
      { groupName: 'vip', probeModel: 'gpt-4.1-mini' },
    ],
    intervalSeconds: 300,
    displayValue: 60,
    displayUnit: 'minute',
    revision: 3,
  })

  assert.equal(result.success, false)
  assert.ok(
    !result.success &&
      result.error.issues.some((issue) => issue.message.includes('只能配置一次'))
  )
})
