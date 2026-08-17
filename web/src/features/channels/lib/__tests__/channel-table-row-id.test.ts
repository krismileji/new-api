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
import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import {
  aggregateChannelsByTag,
  getChannelTableRowId,
  type TagRow,
} from '../channel-utils'

function channel(id: number): Channel {
  return { id } as Channel
}

describe('channel table row identity', () => {
  test('keeps enabled child channels first inside each tag', () => {
    const channels = [
      { id: 4, status: 2 },
      { id: 3, status: 1 },
      { id: 2, status: 3 },
      { id: 1, status: 1 },
    ].map((item) => ({
      ...item,
      tag: '生产',
      used_quota: 0,
      response_time: 0,
      priority: 0,
      weight: 0,
      group: 'default',
    })) as Channel[]

    const rows = aggregateChannelsByTag(channels)
    const tagRow = rows[0] as TagRow

    expect(tagRow.children.map((child) => child.id)).toEqual([3, 1, 4, 2])
  })

  test('keeps each channel identity when priority updates reorder the rows', () => {
    const first = channel(101)
    const updated = channel(202)
    const third = channel(303)

    const beforeUpdate = [first, updated, third].map(getChannelTableRowId)
    const afterUpdate = [updated, first, third].map(getChannelTableRowId)

    expect(beforeUpdate).toEqual(['channel:101', 'channel:202', 'channel:303'])
    expect(afterUpdate).toEqual(['channel:202', 'channel:101', 'channel:303'])
  })

  test('uses separate namespaces for tag and channel rows', () => {
    const tagRow = {
      id: '202' as unknown as number,
      tag: '202',
      children: [channel(202)],
    } as TagRow

    expect(getChannelTableRowId(tagRow)).toBe('tag:202')
    expect(getChannelTableRowId(channel(202))).toBe('channel:202')
  })
})
