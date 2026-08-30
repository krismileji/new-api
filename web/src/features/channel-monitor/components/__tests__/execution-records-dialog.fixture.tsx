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

import { Window } from 'happy-dom'
import type { ReactNode } from 'react'

import type {
  ChannelMonitorSmartScheduleExecutionDetailPage,
  ChannelMonitorTask,
  ChannelMonitorTaskAdjustment,
} from '../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLFormElement',
  'HTMLInputElement',
  'HTMLSelectElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'InputEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'FormData',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
Object.defineProperty(domWindow.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { ChannelMonitorTaskHistoryDialog } =
  await import('../channel-monitor-task-history-dialog')
const { ChannelMonitorSmartScheduleExecutionDialog } =
  await import('../channel-monitor-smart-schedule-execution-dialog')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createTask(
  type: ChannelMonitorTask['type'],
  taskId: string
): ChannelMonitorTask {
  return {
    id: 1,
    task_id: taskId,
    type,
    status: 'succeeded',
    state: null,
    result:
      type === 'channel_smart_schedule'
        ? {
            total: 1,
            updated: 1,
            unchanged: 0,
            skipped: 0,
            failed: 0,
            group_policy_count: 2,
            performance_window_minutes: 60,
            stability_window_minutes: 30,
          }
        : {
            total: 4,
            updated: 4,
            changed: 2,
            balance_updated: 3,
            failed: 0,
          },
    error: '',
    created_at: 1_752_777_845,
    updated_at: 1_752_777_848,
  }
}

function createAdjustment(
  overrides: Partial<ChannelMonitorTaskAdjustment> = {}
): ChannelMonitorTaskAdjustment {
  return {
    channel_id: 7,
    channel_name: '高速稳定渠道',
    group: 'default',
    model: 'gpt-5',
    action: 'updated',
    old_priority: 80,
    new_priority: 100,
    old_weight: 20,
    new_weight: 90,
    score: 0.96,
    previous_effective_time: 1_752_700_000,
    previous_effective_priority: 80,
    previous_effective_weight: 20,
    reason: '综合评分最高，提升为主渠道',
    ...overrides,
  }
}

function createDetailResponse(
  items: ChannelMonitorTaskAdjustment[],
  options: Partial<ChannelMonitorSmartScheduleExecutionDetailPage> = {}
) {
  const allItems = options.items ?? items
  return {
    success: true,
    message: '',
    data: {
      page: 1,
      page_size: 50,
      total: allItems.length,
      items: allItems,
      groups: [...new Set(items.map((item) => item.group))].sort(),
      models: [...new Set(items.map((item) => item.model))].sort(),
      channel_names: Object.fromEntries(
        allItems.map((item) => [String(item.channel_id), item.channel_name])
      ),
      ...options,
    },
  }
}

async function renderDialog(
  element: ReactNode,
  queryClient: InstanceType<typeof QueryClient>
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
  return { container, dialog, root }
}

function waitForDialogText(dialog: HTMLElement, expected: string) {
  if (dialog.textContent?.includes(expected)) return Promise.resolve()

  return new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(() => {
      observer.disconnect()
      reject(new Error(`弹窗未显示预期内容：${expected}`))
    }, 10_000)
    const observer = new MutationObserver(() => {
      if (!dialog.textContent?.includes(expected)) return
      clearTimeout(timeout)
      observer.disconnect()
      resolve()
    })
    observer.observe(dialog, { childList: true, subtree: true })
  })
}

const ratioQueryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
ratioQueryClient.setQueryData(
  ['channel-monitor-task-history', 'ratio', 1, 20],
  {
    success: true,
    message: '',
    data: {
      page: 1,
      page_size: 20,
      total: 1,
      items: [createTask('channel_ratio_monitor', 'ratio-1')],
    },
  }
)
const ratioRendered = await renderDialog(
  <ChannelMonitorTaskHistoryDialog open onOpenChange={() => {}} />,
  ratioQueryClient
)
assert.ok(ratioRendered.dialog.textContent?.includes('倍率与余额更新记录'))
assert.ok(ratioRendered.dialog.classList.contains('max-h-[calc(100dvh-2rem)]'))
assert.equal(
  [...ratioRendered.dialog.classList].some((className) =>
    className.startsWith('h-[')
  ),
  false
)
assert.equal(ratioRendered.dialog.querySelectorAll('[role="tab"]').length, 0)
assert.equal(
  ratioRendered.dialog.textContent?.includes('智能调度执行详情'),
  false
)
assert.ok(ratioRendered.dialog.textContent?.includes('当前页'))
assert.ok(ratioRendered.dialog.textContent?.includes('联动操作'))
const ratioTable = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-slot="table"]'
)
const ratioTableHeader = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-slot="table-header"]'
)
const ratioTaskRow = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-slot="table-body"] > [data-slot="table-row"]'
)
assert.ok(ratioTable?.className.includes('min-w-0'))
assert.ok(ratioTable?.className.includes('sm:min-w-[920px]'))
assert.ok(ratioTableHeader?.className.includes('hidden'))
assert.ok(ratioTableHeader?.className.includes('sm:table-header-group'))
assert.ok(ratioTaskRow?.className.includes('grid'))
assert.ok(ratioTaskRow?.className.includes('sm:table-row'))
assert.ok(ratioTaskRow?.textContent?.includes('执行结果'))
assert.ok(ratioTaskRow?.textContent?.includes('联动操作'))
const ratioButtons = [...ratioRendered.dialog.querySelectorAll('button')]
assert.ok(
  ratioButtons.some((button) =>
    button.textContent?.includes('立即更新倍率和余额')
  )
)
assert.equal(
  ratioButtons.some((button) => button.textContent?.includes('执行智能调度')),
  false
)
await act(async () => ratioRendered.root.unmount())
ratioRendered.container.remove()
ratioQueryClient.clear()

const firstTask = createTask('channel_smart_schedule', 'schedule-1')
const secondTask = createTask('channel_smart_schedule', 'schedule-2')
secondTask.created_at += 120
secondTask.updated_at += 120
const firstAdjustment = createAdjustment()
const firstPageTwoAdjustment = createAdjustment({
  channel_id: 9,
  channel_name: '第二页备用渠道',
  reason: '明细分页后的第二页记录',
})
const secondAdjustment = createAdjustment({
  channel_id: 8,
  channel_name: '低成本备用渠道',
  group: 'vip',
  model: 'gpt-5-mini',
  reason: '按优先级递减采样获得少量真实流量',
})
const scheduleQueryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
scheduleQueryClient.setQueryData(
  ['channel-monitor-smart-schedule-executions', 1],
  {
    success: true,
    message: '',
    data: {
      page: 1,
      page_size: 20,
      total: 2,
      items: [firstTask, secondTask],
    },
  }
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-1',
    1,
    '',
    'all',
    'all',
    'all',
  ],
  createDetailResponse([firstAdjustment, firstPageTwoAdjustment], {
    total: 51,
    items: [firstAdjustment],
  })
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-1',
    2,
    '',
    'all',
    'all',
    'all',
  ],
  createDetailResponse([firstAdjustment, firstPageTwoAdjustment], {
    page: 2,
    total: 51,
    items: [firstPageTwoAdjustment],
  })
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-1',
    1,
    '不存在的渠道',
    'all',
    'all',
    'all',
  ],
  createDetailResponse([firstAdjustment, firstPageTwoAdjustment], {
    total: 0,
    items: [],
  })
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-2',
    1,
    '',
    'all',
    'all',
    'all',
  ],
  createDetailResponse([secondAdjustment])
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-2',
    1,
    '低成本备用渠道',
    'all',
    'all',
    'all',
  ],
  createDetailResponse([secondAdjustment])
)
const scheduleRendered = await renderDialog(
  <ChannelMonitorSmartScheduleExecutionDialog open onOpenChange={() => {}} />,
  scheduleQueryClient
)
assert.ok(scheduleRendered.dialog.textContent?.includes('智能调度执行记录'))
assert.ok(
  scheduleRendered.dialog.classList.contains('max-h-[calc(100dvh-2rem)]')
)
assert.equal(
  [...scheduleRendered.dialog.classList].some((className) =>
    className.startsWith('h-[')
  ),
  false
)
assert.equal(scheduleRendered.dialog.querySelectorAll('[role="tab"]').length, 0)
assert.ok(scheduleRendered.dialog.textContent?.includes('高速稳定渠道'))
assert.ok(scheduleRendered.dialog.textContent?.includes('按 2 个分组策略执行'))
const detailNextButton =
  scheduleRendered.dialog.querySelector<HTMLButtonElement>(
    '[aria-label="下一页明细"]'
  )
assert.ok(detailNextButton)
await act(async () => detailNextButton.click())
assert.ok(scheduleRendered.dialog.textContent?.includes('第二页备用渠道'))
const search = scheduleRendered.dialog.querySelector<HTMLInputElement>(
  '[aria-label="搜索执行明细"]'
)
assert.ok(search)
assert.ok(
  search.closest('.group\\/input-group')?.classList.contains('ring-inset')
)
const setInputValue = Object.getOwnPropertyDescriptor(
  domWindow.HTMLInputElement.prototype,
  'value'
)?.set
assert.ok(setInputValue)
await act(async () => {
  setInputValue.call(search, '不存在的渠道')
  search.dispatchEvent(new Event('input', { bubbles: true }))
})
await waitForDialogText(scheduleRendered.dialog, '没有匹配的路由记录')
const taskButtons = scheduleRendered.dialog.querySelectorAll<HTMLButtonElement>(
  'button[aria-pressed]'
)
assert.equal(taskButtons.length, 2)
await act(async () => taskButtons[1]?.click())
assert.equal(search.value, '')
assert.ok(scheduleRendered.dialog.textContent?.includes('低成本备用渠道'))

await act(async () => {
  setInputValue.call(search, '低成本备用渠道')
  search.dispatchEvent(new Event('input', { bubbles: true }))
})
const replacementTask = createTask('channel_smart_schedule', 'schedule-3')
const replacementAdjustment = createAdjustment({
  channel_id: 10,
  channel_name: '轮询新增渠道',
})
await act(async () => {
  scheduleQueryClient.setQueryData(
    [
      'channel-monitor-smart-schedule-executions',
      'details',
      'schedule-3',
      1,
      '',
      'all',
      'all',
      'all',
    ],
    createDetailResponse([replacementAdjustment])
  )
  scheduleQueryClient.setQueryData(
    ['channel-monitor-smart-schedule-executions', 1],
    {
      success: true,
      message: '',
      data: {
        page: 1,
        page_size: 20,
        total: 1,
        items: [replacementTask],
      },
    }
  )
  await waitForDialogText(scheduleRendered.dialog, '轮询新增渠道')
})
assert.equal(search.value, '')
assert.ok(scheduleRendered.dialog.textContent?.includes('轮询新增渠道'))
assert.equal(
  scheduleRendered.dialog.textContent?.includes('低成本备用渠道'),
  false
)
await act(async () => scheduleRendered.root.unmount())
scheduleRendered.container.remove()
scheduleQueryClient.clear()
domWindow.close()
