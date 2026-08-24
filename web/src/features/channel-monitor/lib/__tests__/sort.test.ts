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

import { CHANNEL_STATUS } from '@/features/channels/constants'

import type {
  ChannelMonitorChannelPerformance,
  ChannelMonitorItem,
  ChannelMonitorSortMode,
} from '../../types'
import { sortChannelMonitorItems } from '../sort'

function createChannel(
  id: number,
  status: number,
  costRatio: number,
  name = `渠道 ${id}`,
  todayCost = 0,
  todayCostConfigured = true
): ChannelMonitorItem {
  return {
    id,
    name,
    status,
    cost_ratio: costRatio,
    today_cost_cny: todayCost,
    today_cost_configured: todayCostConfigured,
  } as ChannelMonitorItem
}

test('keeps enabled channels first and sorts ratios within each status', () => {
  const channels = [
    createChannel(1, CHANNEL_STATUS.ENABLED, 1.2),
    createChannel(2, CHANNEL_STATUS.MANUAL_DISABLED, 0.8),
    createChannel(3, CHANNEL_STATUS.ENABLED, 0.9),
    createChannel(4, CHANNEL_STATUS.MANUAL_DISABLED, 1.5),
  ]

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
    [3, 1, 2, 4]
  )
  assert.deepEqual(
    descending.map((channel) => channel.id),
    [1, 3, 4, 2]
  )
})

test('keeps enabled channels first in every derived sort mode', () => {
  const channels = [
    createChannel(1, CHANNEL_STATUS.MANUAL_DISABLED, 0.5, '渠道 A'),
    createChannel(2, CHANNEL_STATUS.ENABLED, 1, '渠道 M'),
    createChannel(3, CHANNEL_STATUS.MANUAL_DISABLED, 1.5, '渠道 Z'),
  ]
  const performanceByChannel = new Map<
    number,
    ChannelMonitorChannelPerformance
  >([
    [
      1,
      {
        sample_count: 1,
        first_token_sample_count: 1,
        tps_sample_count: 1,
        average_first_token_ms: 100,
        average_tps: 10,
        last_used_time: 1,
      },
    ],
    [
      2,
      {
        sample_count: 1,
        first_token_sample_count: 1,
        tps_sample_count: 1,
        average_first_token_ms: 200,
        average_tps: 20,
        last_used_time: 1,
      },
    ],
    [
      3,
      {
        sample_count: 1,
        first_token_sample_count: 1,
        tps_sample_count: 1,
        average_first_token_ms: 300,
        average_tps: 30,
        last_used_time: 1,
      },
    ],
  ])
  const sortModes: ChannelMonitorSortMode[] = [
    'channel_asc',
    'channel_desc',
    'ratio_asc',
    'ratio_desc',
    'today_cost_asc',
    'today_cost_desc',
    'first_token_asc',
    'first_token_desc',
    'tps_asc',
    'tps_desc',
  ]

  for (const sortMode of sortModes) {
    const sorted = sortChannelMonitorItems(
      channels,
      sortMode,
      [],
      performanceByChannel
    )

    assert.equal(sorted[0]?.id, 2, sortMode)
  }
})

test('sorts configured today costs while keeping unconfigured channels last', () => {
  const channels = [
    createChannel(1, CHANNEL_STATUS.ENABLED, 1, '渠道 A', 12.5),
    createChannel(2, CHANNEL_STATUS.ENABLED, 1, '渠道 B', 0),
    createChannel(3, CHANNEL_STATUS.ENABLED, 1, '渠道 C', 7.25),
    createChannel(4, CHANNEL_STATUS.ENABLED, 1, '渠道 D', 0, false),
  ]

  const ascending = sortChannelMonitorItems(
    channels,
    'today_cost_asc',
    [],
    new Map()
  )
  const descending = sortChannelMonitorItems(
    channels,
    'today_cost_desc',
    [],
    new Map()
  )

  assert.deepEqual(
    ascending.map((channel) => channel.id),
    [2, 3, 1, 4]
  )
  assert.deepEqual(
    descending.map((channel) => channel.id),
    [1, 3, 2, 4]
  )
})

test('keeps enabled channels first without losing custom relative order', () => {
  const channels = [
    createChannel(1, CHANNEL_STATUS.ENABLED, 1),
    createChannel(2, CHANNEL_STATUS.MANUAL_DISABLED, 1),
    createChannel(3, CHANNEL_STATUS.ENABLED, 1),
    createChannel(4, CHANNEL_STATUS.MANUAL_DISABLED, 1),
  ]

  const sorted = sortChannelMonitorItems(
    channels,
    'custom',
    [2, 1, 4, 3],
    new Map()
  )

  assert.deepEqual(
    sorted.map((channel) => channel.id),
    [1, 3, 2, 4]
  )
})
