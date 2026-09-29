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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import type { UpdateOptionRequest } from '../../types'
import {
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '../config'
import { SidebarModulesSection } from '../sidebar-modules-section'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const config = parseSidebarModulesAdmin(
  JSON.stringify({
    personal: {
      enabled: true,
      shop: true,
      shop_url: ['https://first.example.com', 'https://second.example.com'],
      shop_names: ['First', 'Second'],
    },
  })
)

function Fixture() {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <SidebarModulesSection
        config={config}
        initialSerialized={serializeSidebarModulesAdmin(config)}
      />
    </SettingsPageProvider>
  )
}

describe('shop recharge link editing', () => {
  it('keeps focus during typing and saves the remaining URL with its name after removal', async () => {
    const user = userEvent.setup()
    const request = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <Fixture />
      </QueryClientProvider>
    )

    const url = screen.getByRole('textbox', { name: '小铺充值链接 2' })
    await user.type(url, '/recharge')
    expect(url).toHaveFocus()
    expect(url).toHaveValue('https://second.example.com/recharge')

    await user.click(
      screen.getByRole('button', { name: '删除第 1 个小铺充值链接' })
    )
    const remaining = screen.getByRole('textbox', { name: '小铺充值链接 1' })
    expect(remaining).toHaveValue('https://second.example.com/recharge')
    expect(
      screen.getByRole('textbox', { name: '小铺充值菜单名称 1' })
    ).toHaveValue('Second')
    await user.type(remaining, '?plan=pro')
    expect(remaining).toHaveFocus()

    await user.click(
      screen.getByRole('button', { name: 'Save sidebar modules' })
    )
    await waitFor(() => expect(request).toHaveBeenCalledOnce())
    expect(request.mock.calls[0][0]).toBe('/api/option/')
    const payload = request.mock.calls[0][1] as UpdateOptionRequest
    expect(payload.key).toBe('SidebarModulesAdmin')
    const saved = JSON.parse(String(payload.value))
    expect(saved.personal.shop_url).toEqual([
      'https://second.example.com/recharge?plan=pro',
    ])
    expect(saved.personal.shop_names).toEqual(['Second'])
    expect(saved.personal.security).toBe(true)
    expect(saved.console.audit).toBe(true)
    client.clear()
  })
})
