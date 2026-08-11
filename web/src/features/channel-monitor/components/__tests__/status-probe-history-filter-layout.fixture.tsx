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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import type { ChannelStatusProbeChannel } from '../../types'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelStatusProbeHistorySheet } =
  await import('../channel-status-probe-history-sheet')

const channel: ChannelStatusProbeChannel = {
  id: 42,
  name: '演示渠道',
  type: 1,
  channel_status: 1,
  remark: '用于布局测试',
  groups: ['default'],
  cost_ratio: 0.8,
  supported_models: ['gpt-4.1-mini', 'gpt-4.1'],
  allows_custom_model: false,
  config: {
    id: 1,
    channel_id: 42,
    enabled: true,
    models: ['gpt-4.1-mini', 'gpt-4.1'],
    interval_seconds: 300,
    display_value: 60,
    display_unit: 'minute',
    record_sample: false,
    next_run_at: 1_700_000_300,
    manual_request_id: '',
    manual_requested_at: 0,
    revision: 1,
    running_trigger: '',
    running_run_id: '',
    running_started_at: 0,
    created_at: 1_700_000_000,
    updated_at: 1_700_000_000,
  },
  health_status: 'healthy',
  running: false,
  latest: null,
  avg_first_token_ms: 320,
  avg_tps: 42,
  model_statuses: [],
  configured_model_count: 2,
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
  },
})
queryClient.setQueryData(
  [
    'channel-monitor',
    'status-probe',
    'executions',
    channel.id,
    0,
    {
      page: 1,
      pageSize: 20,
      modelName: '',
      result: '',
      trigger: '',
    },
  ],
  {
    success: true,
    message: '',
    data: { page: 1, page_size: 20, total: 0, items: [] },
  }
)

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)
await act(async () => {
  root.render(
    <QueryClientProvider client={queryClient}>
      <ChannelStatusProbeHistorySheet
        channel={channel}
        open
        actionPending={false}
        onOpenChange={() => {}}
        onOpenConfig={() => {}}
        onRun={() => {}}
      />
    </QueryClientProvider>
  )
})

const filters = document.body.querySelector<HTMLElement>(
  '[data-slot="status-probe-history-filters"]'
)
assert.ok(filters)
assert.ok(filters.classList.contains('gap-2'))
assert.ok(filters.classList.contains('sm:flex-row'))
assert.ok(filters.classList.contains('sm:flex-wrap'))
assert.ok(filters.classList.contains('sm:justify-end'))
assert.equal(
  [...filters.classList].some((className) => className.includes('grid')),
  false
)

const triggerWidths = [
  ['按模型筛选执行记录', 'sm:w-48'],
  ['按结果筛选执行记录', 'sm:w-40'],
  ['按触发方式筛选执行记录', 'sm:w-40'],
] as const
for (const [label, widthClass] of triggerWidths) {
  const filterTrigger: HTMLElement | null = filters.querySelector<HTMLElement>(
    `[aria-label="${label}"]`
  )
  assert.ok(filterTrigger)
  assert.ok(filterTrigger.classList.contains('w-full'))
  assert.ok(filterTrigger.classList.contains(widthClass))
}

await act(async () => root.unmount())
container.remove()
queryClient.clear()
process.stdout.write('ok\n')
