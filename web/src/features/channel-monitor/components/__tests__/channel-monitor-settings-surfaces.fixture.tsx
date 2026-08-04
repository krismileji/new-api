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

import { DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES } from '../../lib/email-notification'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
} from '../../lib/query-options'
import type { ChannelMonitorSettings } from '../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'MutationObserver',
  'ResizeObserver',
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

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const {
  ChannelMonitorSettingsDialog,
  ChannelMonitorSmartScheduleSettingsSheet,
} = await import('../channel-monitor-settings-dialog')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const settings = {
  auto_update_interval_minutes: 10,
  auto_update_retry_count: 2,
  upstream_request_timeout_seconds: 45,
  auto_update_consecutive_failure_limit: 3,
  auto_disable_on_update_failure: false,
  auto_enable_on_cost_ratio_recovery: false,
  auto_enable_on_balance_recovery: false,
  cost_retention_days: 120,
  email_notification_enabled: true,
  notification_email: 'alerts@example.com',
  email_notification_types: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
  probe_response_enabled: false,
  relay_response_header_timeout_seconds: 60,
  smart_schedule_enabled: true,
  smart_schedule_group_policies: [
    {
      group: 'vip',
      strategy: 'ratio',
      stability_enabled: true,
      jitter_enabled: true,
      jitter_tolerance_percent: 5,
      jitter_absolute_tolerance_seconds: 10,
      jitter_baseline_minutes: 60,
      scoring: {
        stability_percent: 50,
        primary_traffic_percent: 90,
        primary_switch_threshold_percent: 3,
        smart: {
          cost_ratio_percent: 40,
          first_token_percent: 40,
          tps_percent: 20,
        },
        ratio: {
          cost_ratio_percent: 70,
          first_token_percent: 20,
          tps_percent: 10,
        },
      },
      apply_mode: 'priority_weight',
      models: [],
      min_samples: 5,
      degrade_stability_score: 90,
      recovery_stability_score: 95,
      fast_failure_penalty_percent: 40,
      fast_failure_seconds: 1,
      slow_failure_seconds: 10,
      cooldown_minutes: 30,
      sample_mode: 'traffic',
      exploration_traffic_percent: 3,
      probe_interval_minutes: 10,
      priority_sampling_enabled: true,
      priority_sampling_interval_minutes: 10,
      priority_sampling_base_percent: 3,
      priority_sampling_decay_percent: 70,
      priority_sampling_min_percent: 0.5,
    },
  ],
  smart_schedule_interval_minutes: 10,
  smart_schedule_performance_window_minutes: 60,
  smart_schedule_stability_window_minutes: 120,
  smart_schedule_rate_limit_cooldown_seconds: 30,
  smart_schedule_control_revision: 'revision-a',
} satisfies ChannelMonitorSettings

async function renderSettingsSurface(
  surface: 'general' | 'schedule',
  onOpenChange: (open: boolean) => void = () => {}
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const queryClient = new QueryClient()

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        {surface === 'general' ? (
          <ChannelMonitorSettingsDialog
            settings={settings}
            open
            onOpenChange={onOpenChange}
          />
        ) : (
          <ChannelMonitorSmartScheduleSettingsSheet
            settings={settings}
            modelOptionsByGroup={new Map([['default', ['model-a']]])}
            groupOptions={['default', 'vip']}
            open
            onOpenChange={onOpenChange}
          />
        )}
      </QueryClientProvider>
    )
  })

  return { container, queryClient, root }
}

async function unmountSettingsSurface(
  rendered: Awaited<ReturnType<typeof renderSettingsSurface>>
) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

const generalSurface = await renderSettingsSurface('general')
const generalDialog = document.body.querySelector(
  '[data-slot="dialog-content"]'
)
assert.ok(generalDialog)
const generalTitle = generalDialog.textContent ?? ''
const generalHasSchedule = generalTitle.includes('智能调度')
const notificationTypeFields = [
  ...generalDialog.querySelectorAll<HTMLElement>('[data-notification-type]'),
]
const notificationTypeCheckboxes = notificationTypeFields.map((field) =>
  field.querySelector<HTMLButtonElement>('[role="checkbox"]')
)
assert.equal(notificationTypeCheckboxes.length, 6)
assert.ok(notificationTypeCheckboxes.every(Boolean))
const allNotificationTypesSelected = notificationTypeCheckboxes.every(
  (checkbox) => checkbox?.getAttribute('aria-checked') === 'true'
)
const previewEmailButton = [...generalDialog.querySelectorAll('button')].find(
  (button) => button.textContent?.includes('预览邮件')
)
assert.ok(previewEmailButton)
const previewEmailButtonEnabled = !previewEmailButton.disabled
const firstNotificationCheckbox = notificationTypeCheckboxes[0]
assert.ok(firstNotificationCheckbox)
await act(async () => firstNotificationCheckbox.click())
const notificationTypeCanBeUnchecked =
  firstNotificationCheckbox.getAttribute('aria-checked') === 'false'
await unmountSettingsSurface(generalSurface)

const scheduleSurface = await renderSettingsSurface('schedule')
const sheet = document.body.querySelector('[data-slot="sheet-content"]')
assert.ok(sheet)
const scheduleSide = sheet.getAttribute('data-side')
const scheduleTitle = sheet.textContent ?? ''
const scheduleHeader = sheet.querySelector('[data-slot="sheet-header"]')
const scheduleForm = sheet.querySelector(
  '#channel-monitor-smart-schedule-settings-form'
)
const scheduleFooter = sheet.querySelector('[data-slot="sheet-footer"]')
const scheduleUsesChannelDrawerLayout =
  sheet.classList.contains('h-dvh') &&
  sheet.classList.contains('w-full') &&
  sheet.classList.contains('overflow-hidden') &&
  sheet.classList.contains('p-0') &&
  sheet.classList.contains('sm:max-w-5xl') &&
  scheduleHeader?.classList.contains('border-b') === true &&
  scheduleHeader.classList.contains('sm:px-6') &&
  scheduleForm?.classList.contains('overflow-y-auto') === true &&
  scheduleForm.classList.contains('sm:px-6') &&
  scheduleFooter?.classList.contains('border-t') === true &&
  scheduleFooter.classList.contains('sm:px-6')
const scheduleUsesUnifiedTransition =
  sheet.classList.contains('transition-[opacity,translate]') &&
  sheet.classList.contains('duration-300') &&
  sheet.classList.contains('data-starting-style:translate-x-full') &&
  sheet.classList.contains('data-ending-style:translate-x-full') &&
  sheet.classList.contains('motion-reduce:transition-none')
const scheduleHasExplicitPolicyScope =
  scheduleTitle.includes('只有已配置策略的分组参与智能调度') &&
  scheduleTitle.includes('未配置分组不会参与智能调度')
const scheduleHasNoImplicitPolicyControls =
  !scheduleTitle.includes('参与分组') && !scheduleTitle.includes('默认策略')

const policyTable = sheet.querySelector('table')
assert.ok(policyTable)
const policyTableScrollable =
  policyTable.parentElement?.className.includes('overflow-x-auto')
const addPolicyButton = [...sheet.querySelectorAll('button')].find((button) =>
  button.textContent?.includes('新增分组策略')
)
assert.ok(addPolicyButton)
await act(async () => addPolicyButton.click())

const policyDialog = document.body.querySelector('[data-slot="dialog-content"]')
assert.ok(policyDialog)
const policyDialogCentered =
  policyDialog.className.includes('top-1/2') &&
  policyDialog.className.includes('left-1/2')
const policyDialogScrollArea = policyDialog.querySelector(
  '[data-slot="group-policy-dialog-scroll-area"]'
)
const policyDialogBlocksHorizontalOverflow =
  policyDialogScrollArea?.classList.contains('overflow-x-hidden') === true
const policyDialogText = policyDialog.textContent ?? ''
const policyDialogHasGroupSettingHelp =
  policyDialog.querySelector('button[aria-label="查看“分组”说明"]') !== null
const policyDialogHasCompletePolicyControls = [
  '调度方式',
  '调整方式',
  '参与模型',
  '样本补充方式',
  '探索流量',
  '定时探测',
  '稳定性保护',
  '稳定性占比',
  '降级稳定性得分',
  '恢复稳定性得分',
  '最少样本',
  '快速失败惩罚',
  '快速失败界限',
  '慢失败界限',
  '降级时长',
  '成功延迟抖动',
  '允许抖动',
  '绝对容差',
  '基线学习周期',
  '智能调度指标占比',
  '主渠道切换分差',
].every((label) => policyDialogText.includes(label))
const policyDialogHasNoLegacyWeightControls =
  !policyDialogText.includes('得分曲线指数') &&
  !policyDialogText.includes('相对权重拉伸')
const fastFailureInput = policyDialog.querySelector(
  'input[name="fastFailureSeconds"]'
)
const stabilityFailureGrid = fastFailureInput?.closest(
  '[class*="lg:grid-cols-4"]'
)
const policyDialogStabilityInputsAligned =
  stabilityFailureGrid?.classList.contains('items-start') === true &&
  [
    'fastFailurePenaltyPercent',
    'fastFailureSeconds',
    'slowFailureSeconds',
    'cooldownMinutes',
  ].every((name) => stabilityFailureGrid.querySelector(`input[name="${name}"]`))
const policyDialogExplainsExplicitScope = policyDialogText.includes(
  '应用到当前设置并保存智能调度设置后，该分组才会进入调度范围'
)
const savePolicyButton = [...policyDialog.querySelectorAll('button')].find(
  (button) => button.textContent?.includes('应用到当前设置')
)
assert.ok(savePolicyButton)
await act(async () => savePolicyButton.click())
const policyRows = [...sheet.querySelectorAll('tbody tr')]
const newPolicyVisible =
  policyRows.length === 2 &&
  policyRows.some((row) => row.textContent?.includes('default'))
await unmountSettingsSurface(scheduleSurface)

let conflictFormClosed = false
let resolveConflictClose: (() => void) | undefined
const conflictClose = new Promise<void>((resolve) => {
  resolveConflictClose = resolve
})
const conflictSurface = await renderSettingsSurface('schedule', (open) => {
  if (open) return
  conflictFormClosed = true
  resolveConflictClose?.()
})
conflictSurface.queryClient.setQueryData(['channel-monitor'], {})
conflictSurface.queryClient.setQueryData(
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  {}
)
conflictSurface.queryClient.setQueryData(
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
  {}
)

const originalAdapter = api.defaults.adapter
const conflictAdapter: AxiosAdapter = async (config) => {
  throw Object.assign(new Error('智能调度配置已被其他管理员更新'), {
    config,
    isAxiosError: true,
    response: {
      config,
      data: { message: '智能调度配置已被其他管理员更新' },
      headers: {},
      status: 409,
      statusText: 'Conflict',
    },
  })
}
api.defaults.adapter = conflictAdapter

try {
  const conflictSheet = document.body.querySelector(
    '[data-slot="sheet-content"]'
  )
  assert.ok(conflictSheet)
  const saveSettingsButton = [...conflictSheet.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === '保存'
  )
  assert.ok(saveSettingsButton)
  await act(async () => {
    saveSettingsButton.click()
    await conflictClose
  })
} finally {
  api.defaults.adapter = originalAdapter
}

const conflictMonitorQueryInvalidated =
  conflictSurface.queryClient.getQueryState(['channel-monitor'])
    ?.isInvalidated === true
const conflictExecutionsQueryInvalidated =
  conflictSurface.queryClient.getQueryState(
    CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY
  )?.isInvalidated === true
const conflictHistoryQueryInvalidated =
  conflictSurface.queryClient.getQueryState(
    CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY
  )?.isInvalidated === true

process.stdout.write(
  `${JSON.stringify({
    allNotificationTypesSelected,
    conflictExecutionsQueryInvalidated,
    conflictFormClosed,
    conflictHistoryQueryInvalidated,
    conflictMonitorQueryInvalidated,
    generalHasSchedule,
    generalTitle,
    notificationTypeCanBeUnchecked,
    policyDialogBlocksHorizontalOverflow,
    policyDialogCentered,
    policyTableScrollable,
    previewEmailButtonEnabled,
    newPolicyVisible,
    policyDialogExplainsExplicitScope,
    policyDialogHasGroupSettingHelp,
    policyDialogHasCompletePolicyControls,
    policyDialogHasNoLegacyWeightControls,
    policyDialogStabilityInputsAligned,
    scheduleHasExplicitPolicyScope,
    scheduleHasNoImplicitPolicyControls,
    scheduleSide,
    scheduleTitle,
    scheduleUsesChannelDrawerLayout,
    scheduleUsesUnifiedTransition,
  })}\n`
)

await unmountSettingsSurface(conflictSurface)
domWindow.close()
