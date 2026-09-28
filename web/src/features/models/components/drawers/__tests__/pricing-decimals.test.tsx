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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'

import type { Model } from '../../../types'
import { ModelMutateDrawer } from '../model-mutate-drawer'

test.each([
  { option: 'ModelPrice', label: 'Fixed price', expected: '0.0000002' },
  { option: 'ModelRatio', label: 'Input price', expected: '0.0000004' },
])('模型编辑器加载 $option 时显示完整小数', async ({ option, label, expected }) => {
  const previousAuth = useAuthStore.getState().auth
  useAuthStore.setState({ auth: { ...previousAuth, user: { ...previousAuth.user, role: 100 } as NonNullable<typeof previousAuth.user> } })
  usePricingPreferencesStore.setState({ currency: 'USD' })
  const model: Model = {
    id: 1,
    model_name: 'decimal-model',
    status: 1,
    sync_official: 1,
    created_time: 0,
    updated_time: 0,
    name_rule: 0,
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/option/model_pricing') {
      return {
        data: {
          success: true,
          data: { entries: [{model_name: 'decimal-model', version: '1', configured: { [option]: 2e-7 }, effective: { [option]: 2e-7 }}], options: {}, empty_version: '0' },
        },
      }
    }
    if (url === '/api/models/1') {
      return { data: { success: true, data: model } }
    }
    return { data: { success: true, data: { items: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelMutateDrawer
        currentRow={model}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )

  fireEvent.click(screen.getByRole('tab', { name: 'Pricing' }))
  await waitFor(() =>
    expect(screen.getByLabelText(label)).toHaveValue(expected)
  )
  client.clear()
  useAuthStore.setState({ auth: previousAuth })
})
