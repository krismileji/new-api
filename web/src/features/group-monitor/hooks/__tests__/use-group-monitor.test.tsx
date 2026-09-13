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
import {
  focusManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { GroupMonitorContent } from '../../index'
import type { PricingGroupMonitor } from '../../types'
import { useGroupMonitor } from '../use-group-monitor'

const originalAdapter = api.defaults.adapter
let queryClient: QueryClient
let response: PricingGroupMonitor

function monitorResult(category: string): PricingGroupMonitor {
  return {
    enabled: true,
    server_now: 1_789_298_100,
    data_cutoff_at: 1_789_294_500,
    display_value: 60,
    display_unit: 'minute',
    items: [
      {
        group: 'default',
        category,
        initial: 'D',
        status: 'unavailable',
        latest_first_token_ms: null,
        success_rate: null,
        last_finished_at: 0,
        recent_window: [],
      },
    ],
  }
}

function MonitorPage() {
  const query = useGroupMonitor()
  if (!query.data) return <p>正在加载</p>
  return <GroupMonitorContent result={query.data.data} />
}

function renderMonitor() {
  return render(
    <QueryClientProvider client={queryClient}>
      <MonitorPage />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  focusManager.setFocused(true)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  response = monitorResult('88')
  api.defaults.adapter = async (config) => ({
    config,
    data: { success: true, data: response },
    status: 200,
    statusText: 'OK',
    headers: {},
  })
})

afterEach(() => {
  queryClient.clear()
  api.defaults.adapter = originalAdapter
  focusManager.setFocused(undefined)
})

test('在另一标签保存分类后切回展示页会刷新仍在缓存期内的数据', async () => {
  renderMonitor()
  expect(await screen.findByRole('region', { name: '88' })).toBeVisible()

  act(() => focusManager.setFocused(false))
  response = monitorResult('77')
  act(() => focusManager.setFocused(true))

  expect(await screen.findByRole('region', { name: '77' })).toBeVisible()
  expect(screen.queryByRole('region', { name: '88' })).not.toBeInTheDocument()
})

test('保存分类后立即重新进入展示页会读取新分类', async () => {
  const view = renderMonitor()
  expect(await screen.findByRole('region', { name: '88' })).toBeVisible()
  view.unmount()

  response = monitorResult('77')
  renderMonitor()

  expect(await screen.findByRole('region', { name: '77' })).toBeVisible()
  expect(screen.queryByRole('region', { name: '88' })).not.toBeInTheDocument()
})
