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
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelMonitorSmartSchedulePrimaryManagement } from '../../types'
import { ChannelMonitorSmartSchedulePrimaryManagementDialog } from '../channel-monitor-smart-schedule-primary-management-dialog'

const originalAdapter = api.defaults.adapter
let client: QueryClient

afterEach(() => {
  cleanup()
  api.defaults.adapter = originalAdapter
  client?.clear()
})

Object.defineProperty(Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})

function management(): ChannelMonitorSmartSchedulePrimaryManagement {
  const manualPrimaryUntil = Math.floor(Date.now() / 1000) + 3600
  return {
    channel_rules: [
      {
        channel_id: 11,
        channel_name: '固定渠道甲',
        manual_primary_until: manualPrimaryUntil,
        allow_stability_degrade: true,
        updated_at: 1_800_000_000,
        routes: [
          {
            group: 'vip',
            model: 'model-b',
            status: 'channel_rule_conflict',
            current_primary_channel_id: 12,
            current_primary_channel_name: '固定渠道乙',
          },
        ],
      },
    ],
    route_primaries: [
      {
        channel_id: 21,
        channel_name: '单模型渠道',
        group: 'default',
        model: 'model-a',
        manual_primary_until: manualPrimaryUntil,
        allow_stability_degrade: false,
      },
    ],
  }
}

function mount(onActionComplete = vi.fn(async () => {})) {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ChannelMonitorSmartSchedulePrimaryManagementDialog
        open
        onOpenChange={() => {}}
        onActionComplete={onActionComplete}
      />
    </QueryClientProvider>
  )
}

test('search filters both rule and route entries and updates the selected channel rule', async () => {
  const requests: Array<{ url?: string; method?: string; data?: unknown }> = []
  api.defaults.adapter = async (config) => {
    requests.push({
      url: config.url,
      method: config.method,
      data:
        typeof config.data === 'string' ? JSON.parse(config.data) : config.data,
    })
    return {
      data:
        config.method === 'get'
          ? { success: true, data: management() }
          : { success: true, data: null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const onActionComplete = vi.fn(async () => {})
  const user = userEvent.setup()
  mount(onActionComplete)

  expect(await screen.findByText(/单模型固定/)).toBeVisible()
  expect(screen.getByText('优先应用 固定渠道乙 的更新规则')).toBeVisible()
  await user.type(
    screen.getByRole('searchbox', { name: '搜索固定项' }),
    'model-b'
  )
  expect(screen.getByText('固定渠道甲')).toBeVisible()
  expect(screen.queryByText('单模型渠道')).not.toBeInTheDocument()

  fireEvent.change(
    screen.getByRole('spinbutton', { name: '固定渠道甲 固定时长（分钟）' }),
    {
      target: { value: '120' },
    }
  )
  await user.click(screen.getByRole('button', { name: '更新规则' }))

  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        url: '/api/channel_monitor/channel/11/schedule/primary-rule',
        method: 'put',
        data: expect.objectContaining({
          duration_minutes: 120,
          allow_stability_degrade: true,
        }),
      })
    )
  )
  expect(onActionComplete).toHaveBeenCalledOnce()
})

test('search finds a single-model pin and removes only that route', async () => {
  const requests: Array<{ url?: string; method?: string; data?: unknown }> = []
  api.defaults.adapter = async (config) => {
    requests.push({
      url: config.url,
      method: config.method,
      data:
        typeof config.data === 'string' ? JSON.parse(config.data) : config.data,
    })
    return {
      data:
        config.method === 'get'
          ? { success: true, data: management() }
          : { success: true, data: null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const user = userEvent.setup()
  mount()

  await screen.findByText('单模型渠道')
  await user.type(
    screen.getByRole('searchbox', { name: '搜索固定项' }),
    'model-a'
  )
  await user.click(screen.getByRole('button', { name: '解除固定' }))

  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        url: '/api/channel_monitor/channel/21/schedule/route/primary',
        method: 'put',
        data: expect.objectContaining({
          group: 'default',
          model: 'model-a',
          duration_minutes: 0,
        }),
      })
    )
  )
})
