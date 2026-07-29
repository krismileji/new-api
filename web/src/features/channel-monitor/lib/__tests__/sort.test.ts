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
import { test } from 'node:test'

import type { ChannelMonitorItem } from '../../types'
import { sortChannelMonitorItems } from '../sort'

function createChannel(
  id: number,
  status: number,
  costRatio: number
): ChannelMonitorItem {
  return {
    id,
    name: `渠道 ${id}`,
    status,
    cost_ratio: costRatio,
  } as ChannelMonitorItem
}

test('sorts channel cost ratios directly without moving disabled channels', () => {
  const channels = [createChannel(1, 1, 1.2), createChannel(2, 2, 0.8)]

  const ascending = sortChannelMonitorItems(
    channels,
    'ratio_asc',
    [],
    new Map()
  )
  const descending = sortChannelMonitorItems(
    channels,
    'ratio_desc',
    [],
    new Map()
  )

  assert.deepEqual(
    ascending.map((channel) => channel.id),
    [2, 1]
  )
  assert.deepEqual(
    descending.map((channel) => channel.id),
    [1, 2]
  )
})
