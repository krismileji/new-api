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
import { Window } from 'happy-dom'
import type { ReactNode } from 'react'

import { api } from '@/lib/api'

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

let activeQueryClient: InstanceType<typeof QueryClient> | null = null
const originalAdapter = api.defaults.adapter
const cachedQueryAdapter = ((config) => {
  let queryKey: readonly unknown[]
  if (config.url?.includes('/details')) {
    const taskId = config.url.split('/').at(-2) ?? ''
    queryKey = [
      'channel-monitor-smart-schedule-executions',
      'details',
      taskId,
      config.params?.p ?? 1,
      config.params?.q ?? '',
      config.params?.group ?? '',
      config.params?.model ?? 'all',
      config.params?.action ?? 'all',
    ]
  } else if (config.params?.kind === 'schedule') {
    queryKey = [
      'channel-monitor-smart-schedule-executions',
      config.params?.p ?? 1,
    ]
  } else {
    queryKey = [
      'channel-monitor-task-history',
      'ratio',
      config.params?.p ?? 1,
      config.params?.page_size ?? 20,
    ]
  }
  const cachedData = activeQueryClient?.getQueryData<unknown>(queryKey)
  if (!cachedData) throw new Error(`Unexpected request: ${config.url}`)
  return Promise.resolve({
    config,
    data: cachedData,
    headers: {},
    status: 200,
    statusText: 'OK',
  })
}) as AxiosAdapter
api.defaults.adapter = cachedQueryAdapter

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
  const modelsByGroup = Object.fromEntries(
    [...new Set(items.map((item) => item.group))].map((group) => [
      group,
      [
        ...new Set(
          items.filter((item) => item.group === group).map((item) => item.model)
        ),
      ].sort(),
    ])
  )
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
      models_by_group: modelsByGroup,
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
  activeQueryClient = queryClient
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
assert.ok(ratioRendered.dialog.textContent?.includes('倍率变化'))
assert.ok(ratioRendered.dialog.textContent?.includes('余额刷新'))
assert.ok(ratioRendered.dialog.textContent?.includes('联动影响'))
assert.ok(
  ratioRendered.dialog.textContent?.includes(
    '检查 4 个渠道，2 个倍率发生变化，刷新 3 个渠道余额。'
  )
)
const ratioOverview = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-ratio-task-overview]'
)
const ratioTimeline = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-ratio-task-timeline]'
)
const ratioTaskRecord = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-ratio-task-record]'
)
const ratioTaskMetrics = ratioRendered.dialog.querySelector<HTMLElement>(
  '[data-ratio-task-metrics]'
)
assert.ok(ratioOverview)
assert.ok(ratioTimeline)
assert.ok(ratioTaskRecord)
assert.ok(ratioTaskMetrics)
assert.ok(ratioOverview.classList.contains('grid-cols-2'))
assert.ok(ratioOverview.classList.contains('lg:grid-cols-4'))
assert.equal(ratioRendered.dialog.querySelector('[data-slot="table"]'), null)
assert.ok(ratioTaskMetrics.classList.contains('grid-cols-2'))
assert.ok(ratioTaskMetrics.classList.contains('sm:grid-cols-4'))
assert.ok(ratioTaskMetrics.textContent?.includes('检查渠道'))
assert.ok(ratioTaskMetrics.textContent?.includes('上游失败'))
assert.ok(ratioTaskRecord.textContent?.includes('未触发分组或渠道状态联动'))
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
firstTask.created_at += 120
firstTask.updated_at += 120
const firstAdjustment = createAdjustment()
const firstPageTwoAdjustment = createAdjustment({
  channel_id: 9,
  channel_name: '第二页备用渠道',
  group: 'vip',
  model: 'gpt-5',
  reason: '明细分页后的第二页记录',
})
const secondAdjustment = createAdjustment({
  channel_id: 8,
  channel_name: '低成本备用渠道',
  group: 'vip',
  model: 'gpt-5',
  reason: '按优先级递减采样获得少量真实流量',
})
const failedAdjustment = createAdjustment({
  channel_id: 11,
  channel_name: '筛选后的失败路由',
  action: 'failed',
  failure_stage: 'apply',
  reason: '筛选后仅保留的失败路由',
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
    'default',
    'gpt-5',
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
    'vip',
    'gpt-5',
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
    'vip',
    'gpt-5',
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
    'schedule-1',
    1,
    '',
    'default',
    'gpt-5',
    'failed',
  ],
  createDetailResponse([failedAdjustment])
)
scheduleQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'schedule-2',
    1,
    '',
    'vip',
    'gpt-5',
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
    'vip',
    'gpt-5',
    'all',
  ],
  createDetailResponse([secondAdjustment])
)
const scheduleRendered = await renderDialog(
  <ChannelMonitorSmartScheduleExecutionDialog
    open
    onOpenChange={() => {}}
    selection={{ group: 'default', model: 'gpt-5' }}
  />,
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
assert.ok(scheduleRendered.dialog.textContent?.includes('执行时间线'))
assert.ok(scheduleRendered.dialog.textContent?.includes('第 1 批执行'))
assert.ok(scheduleRendered.dialog.textContent?.includes('综合评分'))
assert.ok(scheduleRendered.dialog.textContent?.includes('调度理由'))
assert.ok(
  scheduleRendered.dialog.textContent?.includes('同优先级内按权重随机选择')
)
assert.ok(
  scheduleRendered.dialog.querySelector('[data-adjustment-action="updated"]')
)
assert.ok(
  scheduleRendered.dialog.querySelector('[data-task-status="succeeded"]')
)
const executionFilters = scheduleRendered.dialog.querySelector<HTMLElement>(
  '[data-schedule-execution-filters]'
)
assert.ok(executionFilters)
assert.ok(executionFilters.classList.contains('flex-col'))
assert.ok(executionFilters.classList.contains('sm:flex-wrap'))
assert.ok(executionFilters.classList.contains('xl:flex-nowrap'))
assert.equal(executionFilters.classList.contains('flex-wrap'), false)
const executionFilterTriggers = ['按分组筛选', '按模型筛选', '按结果筛选'].map(
  (label) => {
    const trigger = scheduleRendered.dialog.querySelector<HTMLButtonElement>(
      `[aria-label="${label}"]`
    )
    assert.ok(trigger)
    return trigger
  }
)
for (const trigger of executionFilterTriggers) {
  await act(async () => trigger.click())
  const content = document.body.querySelector<HTMLElement>(
    '[data-slot="select-content"]'
  )
  assert.equal(content?.dataset.alignTrigger, 'false')
  await act(async () => trigger.click())
}
const actionFilterTrigger = executionFilterTriggers[2]
assert.ok(actionFilterTrigger)
await act(async () => actionFilterTrigger.click())
const failedFilterOption = [
  ...document.body.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
].find((item) => item.textContent?.trim() === '失败')
assert.ok(failedFilterOption)
await act(async () => failedFilterOption.click())
await waitForDialogText(scheduleRendered.dialog, '筛选后的失败路由')
assert.ok(scheduleRendered.dialog.textContent?.includes('清除筛选'))
await act(async () => actionFilterTrigger.click())
const allActionFilterOption = [
  ...document.body.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
].find((item) => item.textContent?.trim() === '全部结果')
assert.ok(allActionFilterOption)
await act(async () => allActionFilterOption.click())
await waitForDialogText(scheduleRendered.dialog, '高速稳定渠道')
let resolveGroupFilterRequest: (() => void) | undefined
api.defaults.adapter = ((config) => {
  assert.equal(config.url, '/api/channel_monitor/tasks/schedule-1/details')
  assert.equal(config.params?.group, 'vip')
  return new Promise((resolve) => {
    resolveGroupFilterRequest = () =>
      resolve({
        config,
        data: createDetailResponse([secondAdjustment], { total: 51 }),
        headers: {},
        status: 200,
        statusText: 'OK',
      })
  })
}) as AxiosAdapter
const groupFilterTrigger = executionFilterTriggers[0]
assert.ok(groupFilterTrigger)
await act(async () => groupFilterTrigger.click())
const vipGroupFilterOption = [
  ...document.body.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
].find((item) => item.textContent?.trim() === 'vip')
assert.ok(vipGroupFilterOption)
await act(async () => vipGroupFilterOption.click())
assert.ok(resolveGroupFilterRequest)
assert.ok(groupFilterTrigger.textContent?.includes('vip'))
assert.ok(
  scheduleRendered.dialog.querySelector(
    '[data-schedule-execution-detail-loading]'
  )
)
assert.equal(
  scheduleRendered.dialog.textContent?.includes('高速稳定渠道'),
  false
)
assert.ok(scheduleRendered.dialog.textContent?.includes('正在加载执行明细'))
await act(async () => resolveGroupFilterRequest?.())
await waitForDialogText(scheduleRendered.dialog, '低成本备用渠道')
assert.equal(
  scheduleRendered.dialog.textContent?.includes('高速稳定渠道'),
  false
)
await act(async () => groupFilterTrigger.click())
assert.equal(
  [
    ...document.body.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
  ].some((item) => item.textContent?.trim() === '全部分组'),
  false
)
await act(async () => groupFilterTrigger.click())
api.defaults.adapter = cachedQueryAdapter
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
  group: 'vip',
  model: 'gpt-5',
})
await act(async () => {
  scheduleQueryClient.setQueryData(
    [
      'channel-monitor-smart-schedule-executions',
      'details',
      'schedule-3',
      1,
      '',
      'vip',
      'gpt-5',
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

domWindow.localStorage.clear()
const preferenceNewestTask = createTask(
  'channel_smart_schedule',
  'preference-newest'
)
preferenceNewestTask.created_at += 120
preferenceNewestTask.updated_at += 120
const preferenceOldestTask = createTask(
  'channel_smart_schedule',
  'preference-oldest'
)
const lowerWeightAdjustment = createAdjustment({
  channel_id: 21,
  channel_name: '同优先级低权重渠道',
  group: 'vip',
  model: 'gpt-5',
  new_priority: 100,
  new_weight: 20,
})
const higherWeightAdjustment = createAdjustment({
  channel_id: 22,
  channel_name: '同优先级高权重渠道',
  group: 'vip',
  model: 'gpt-5',
  new_priority: 100,
  new_weight: 80,
})
const longDefaultModelName = 'gpt-5-mini-long-context-preview-model-2026-08-30'
const defaultGroupAdjustment = createAdjustment({
  channel_id: 23,
  channel_name: '默认分组首个模型渠道',
  group: 'default',
  model: longDefaultModelName,
  new_priority: 90,
  new_weight: 100,
})
const preferenceQueryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
preferenceQueryClient.setQueryData(
  ['channel-monitor-smart-schedule-executions', 1],
  {
    success: true,
    message: '',
    data: {
      page: 1,
      page_size: 20,
      total: 2,
      items: [preferenceOldestTask, preferenceNewestTask],
    },
  }
)
preferenceQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'preference-newest',
    1,
    '',
    '',
    '',
    'all',
  ],
  createDetailResponse([
    lowerWeightAdjustment,
    higherWeightAdjustment,
    defaultGroupAdjustment,
  ])
)
preferenceQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'preference-newest',
    1,
    '',
    'vip',
    'gpt-5',
    'all',
  ],
  createDetailResponse([lowerWeightAdjustment, higherWeightAdjustment], {
    groups: ['default', 'vip'],
    models: ['gpt-5', longDefaultModelName],
    models_by_group: {
      vip: ['gpt-5'],
      default: [longDefaultModelName],
    },
  })
)
preferenceQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'preference-newest',
    1,
    '',
    'default',
    longDefaultModelName,
    'all',
  ],
  createDetailResponse([defaultGroupAdjustment], {
    groups: ['default', 'vip'],
    models: ['gpt-5', longDefaultModelName],
    models_by_group: {
      vip: ['gpt-5'],
      default: [longDefaultModelName],
    },
  })
)
const preferenceModelsByGroup = new Map<string, readonly string[]>([
  ['vip', ['gpt-5']],
  ['default', [longDefaultModelName]],
])
const preferenceRendered = await renderDialog(
  <ChannelMonitorSmartScheduleExecutionDialog
    open
    onOpenChange={() => {}}
    groupOrder={['vip', 'default']}
    modelsByGroup={preferenceModelsByGroup}
  />,
  preferenceQueryClient
)
await waitForDialogText(preferenceRendered.dialog, '同优先级高权重渠道')
const preferenceTaskButtons =
  preferenceRendered.dialog.querySelectorAll<HTMLButtonElement>(
    'button[aria-pressed]'
  )
assert.ok(preferenceTaskButtons[0]?.textContent?.includes('preference-n'))
const preferenceGroupTrigger =
  preferenceRendered.dialog.querySelector<HTMLButtonElement>(
    '[aria-label="按分组筛选"]'
  )
const preferenceModelTrigger =
  preferenceRendered.dialog.querySelector<HTMLButtonElement>(
    '[aria-label="按模型筛选"]'
  )
assert.ok(preferenceGroupTrigger?.textContent?.includes('vip'))
assert.ok(preferenceModelTrigger?.textContent?.includes('gpt-5'))
assert.ok(preferenceModelTrigger?.classList.contains('w-full'))
assert.ok(preferenceModelTrigger?.classList.contains('min-w-0'))
assert.equal(preferenceModelTrigger?.title, 'gpt-5')
assert.ok(preferenceModelTrigger)
await act(async () => preferenceModelTrigger.click())
const vipModelContent = [
  ...document.body.querySelectorAll<HTMLElement>(
    '[data-slot="select-content"]'
  ),
].find((content) => content.textContent?.includes('全部模型'))
assert.ok(vipModelContent)
const vipModelOptions = [
  ...vipModelContent.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
].map((item) => item.textContent?.trim())
assert.deepEqual(vipModelOptions, ['全部模型', 'gpt-5'])
assert.equal(vipModelOptions.includes(longDefaultModelName), false)
assert.ok(vipModelContent.classList.contains('w-max'))
await act(async () => preferenceModelTrigger.click())
const orderedAdjustmentRows = [
  ...preferenceRendered.dialog.querySelectorAll<HTMLElement>(
    '[data-adjustment-action]'
  ),
]
assert.ok(orderedAdjustmentRows[0]?.textContent?.includes('同优先级高权重渠道'))
assert.ok(orderedAdjustmentRows[1]?.textContent?.includes('同优先级低权重渠道'))
assert.ok(preferenceGroupTrigger)
await act(async () => preferenceGroupTrigger.click())
let preferenceGroupContent: HTMLElement | undefined
for (const content of document.body.querySelectorAll<HTMLElement>(
  '[data-slot="select-content"]'
)) {
  if (content.textContent?.includes('default')) {
    preferenceGroupContent = content
  }
}
assert.ok(preferenceGroupContent)
const preferenceGroupOptions = [
  ...preferenceGroupContent.querySelectorAll<HTMLElement>(
    '[data-slot="select-item"]'
  ),
]
assert.deepEqual(
  preferenceGroupOptions.map((item) => item.textContent?.trim()),
  ['vip', 'default']
)
assert.equal(
  preferenceGroupOptions.some(
    (item) => item.textContent?.trim() === '全部分组'
  ),
  false
)
const defaultGroupOption = preferenceGroupOptions.find(
  (item) => item.textContent?.trim() === 'default'
)
assert.ok(defaultGroupOption)
await act(async () => defaultGroupOption.click())
await waitForDialogText(preferenceRendered.dialog, '默认分组首个模型渠道')
assert.equal(
  domWindow.localStorage.getItem('channel-monitor:smart-schedule-display:v1'),
  JSON.stringify({ group: 'default', model: longDefaultModelName })
)
assert.equal(preferenceModelTrigger.title, longDefaultModelName)
assert.ok(preferenceModelTrigger.textContent?.includes(longDefaultModelName))
await act(async () => preferenceModelTrigger.click())
const defaultModelOption = [
  ...document.body.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
].find((item) => item.textContent?.trim() === longDefaultModelName)
assert.ok(defaultModelOption)
assert.ok(
  defaultModelOption.classList.contains(
    '[&_[data-slot=select-item-text]]:truncate'
  )
)
assert.equal(
  defaultModelOption.classList.contains(
    '[&_[data-slot=select-item-text]]:whitespace-normal'
  ),
  false
)
await act(async () => preferenceModelTrigger.click())
await act(async () => preferenceRendered.root.unmount())
preferenceRendered.container.remove()
preferenceQueryClient.clear()

const restoredPreferenceQueryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
restoredPreferenceQueryClient.setQueryData(
  ['channel-monitor-smart-schedule-executions', 1],
  {
    success: true,
    message: '',
    data: {
      page: 1,
      page_size: 20,
      total: 1,
      items: [preferenceNewestTask],
    },
  }
)
restoredPreferenceQueryClient.setQueryData(
  [
    'channel-monitor-smart-schedule-executions',
    'details',
    'preference-newest',
    1,
    '',
    'default',
    longDefaultModelName,
    'all',
  ],
  createDetailResponse([defaultGroupAdjustment])
)
const restoredPreferenceRendered = await renderDialog(
  <ChannelMonitorSmartScheduleExecutionDialog
    open
    onOpenChange={() => {}}
    groupOrder={['vip', 'default']}
    modelsByGroup={preferenceModelsByGroup}
  />,
  restoredPreferenceQueryClient
)
await waitForDialogText(
  restoredPreferenceRendered.dialog,
  '默认分组首个模型渠道'
)
assert.ok(
  restoredPreferenceRendered.dialog
    .querySelector<HTMLButtonElement>('[aria-label="按分组筛选"]')
    ?.textContent?.includes('default')
)
assert.ok(
  restoredPreferenceRendered.dialog
    .querySelector<HTMLButtonElement>('[aria-label="按模型筛选"]')
    ?.textContent?.includes(longDefaultModelName)
)
await act(async () => restoredPreferenceRendered.root.unmount())
restoredPreferenceRendered.container.remove()
restoredPreferenceQueryClient.clear()
api.defaults.adapter = originalAdapter
domWindow.close()
