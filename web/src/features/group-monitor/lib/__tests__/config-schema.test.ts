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

import { expect, test } from 'vitest'

import { channelGroupMonitorConfigSchema } from '../config-schema'

const configuration = {
  enabled: true,
  categories: [{ categoryId: 'general', name: '通用模型' }],
  groups: [
    { groupName: 'default', probeModel: 'gpt-4.1', categoryId: 'general' },
  ],
  intervalSeconds: 60,
  displayValue: 60,
  displayUnit: 'minute',
  revision: 0,
}

test.each(['  编程模型  ', '🚀'.repeat(64)])(
  'accepts category name %s and trims whitespace',
  (name) => {
    const result = channelGroupMonitorConfigSchema.parse({
      ...configuration,
      categories: [{ categoryId: 'general', name }],
    })
    expect(result.categories[0].name).toBe(name.trim())
  }
)

test('empty categories can be saved before adding any groups', () => {
  const result = channelGroupMonitorConfigSchema.parse({
    ...configuration,
    groups: [],
  })
  expect(result.categories).toEqual(configuration.categories)
  expect(result.groups).toEqual([])
})

test.each([
  { name: '  ', message: '请填写分类名称' },
  { name: '类'.repeat(65), message: '分类名称不能超过 64 个字符' },
])(
  'rejects invalid category names with a field error: $message',
  ({ name, message }) => {
    const result = channelGroupMonitorConfigSchema.safeParse({
      ...configuration,
      categories: [{ categoryId: 'general', name }],
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues).toContainEqual(
        expect.objectContaining({ path: ['categories', 0, 'name'], message })
      )
    }
  }
)

test('rejects duplicate trimmed category names', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    ...configuration,
    categories: [
      { categoryId: 'general', name: '通用模型' },
      { categoryId: 'other', name: ' 通用模型 ' },
    ],
  })
  expect(result.success).toBe(false)
  if (!result.success) {
    expect(result.error.issues).toContainEqual(
      expect.objectContaining({
        path: ['categories', 1, 'name'],
        message: '分类名称不能重复',
      })
    )
  }
})

test('requires groups to belong to a created category', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    ...configuration,
    categories: [],
  })
  expect(result.success).toBe(false)
  if (!result.success) {
    expect(result.error.issues).toContainEqual(
      expect.objectContaining({
        path: ['groups', 0, 'categoryId'],
        message: '请选择已创建的分类',
      })
    )
  }
})

test('rejects a display window shorter than two probe periods', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    ...configuration,
    groups: [
      { categoryId: 'general', groupName: 'default', probeModel: 'gpt-4.1' },
    ],
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
    ...configuration,
    groups: [
      { categoryId: 'general', groupName: 'vip', probeModel: 'gpt-4.1' },
      { categoryId: 'general', groupName: 'vip', probeModel: 'gpt-4.1-mini' },
    ],
    intervalSeconds: 300,
    displayValue: 60,
    displayUnit: 'minute',
    revision: 3,
  })

  assert.equal(result.success, false)
  assert.ok(
    !result.success &&
      result.error.issues.some((issue) =>
        issue.message.includes('只能配置一次')
      )
  )
})

test('accepts one Unicode display character and defaults an omitted value', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    ...configuration,
    groups: [
      {
        categoryId: 'general',
        groupName: 'default',
        probeModel: 'gpt-4.1',
        displayInitial: '组',
      },
      { categoryId: 'general', groupName: 'vip', probeModel: 'gpt-4.1-mini' },
    ],
    intervalSeconds: 300,
    displayValue: 60,
    displayUnit: 'minute',
    revision: 0,
  })

  assert.equal(result.success, true)
  if (result.success) {
    assert.equal(result.data.groups[0].displayInitial, '组')
    assert.equal(result.data.groups[1].displayInitial, '')
  }
})

test('rejects a display value containing multiple Unicode characters', () => {
  const result = channelGroupMonitorConfigSchema.safeParse({
    ...configuration,
    groups: [
      {
        categoryId: 'general',
        groupName: 'default',
        probeModel: 'gpt-4.1',
        displayInitial: 'AB',
      },
    ],
    intervalSeconds: 300,
    displayValue: 60,
    displayUnit: 'minute',
    revision: 0,
  })

  assert.equal(result.success, false)
  assert.ok(
    !result.success &&
      result.error.issues.some((issue) =>
        issue.message.includes('展示字只能配置一个字符')
      )
  )
})
