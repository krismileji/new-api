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
import { render, screen, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { Model } from '../../../types'
import { ModelMutateDrawer } from '../model-mutate-drawer'

test.each([
  { option: 'ModelPrice', label: 'Fixed price (USD)' },
  { option: 'ModelRatio', label: 'Model ratio' },
])('模型编辑器加载 $option 时显示完整小数', async ({ option, label }) => {
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
    if (url === '/api/option/') {
      return {
        data: {
          success: true,
          data: [{ key: option, value: '{"decimal-model":2e-7}' }],
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

  await waitFor(() =>
    expect(screen.getByLabelText(label)).toHaveValue('0.0000002')
  )
  client.clear()
})
