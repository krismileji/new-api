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
import { describe, expect, it } from 'vitest'

import { parseSidebarModulesAdmin } from '../config'

describe('parseSidebarModulesAdmin', () => {
  it('defaults the shop module to disabled for existing configurations', () => {
    const config = parseSidebarModulesAdmin(
      JSON.stringify({
        personal: {
          enabled: true,
          topup: true,
          personal: true,
        },
      })
    )

    expect(config.personal.shop).toBe(false)
    expect(config.personal.shop_url).toBe('')
  })

  it('preserves the configured shop URL alongside its toggle', () => {
    const config = parseSidebarModulesAdmin(
      JSON.stringify({
        personal: {
          shop: true,
          shop_url: 'https://shop.example.com/recharge',
        },
      })
    )

    expect(config.personal.shop).toBe(true)
    expect(config.personal.shop_url).toBe('https://shop.example.com/recharge')
  })
})
