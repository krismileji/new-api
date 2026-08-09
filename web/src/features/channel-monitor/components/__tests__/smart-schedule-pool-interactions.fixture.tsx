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

import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
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
const {
  channelMonitorSmartScheduleRouteKey,
  placeChannelMonitorSmartScheduleRoutes,
  summarizeChannelMonitorSmartSchedulePools,
} = await import('../../lib/smart-schedule-summary')
const { ChannelMonitorSmartSchedulePool } =
  await import('../channel-monitor-smart-schedule-pool')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createRoute(
  channelId: number,
  name: string,
  priority: number,
  overrides: Partial<ChannelMonitorSmartScheduleRoute> = {}
): ChannelMonitorSmartScheduleRoute {
  return {
    channel_id: channelId,
    channel_name: name,
    channel_status: 1,
    channel_priority: priority,
    channel_weight: 100,
    group: 'production',
    model: 'cache-model',
    enabled: true,
    priority,
    weight: 100,
    shared_samples: {
      id: channelId,
      channel_id: channelId,
      model: 'cache-model',
      window_start: 1_752_700_000,
      observation_since: 0,
      last_time: 1_752_777_845,
      last_success: true,
      last_error: '',
      sample_count: 12,
      success_count: 11,
      failure_duration_sample_count: 1,
      average_failure_duration_ms: 800,
      first_token_sample_count: 12,
      average_first_token_ms: 430,
      tps_sample_count: 12,
      average_tps: 42.5,
    },
    ...overrides,
    state: {
      id: channelId,
      channel_id: channelId,
      group: 'production',
      model: 'cache-model',
      participation_set: true,
      excluded: false,
      last_schedule_status: 'succeeded',
      last_schedule_error: '',
      last_schedule_score: 0.95,
      last_schedule_priority: priority,
      last_schedule_weight: 100,
      last_schedule_time: 1_752_777_845,
      stability_state: '',
      stability_until: 0,
      stability_since: 0,
      stability_saved_priority: 0,
      stability_saved_weight: 0,
      runtime_protection_until: 0,
      base_rank: channelId,
      base_priority: priority,
      base_weight: 100,
      temporary_traffic_kind: '',
      temporary_traffic_since: 0,
      temporary_traffic_target_percent: 0,
      last_priority_sample_time: 0,
      manual_primary_until: 0,
      manual_primary_allow_stability_degrade: true,
      ...overrides.state,
    },
  }
}

function createChannel(
  id: number,
  name: string,
  costRatio: number,
  remark: string
): ChannelMonitorItem {
  return {
    id,
    name,
    type: 1,
    status: 1,
    status_reason: '',
    priority: 100,
    weight: 100,
    base_url: 'https://example.com',
    models: 'cache-model',
    test_model: 'cache-model',
    groups: ['production'],
    ratio: costRatio,
    previous_ratio: costRatio,
    cost_ratio: costRatio,
    previous_cost_ratio: costRatio,
    conversion_factor: 1,
    remark,
    channel_remark: remark,
    updated_time: 1_752_777_845,
    updated_by: 1,
    updated_by_username: '系统自动更新',
    last_fetch_status: 'succeeded',
    last_fetch_error: '',
    last_fetch_time: 1_752_777_845,
    consecutive_failures: 0,
    upstream_balance: 100,
    last_balance_time: 1_752_777_845,
    last_balance_error: '',
    today_cost_cny: 0,
    today_cost_configured: false,
    today_cost_complete: true,
    today_cost_unresolved_count: 0,
    concurrency_limit: 0,
    concurrency_active: 0,
    upstream: null,
  }
}

const routes = [
  createRoute(1, '上海主渠道', 100, {
    state: {
      manual_primary_until: 1_900_000_000,
      manual_primary_allow_stability_degrade: true,
    } as ChannelMonitorSmartScheduleRoute['state'],
  }),
  createRoute(2, '北京备用渠道', 90),
  createRoute(3, '广州暂停渠道', 80, {
    traffic_paused_until: 4_102_444_800,
    state: { excluded: true } as ChannelMonitorSmartScheduleRoute['state'],
  }),
]
const channels = new Map([
  [1, createChannel(1, '上海主渠道', 0.7, '华东低延迟')],
  [2, createChannel(2, '北京备用渠道', 0.9, '华北备用')],
  [3, createChannel(3, '广州暂停渠道', 1.1, '维护中')],
])
const placements = placeChannelMonitorSmartScheduleRoutes(routes)
const primaryRouteKey = channelMonitorSmartScheduleRouteKey(routes[0])
const performanceByRoute = new Map<
  string,
  ChannelMonitorSmartScheduleRoutePerformance
>([
  [
    primaryRouteKey,
    {
      channel_id: 1,
      group: 'production',
      model: 'cache-model',
      group_count: 3,
      sample_count: 1248,
      first_token_sample_count: 1200,
      first_token_duration_sample_count: 1180,
      tps_sample_count: 1100,
      average_first_token_ms: 420,
      first_token_p50_ms: 390,
      first_token_p95_ms: 870,
      winsorized_average_first_token_ms: 415,
      average_tps: 31.75,
      last_used_time: 1_752_777_845,
    },
  ],
])
const stabilityByRoute = new Map<
  string,
  ChannelMonitorSmartScheduleRouteStability
>([
  [
    primaryRouteKey,
    {
      channel_id: 1,
      group: 'production',
      model: 'cache-model',
      group_count: 3,
      success_count: 1244,
      failure_count: 16,
      final_failure_count: 4,
      retry_failure_count: 12,
      sample_count: 1260,
      success_rate: 1244 / 1260,
      stability_score: 0.975,
      average_retry_failure_duration_ms: 820,
      retry_failure_duration_buckets: [],
      jitter_available: true,
      first_token_p50_ms: 390,
      first_token_p95_ms: 870,
      jitter_threshold_ms: 11_800,
      jitter_sample_count: 1180,
      jitter_slow_count: 2,
      jitter_allowed_count: 59,
      jitter_penalty: 0,
    },
  ],
])
const poolSummary = summarizeChannelMonitorSmartSchedulePools(routes)[0]
assert.ok(poolSummary)

async function renderPool(options?: {
  onSetPrimary?: (route: ChannelMonitorSmartScheduleRoute) => void
  onClearPrimary?: (route: ChannelMonitorSmartScheduleRoute) => void
  onSaveManualRouting?: (
    route: ChannelMonitorSmartScheduleRoute,
    priority: number,
    weight: number
  ) => void
  onGroupPauseChange?: (
    route: ChannelMonitorSmartScheduleRoute,
    durationMinutes: number
  ) => void
}) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <ChannelMonitorSmartSchedulePool
        pool={{ summary: poolSummary, routes }}
        policy={undefined}
        channelsById={channels}
        placements={placements}
        performanceByRoute={performanceByRoute}
        stabilityByRoute={stabilityByRoute}
        updateRouteKey={null}
        manualRoutingKey={null}
        groupPauseKey={null}
        updateDisabled={false}
        onParticipationChange={() => {}}
        onClearProtection={() => {}}
        onSetPrimary={options?.onSetPrimary ?? (() => {})}
        onClearPrimary={options?.onClearPrimary ?? (() => {})}
        onSaveManualRouting={options?.onSaveManualRouting ?? (() => {})}
        onGroupPauseChange={options?.onGroupPauseChange ?? (() => {})}
      />
    )
  })
  return { container, root }
}

const filtered = await renderPool()
const search = filtered.container.querySelector<HTMLInputElement>(
  '[aria-label="搜索调度池渠道"]'
)
assert.ok(search)
const setInputValue = Object.getOwnPropertyDescriptor(
  domWindow.HTMLInputElement.prototype,
  'value'
)?.set
assert.ok(setInputValue)
await act(async () => {
  setInputValue.call(search, '华北备用')
  search.dispatchEvent(new Event('input', { bubbles: true }))
})
assert.equal(
  filtered.container.querySelectorAll('[data-schedule-route-row]').length,
  1
)
assert.ok(filtered.container.textContent?.includes('北京备用渠道'))
assert.ok(filtered.container.textContent?.includes('显示 1 / 3 条'))
const clearSearch = filtered.container.querySelector<HTMLButtonElement>(
  '[aria-label="清空渠道搜索"]'
)
assert.ok(clearSearch)
await act(async () => clearSearch.click())
const sortFilter = filtered.container.querySelector<HTMLButtonElement>(
  '[aria-label="按渠道排序"]'
)
assert.ok(sortFilter?.textContent?.includes('成本倍率从低到高'))
const statusFilter = filtered.container.querySelector<HTMLButtonElement>(
  '[aria-label="按状态筛选"]'
)
assert.ok(statusFilter)
await act(async () => {
  statusFilter.focus()
  statusFilter.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'ArrowDown',
      code: 'ArrowDown',
      bubbles: true,
    })
  )
})
const backupOption = [
  ...document.querySelectorAll<HTMLElement>('[role="option"]'),
].find((option) => option.textContent?.trim() === '备用渠道')
assert.ok(backupOption)
await act(async () => backupOption.click())
const backupRows = filtered.container.querySelectorAll(
  '[data-schedule-route-row]'
)
assert.equal(backupRows.length, 1)
assert.ok(backupRows[0]?.textContent?.includes('北京备用渠道'))
assert.equal(backupRows[0]?.textContent?.includes('上海主渠道'), false)
await act(async () => filtered.root.unmount())
filtered.container.remove()

const expanded = await renderPool()
assert.ok(expanded.container.textContent?.includes('评分第一'))
assert.ok(expanded.container.textContent?.includes('实际主渠道'))
assert.ok(expanded.container.textContent?.includes('实际最高层'))
assert.ok(expanded.container.textContent?.includes('基础排名'))
assert.ok(expanded.container.textContent?.includes('基础 P / W'))
assert.ok(expanded.container.textContent?.includes('当前 P / W'))
assert.ok(expanded.container.textContent?.includes('临时流量'))
const detailButtons = expanded.container.querySelectorAll<HTMLButtonElement>(
  '[aria-label="查看 上海主渠道 的调度详情"]'
)
assert.equal(detailButtons.length, 2)
assert.equal(detailButtons[0]?.getAttribute('aria-expanded'), 'false')
await act(async () => detailButtons[0]?.click())
const expandedButtons = expanded.container.querySelectorAll<HTMLButtonElement>(
  '[aria-label="查看 上海主渠道 的调度详情"][aria-expanded="true"]'
)
assert.equal(expandedButtons.length, 2)
assert.equal(
  expandedButtons[0]?.getAttribute('aria-controls'),
  'channel-monitor-route-details-1'
)
const details = document.querySelector<HTMLElement>(
  '[aria-label="上海主渠道 调度详情"]'
)
assert.ok(details)
assert.ok(details.textContent?.includes('基础排名'))
assert.ok(details.textContent?.includes('基础 P / W'))
assert.ok(details.textContent?.includes('当前 P / W'))
assert.ok(details.textContent?.includes('临时流量类型与目标'))
assert.ok(details.querySelector('[aria-label="调度池决策结果"]'))
const sharedWindow = details.querySelector<HTMLElement>(
  '[aria-label="渠道与模型共享窗口数据"]'
)
assert.ok(sharedWindow)
assert.ok(sharedWindow.textContent?.includes('覆盖 3 个业务分组'))
assert.ok(sharedWindow.textContent?.includes('稳定性评分窗口'))
assert.ok(sharedWindow.textContent?.includes('1260 次'))
assert.ok(sharedWindow.textContent?.includes('性能窗口'))
assert.ok(sharedWindow.textContent?.includes('1248 次'))
assert.ok(sharedWindow.textContent?.includes('测试 / 探测'))
assert.ok(sharedWindow.textContent?.includes('12 次'))
await act(async () => expanded.root.unmount())
expanded.container.remove()

const primaryActions: Array<{
  action: 'set' | 'clear'
  route: ChannelMonitorSmartScheduleRoute
}> = []
const primaryToggle = await renderPool({
  onSetPrimary: (route) => primaryActions.push({ action: 'set', route }),
  onClearPrimary: (route) => primaryActions.push({ action: 'clear', route }),
})
const setPrimaryButtons =
  primaryToggle.container.querySelectorAll<HTMLButtonElement>(
    '[aria-label="固定 北京备用渠道 为主渠道"]'
  )
assert.equal(setPrimaryButtons.length, 2)
await act(async () => setPrimaryButtons[0]?.click())
const clearPrimaryButtons =
  primaryToggle.container.querySelectorAll<HTMLButtonElement>(
    '[aria-label="取消固定 上海主渠道"]'
  )
assert.equal(clearPrimaryButtons.length, 2)
await act(async () => clearPrimaryButtons[0]?.click())
assert.deepEqual(
  primaryActions.map(({ action, route }) => [action, route.channel_id]),
  [
    ['set', 2],
    ['clear', 1],
  ]
)
await act(async () => primaryToggle.root.unmount())
primaryToggle.container.remove()

const groupPauseActions: Array<{
  route: ChannelMonitorSmartScheduleRoute
  durationMinutes: number
}> = []
const groupPause = await renderPool({
  onGroupPauseChange: (route, durationMinutes) =>
    groupPauseActions.push({ route, durationMinutes }),
})
const pauseButtons = groupPause.container.querySelectorAll<HTMLButtonElement>(
  '[aria-label="暂停 上海主渠道 在 production 分组使用 cache-model 模型的流量"]'
)
assert.equal(pauseButtons.length, 2)
await act(async () => pauseButtons[0]?.click())
const pauseForm = document.querySelector<HTMLFormElement>(
  '[aria-label="上海主渠道 production cache-model 路由流量暂停设置"]'
)
assert.ok(pauseForm)
const pauseDuration = pauseForm.querySelector<HTMLInputElement>(
  '#route-pause-duration-1'
)
const pauseSubmit = pauseForm.querySelector<HTMLButtonElement>(
  'button[type="submit"]'
)
assert.ok(pauseDuration)
assert.ok(pauseSubmit)
pauseDuration.value = '120'
await act(async () => pauseSubmit.click())
assert.deepEqual(
  groupPauseActions.map(({ route, durationMinutes }) => [
    route.channel_id,
    durationMinutes,
  ]),
  [[1, 120]]
)
await act(async () => groupPause.root.unmount())
groupPause.container.remove()

const groupResume = await renderPool({
  onGroupPauseChange: (route, durationMinutes) =>
    groupPauseActions.push({ route, durationMinutes }),
})
const resumeDetailButtons =
  groupResume.container.querySelectorAll<HTMLButtonElement>(
    '[aria-label="恢复 广州暂停渠道 在 production 分组使用 cache-model 模型的流量"]'
  )
assert.equal(resumeDetailButtons.length, 2)
await act(async () => resumeDetailButtons[0]?.click())
const resumeDetails = document.querySelector<HTMLElement>(
  '[aria-label="广州暂停渠道 调度详情"]'
)
assert.ok(resumeDetails)
assert.ok(resumeDetails.textContent?.includes('流量已暂停'))
const resumeButton = [
  ...resumeDetails.querySelectorAll<HTMLButtonElement>('button'),
].find((button) => button.textContent?.includes('立即恢复'))
assert.ok(resumeButton)
await act(async () => resumeButton.click())
assert.deepEqual(groupPauseActions.at(-1), {
  route: routes[2],
  durationMinutes: 0,
})
await act(async () => groupResume.root.unmount())
groupResume.container.remove()

let submitted:
  | {
      route: ChannelMonitorSmartScheduleRoute
      priority: number
      weight: number
    }
  | undefined
const manual = await renderPool({
  onSaveManualRouting: (route, priority, weight) => {
    submitted = { route, priority, weight }
  },
})
const manualButtons = manual.container.querySelectorAll<HTMLButtonElement>(
  '[aria-label="查看 广州暂停渠道 的调度详情"]'
)
assert.equal(manualButtons.length, 2)
await act(async () => manualButtons[0]?.click())
const manualDetails = document.querySelector<HTMLElement>(
  '[aria-label="广州暂停渠道 调度详情"]'
)
assert.ok(manualDetails)
assert.ok(manualDetails.textContent?.includes('人工路由设置'))
const priorityInput =
  manualDetails.querySelector<HTMLInputElement>('#manual-priority-3')
const weightInput =
  manualDetails.querySelector<HTMLInputElement>('#manual-weight-3')
const manualForm = manualDetails.querySelector<HTMLFormElement>(
  '[aria-label="广州暂停渠道 production cache-model 人工路由设置"]'
)
const submit = manualForm?.querySelector<HTMLButtonElement>(
  'button[type="submit"]'
)
assert.ok(priorityInput)
assert.ok(weightInput)
assert.ok(submit)
priorityInput.value = '30'
weightInput.value = '400'
await act(async () => submit.click())
assert.equal(submitted?.route.channel_id, 3)
assert.equal(submitted?.priority, 30)
assert.equal(submitted?.weight, 400)
await act(async () => manual.root.unmount())
manual.container.remove()
domWindow.close()
