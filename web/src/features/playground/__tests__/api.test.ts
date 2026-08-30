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
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getUserGroups } from '../api'

describe('playground group API', () => {
  test('returns groups in the order configured by the backend', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          vip: { desc: 'VIP', ratio: 2 },
          auto: { desc: 'Auto', ratio: 1 },
          default: { desc: 'Default', ratio: 1 },
          basic: { desc: 'Basic', ratio: 0.5 },
        },
        group_order: ['basic', 'default', 'vip'],
      },
    })

    const groups = await getUserGroups()

    expect(groups.map((group) => group.value)).toEqual([
      'basic',
      'default',
      'vip',
      'auto',
    ])
  })
})
