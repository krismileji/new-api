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

import { expandShopRechargeItems } from '../use-sidebar-config'

describe('expandShopRechargeItems', () => {
  it('creates one sidebar entry per valid configured shop URL', () => {
    const items = expandShopRechargeItems(
      [{ title: '小铺充值', url: '/wallet/shop' }],
      {
        personal: {
          enabled: true,
          shop: true,
          shop_urls: [
            'https://shop.example.com/recharge',
            'not-a-url',
            'https://shop.example.com/credits',
          ],
        },
      }
    )

    expect(items).toHaveLength(2)
    expect(items.map((item) => item.title)).toEqual([
      '小铺充值 1',
      '小铺充值 2',
    ])
    expect(items.map((item) => item.url)).toEqual([
      '/wallet/shop?index=0',
      '/wallet/shop?index=1',
    ])
  })

  it('keeps the legacy single URL working', () => {
    const items = expandShopRechargeItems(
      [{ title: '小铺充值', url: '/wallet/shop' }],
      {
        personal: {
          enabled: true,
          shop: true,
          shop_url: 'https://shop.example.com/recharge',
        },
      }
    )

    expect(items).toHaveLength(1)
    expect(items[0]?.title).toBe('小铺充值')
    expect(items[0]?.url).toBe('/wallet/shop?index=0')
  })

  it('uses custom names for expanded shop entries', () => {
    const items = expandShopRechargeItems(
      [{ title: '小铺充值', url: '/wallet/shop' }],
      {
        personal: {
          enabled: true,
          shop: true,
          shop_url: [
            'https://shop.example.com/recharge',
            'https://shop.example.com/credits',
          ],
          shop_names: ['充值中心', '积分商城'],
        },
      }
    )

    expect(items.map((item) => item.title)).toEqual(['充值中心', '积分商城'])
  })
})
