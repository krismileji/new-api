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

import { CHANNEL_STATUS } from '@/features/channels/constants'

import { orderGroupChannelOptions } from '../group-channel-order'

test('orders enabled channels first and cost ratios low to high within each status', () => {
  const channels = [
    { id: 1, name: '禁用低倍率', status: 2, cost_ratio: 0.2 },
    {
      id: 2,
      name: '启用无倍率',
      status: CHANNEL_STATUS.ENABLED,
      cost_ratio: null,
    },
    {
      id: 3,
      name: '启用高倍率',
      status: CHANNEL_STATUS.ENABLED,
      cost_ratio: 1.5,
    },
    {
      id: 4,
      name: '启用低倍率',
      status: CHANNEL_STATUS.ENABLED,
      cost_ratio: 0.5,
    },
    { id: 5, name: '禁用高倍率', status: 3, cost_ratio: 2 },
  ]

  const orderedIds = orderGroupChannelOptions(channels).map(
    (channel) => channel.id
  )

  assert.deepEqual(orderedIds, [4, 3, 2, 1, 5])
})
