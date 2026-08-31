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
import assert from 'node:assert/strict'

import type { AxiosAdapter } from 'axios'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactElement } from 'react'

import { api } from '@/lib/api'

import type { ChannelMonitorItem } from '../../types'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { channelMonitorDialogContentClassName } =
  await import('../channel-monitor-dialog-layout')
const { ChannelMonitorOrderDialog } =
  await import('../channel-monitor-order-dialog')
const { ChannelMonitorSuccessDetailDialog } =
  await import('../channel-monitor-success-detail-dialog')
const { EditGroupChannelsDialog } =
  await import('../edit-group-channels-dialog')

function createChannel(
  overrides: Partial<ChannelMonitorItem> = {}
): ChannelMonitorItem {
  return {
    id: 7,
    name: '测试渠道',
    type: 1,
    status: 1,
    status_reason: '',
    priority: 0,
    weight: 0,
    base_url: 'https://example.com',
    models: 'test-model',
    test_model: 'test-model',
    groups: ['default'],
    ratio: 1,
    previous_ratio: 1,
    cost_ratio: 1,
    previous_cost_ratio: 1,
    conversion_factor: 1,
    remark: '',
    channel_remark: '',
    updated_time: 0,
    updated_by: 0,
    updated_by_username: '',
    last_fetch_status: '',
    last_fetch_error: '',
    last_fetch_time: 0,
    consecutive_failures: 0,
    upstream_balance: null,
    last_balance_time: 0,
    last_balance_error: '',
    today_cost_cny: 0,
    today_cost_configured: false,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    current_rpm: 0,
    upstream: null,
    ...overrides,
  }
}

async function renderDialog(
  element: ReactElement,
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    },
  })
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>{element}</QueryClientProvider>
    )
  })
  const dialog = document.body.querySelector<HTMLElement>(
    '[data-slot="dialog-content"]'
  )
  assert.ok(dialog)

  return {
    dialog,
    async cleanup() {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    },
  }
}

const contentClasses =
  channelMonitorDialogContentClassName('flex flex-col').split(/\s+/)
assert.ok(contentClasses.includes('max-h-[calc(100dvh-2rem)]'))
assert.ok(contentClasses.includes('overflow-hidden'))
assert.equal(
  contentClasses.some((value) => value.startsWith('h-[')),
  false
)

const channel = createChannel()
const orderRendered = await renderDialog(
  <ChannelMonitorOrderDialog
    channels={[channel]}
    channelOrder={[channel.id]}
    open
    onOpenChange={() => {}}
  />
)
const orderList = orderRendered.dialog.querySelector<HTMLElement>(
  '[data-slot="channel-order-list"]'
)
assert.ok(orderList)
assert.ok(orderList.classList.contains('min-h-0'))
assert.ok(orderList.classList.contains('overflow-y-auto'))
assert.equal(
  [...orderList.classList].some((value) => value.startsWith('h-[')),
  false
)
assert.equal(
  [...orderList.classList].some((value) => value.startsWith('max-h-[')),
  false
)
await orderRendered.cleanup()

const groupRendered = await renderDialog(
  <EditGroupChannelsDialog
    group={{
      name: 'default',
      ratio: 1,
      coefficient: 1,
      channels: [channel],
    }}
    channels={[channel]}
    open
    onOpenChange={() => {}}
  />
)
const groupList = groupRendered.dialog.querySelector<HTMLElement>(
  '[data-slot="group-channel-list"]'
)
assert.ok(groupList)
const groupSearch = groupRendered.dialog.querySelector<HTMLInputElement>(
  '[aria-label="搜索渠道"]'
)
assert.ok(groupSearch)
assert.ok(
  groupSearch.closest('.group\\/input-group')?.classList.contains('ring-inset')
)
assert.ok(groupList.classList.contains('min-h-0'))
assert.ok(groupList.classList.contains('flex-1'))
assert.ok(groupList.classList.contains('overflow-y-auto'))
assert.equal(
  [...groupList.classList].some((value) => value.startsWith('h-[')),
  false
)
await groupRendered.cleanup()

const successQueryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
  },
})
successQueryClient.setQueryData(
  ['channel-monitor-success-detail', 1440, 'channel', 7, undefined],
  {
    success: true,
    message: '',
    data: {
      range_minutes: 1440,
      generated_at: 1_752_777_845,
      success_metrics_available: true,
      scope: 'channel',
      detail: {
        summary: {
          actual_success_count: 9,
          actual_failure_count: 1,
          actual_sample_count: 10,
          actual_success_rate: 0.9,
          final_success_count: 9,
          final_failure_count: 1,
          final_sample_count: 10,
          final_success_rate: 0.9,
          cache_hit_count: 0,
          cache_sample_count: 0,
          cache_hit_rate: 0,
          cache_read_tokens: 0,
          input_tokens: 0,
          cache_utilization_rate: 0,
        },
        channel_items: [],
        api_key_items: [],
        failure_categories: [
          {
            channel_id: 7,
            status_code: 503,
            error_type: 'upstream_error',
            error_code: 'service_unavailable',
            sample_content: '上游暂时不可用',
            actual_count: 1,
            final_count: 1,
            last_occurred_at: 1_752_777_845,
          },
        ],
      },
    },
  }
)
const successDetailQueryKey = [
  'channel-monitor-success-detail',
  1440,
  'channel',
  7,
  undefined,
] as const
const originalAdapter = api.defaults.adapter
const successDetailAdapter: AxiosAdapter = async (config) => ({
  data: successQueryClient.getQueryData(successDetailQueryKey),
  status: 200,
  statusText: 'OK',
  headers: {},
  config,
})
api.defaults.adapter = successDetailAdapter

const successRendered = await renderDialog(
  <ChannelMonitorSuccessDetailDialog
    target={{
      scope: 'channel',
      mode: 'actual',
      channelId: 7,
      channelName: '测试渠道',
    }}
    channels={[]}
    rangeMinutes={1440}
    rangeLabel='24 小时'
    open
    onOpenChange={() => {}}
  />,
  successQueryClient
)
await act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0))
})
api.defaults.adapter = originalAdapter
const failureTable = [
  ...successRendered.dialog.querySelectorAll<HTMLElement>(
    '[data-slot="table"]'
  ),
].find((table) => table.classList.contains('min-w-[860px]'))
assert.ok(successRendered.dialog.classList.contains('sm:max-w-5xl'))
assert.ok(failureTable)
assert.ok(failureTable.parentElement?.classList.contains('overflow-x-auto'))
await successRendered.cleanup()

process.stdout.write('ok\n')
