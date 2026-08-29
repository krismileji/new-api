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

import {
  getSidebarShopEntries,
  getSidebarShopEntryTitle,
  getSidebarShopUrls,
  parseSidebarModulesAdmin,
} from '../config'

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
    expect(config.personal.shop_urls).toBeUndefined()
    expect(config.personal.shop_names).toBeUndefined()
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

  it('parses multiple shop URLs from the array configuration', () => {
    const config = parseSidebarModulesAdmin(
      JSON.stringify({
        personal: {
          shop: true,
          shop_urls: [
            'https://shop.example.com/recharge',
            'https://shop.example.com/credits',
          ],
        },
      })
    )

    expect(config.personal.shop_urls).toEqual([
      'https://shop.example.com/recharge',
      'https://shop.example.com/credits',
    ])
    expect(getSidebarShopUrls(config.personal)).toEqual([
      'https://shop.example.com/recharge',
      'https://shop.example.com/credits',
    ])
  })

  it('accepts an array in the legacy shop_url field', () => {
    const config = parseSidebarModulesAdmin(
      JSON.stringify({
        personal: {
          shop: true,
          shop_url: ['https://shop.example.com/recharge', ''],
        },
      })
    )

    expect(getSidebarShopUrls(config.personal)).toEqual([
      'https://shop.example.com/recharge',
    ])
  })

  it('prefers the newer array value when legacy and new values coexist', () => {
    expect(
      getSidebarShopUrls({
        shop_url: 'https://shop.example.com/legacy',
        shop_urls: ['https://shop.example.com/new'],
      })
    ).toEqual(['https://shop.example.com/new'])
  })

  it('allows an empty newer array to clear a stale legacy URL', () => {
    expect(
      getSidebarShopUrls({
        shop_url: 'https://shop.example.com/legacy',
        shop_urls: [],
      })
    ).toEqual([])
  })

  it('keeps custom names aligned with their configured URLs', () => {
    const config = parseSidebarModulesAdmin(
      JSON.stringify({
        personal: {
          shop: true,
          shop_url: [
            'https://shop.example.com/recharge',
            'https://shop.example.com/credits',
          ],
          shop_names: ['充值中心', '积分商城'],
        },
      })
    )

    const entries = getSidebarShopEntries(config.personal)
    expect(entries).toEqual([
      { url: 'https://shop.example.com/recharge', name: '充值中心' },
      { url: 'https://shop.example.com/credits', name: '积分商城' },
    ])
    const firstEntry = entries[0]
    expect(firstEntry).toBeDefined()
    if (!firstEntry) return
    expect(getSidebarShopEntryTitle(firstEntry, 0, entries.length)).toBe(
      '充值中心'
    )
  })

  it('uses numbered fallback names when a custom name is blank', () => {
    const entries = getSidebarShopEntries({
      shop_url: [
        'https://shop.example.com/recharge',
        'https://shop.example.com/credits',
      ],
      shop_names: ['', '积分商城'],
    })

    const [firstEntry, secondEntry] = entries
    expect(firstEntry).toBeDefined()
    expect(secondEntry).toBeDefined()
    if (!firstEntry || !secondEntry) return
    expect(getSidebarShopEntryTitle(firstEntry, 0, entries.length)).toBe(
      '小铺充值 1'
    )
    expect(getSidebarShopEntryTitle(secondEntry, 1, entries.length)).toBe(
      '积分商城'
    )
  })
})
