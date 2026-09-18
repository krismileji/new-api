import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
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
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { emptyChannelLimitGroup } from '../../lib/limit-group'
import type { ChannelMonitorItem } from '../../types'
import { ChannelLimitGroupsDialog } from '../channel-limit-groups-dialog'

const originalAdapter = api.defaults.adapter
let client: QueryClient
afterEach(() => {
  cleanup()
  api.defaults.adapter = originalAdapter
  client?.clear()
})

function mount() {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ChannelLimitGroupsDialog
        channels={[{ id: 1, name: '重要渠道' }] as ChannelMonitorItem[]}
        onOpenChange={() => {}}
      />
    </QueryClientProvider>
  )
}

test('运行中的组可直接删除，确认后提交原版本', async () => {
  const requests: unknown[] = []
  const group = {
    ...emptyChannelLimitGroup(),
    id: 7,
    name: '共享上游',
    revision: 8,
    members: [{ channel_id: 1, priority: 100 }],
    runtime: { active: 1, rpm: 5, waiting: 0 },
  }
  api.defaults.adapter = async (config) => {
    if (config.method === 'delete') {
      requests.push({
        url: config.url,
        params: config.params,
      })
    }
    return {
      data: { success: true, data: config.method === 'get' ? [group] : null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  mount()
  fireEvent.click(await screen.findByRole('button', { name: '删除' }))
  expect(requests).toHaveLength(0)
  expect(screen.getByRole('alertdialog')).toHaveTextContent('已有请求继续执行')
  fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
  await waitFor(() =>
    expect(requests).toEqual([
      {
        url: '/api/channel_monitor/limit-groups/7',
        params: { revision: 8 },
      },
    ])
  )
})

test('运行中展示真实等级用量，暂停失败时保留配置并显示原因', async () => {
  const group = {
    ...emptyChannelLimitGroup(),
    id: 7,
    name: '共享上游',
    revision: 8,
    enabled: true,
    concurrency_limit: 10,
    rpm_limit: 300,
    members: [{ channel_id: 1, priority: 100 }],
    runtime: {
      active: 2,
      rpm: 12,
      waiting: 1,
      tiers: [{ priority: 100, active: 2, rpm: 12 }],
    },
  }
  api.defaults.adapter = async (config) => ({
    data:
      config.method === 'get'
        ? { success: true, data: [group] }
        : { success: false, message: '共享限流配置已变化，请刷新后重试' },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  mount()
  await screen.findByText('共享上游')
  expect(screen.getByText(/已用 2 并发 \/ 12 RPM/)).toBeVisible()
  expect(
    screen.queryByRole('button', { name: '恢复异常状态' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '删除' })).not.toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '暂停准入' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    '共享限流配置已变化'
  )
  expect(screen.getByText('共享上游')).toBeVisible()
})
