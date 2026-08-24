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

import { orderUpstreamChannelsForSelection } from '../channel-selector-order'
import { MODELS_DEV_PRESET_ID, OFFICIAL_CHANNEL_ID } from '../constants'

test('orders upstream channels by enabled status before official presets', () => {
  const ordered = orderUpstreamChannelsForSelection([
    {
      id: OFFICIAL_CHANNEL_ID,
      name: '禁用官方渠道',
      base_url: 'https://official.example.com',
      status: CHANNEL_STATUS.MANUAL_DISABLED,
    },
    {
      id: 10,
      name: '启用普通渠道',
      base_url: 'https://enabled.example.com',
      status: CHANNEL_STATUS.ENABLED,
    },
    {
      id: MODELS_DEV_PRESET_ID,
      name: '启用官方渠道',
      base_url: 'https://models.dev',
      status: CHANNEL_STATUS.ENABLED,
    },
    {
      id: 11,
      name: '禁用普通渠道',
      base_url: 'https://disabled.example.com',
      status: CHANNEL_STATUS.AUTO_DISABLED,
    },
  ])

  assert.deepEqual(
    ordered.map((channel) => channel.id),
    [MODELS_DEV_PRESET_ID, 10, OFFICIAL_CHANNEL_ID, 11]
  )
})
