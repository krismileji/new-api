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
  auto_update_consecutive_failure_limit: 3,
  auto_disable_on_update_failure: false,
  auto_enable_on_cost_ratio_recovery: false,
  auto_enable_on_balance_recovery: false,
  cost_retention_days: 120,
  email_notification_enabled: false,
  notification_email: '',
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
      jitter_threshold_multiplier: 3,
      jitter_absolute_tolerance_ms: 1000,
      jitter_baseline_hours: 24,
      scoring: {
        stability_percent: 50,
        curve_exponent: 1,
        relative_weight_enabled: true,
        relative_weight_start_percent: 3,
        relative_weight_full_percent: 10,
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
    },
  ],
  smart_schedule_interval_minutes: 10,
  smart_schedule_performance_minutes: 60,
} satisfies ChannelMonitorSettings

async function renderSettingsSurface(surface: 'general' | 'schedule') {
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
            onOpenChange={() => {}}
          />
        ) : (
          <ChannelMonitorSmartScheduleSettingsSheet
            settings={settings}
            modelOptions={['model-a']}
            groupOptions={['default', 'vip']}
            open
            onOpenChange={() => {}}
          />
        )}
      </QueryClientProvider>
    )
  })

  return { container, root }
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
  '判定倍率',
  '绝对容差',
  '基线学习周期',
  '智能调度指标占比',
  '得分曲线指数',
  '相对权重拉伸',
].every((label) => policyDialogText.includes(label))
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
  '保存策略后，该分组才会进入智能调度'
)
const savePolicyButton = [...policyDialog.querySelectorAll('button')].find(
  (button) => button.textContent?.includes('保存分组策略')
)
assert.ok(savePolicyButton)
await act(async () => savePolicyButton.click())
const policyRows = [...sheet.querySelectorAll('tbody tr')]
const newPolicyVisible =
  policyRows.length === 2 &&
  policyRows.some((row) => row.textContent?.includes('default'))

process.stdout.write(
  `${JSON.stringify({
    generalHasSchedule,
    generalTitle,
    policyDialogBlocksHorizontalOverflow,
    policyDialogCentered,
    policyTableScrollable,
    newPolicyVisible,
    policyDialogExplainsExplicitScope,
    policyDialogHasGroupSettingHelp,
    policyDialogHasCompletePolicyControls,
    policyDialogStabilityInputsAligned,
    scheduleHasExplicitPolicyScope,
    scheduleHasNoImplicitPolicyControls,
    scheduleSide,
    scheduleTitle,
    scheduleUsesChannelDrawerLayout,
    scheduleUsesUnifiedTransition,
  })}\n`
)

await unmountSettingsSurface(scheduleSurface)
domWindow.close()
