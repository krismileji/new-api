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

import { getChannelMonitorSmartScheduleModelOptionsByGroup } from '../smart-schedule-model-order'

describe('smart schedule model options by group', () => {
  test('isolates route models by group and preserves configured models', () => {
    const optionsByGroup = getChannelMonitorSmartScheduleModelOptionsByGroup(
      [
        { group: 'vip', model: 'vip-route' },
        { group: 'standard', model: 'standard-route' },
      ],
      [
        {
          groups: ['vip', 'fallback'],
          models: 'channel-beta, channel-alpha',
        },
        { groups: ['standard'], models: 'unrelated-channel-model' },
      ],
      [
        {
          group: 'vip',
          models: ['vip-configured'],
          model_order: ['vip-order-only'],
        },
      ]
    )

    assert.deepEqual(optionsByGroup.get('vip'), [
      'vip-configured',
      'vip-order-only',
      'vip-route',
    ])
    assert.deepEqual(optionsByGroup.get('standard'), ['standard-route'])
    assert.deepEqual(optionsByGroup.get('fallback'), [
      'channel-alpha',
      'channel-beta',
    ])
  })
})
