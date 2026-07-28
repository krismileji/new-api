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
  smart_schedule_groups: ['default', 'vip'],
  smart_schedule_group_policies: [{ group: 'vip', strategy: 'ratio' }],
  smart_schedule_interval_minutes: 10,
  smart_schedule_strategy: 'smart',
  smart_schedule_stability_enabled: true,
  smart_schedule_apply_mode: 'weight',
  smart_schedule_performance_minutes: 60,
  smart_schedule_model: 'model-a',
  smart_schedule_models: ['model-a'],
  smart_schedule_min_samples: 5,
  smart_schedule_min_success_rate: 80,
  smart_schedule_cooldown_minutes: 30,
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
const scheduleUsesUnifiedTransition =
  sheet.classList.contains('transition-[opacity,translate]') &&
  sheet.classList.contains('duration-300') &&
  sheet.classList.contains('data-starting-style:translate-x-full') &&
  sheet.classList.contains('data-ending-style:translate-x-full') &&
  sheet.classList.contains('motion-reduce:transition-none')
const groupsFormItem = [
  ...sheet.querySelectorAll('[data-slot="form-item"]'),
].find(
  (item) =>
    item.querySelector('[data-slot="form-label"]')?.textContent === '参与分组'
)
assert.ok(groupsFormItem)
const groupsControl = groupsFormItem.querySelector(
  '[data-slot="combobox-chips"]'
)
assert.ok(groupsControl)
const scheduleControlsAligned =
  groupsFormItem.parentElement?.classList.contains('grid') === true &&
  groupsFormItem.classList.contains('min-w-0') &&
  groupsControl.classList.contains('h-8') &&
  groupsControl.classList.contains('min-h-8')
const scheduleGroupsCompact =
  groupsControl.textContent?.includes('已选择 2 个分组')
const groupPoliciesTab = [...sheet.querySelectorAll('[role="tab"]')].find(
  (tab) => tab.textContent?.includes('分组策略')
) as HTMLElement | undefined
assert.ok(groupPoliciesTab)
await act(async () => groupPoliciesTab.click())

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
const savePolicyButton = [...policyDialog.querySelectorAll('button')].find(
  (button) => button.textContent?.includes('保存分组策略')
)
assert.ok(savePolicyButton)
await act(async () => savePolicyButton.click())
const policyRows = [...sheet.querySelectorAll('tbody tr')]
const matchingDefaultPolicyVisible =
  policyRows.length === 2 &&
  policyRows.some((row) => row.textContent?.includes('default'))

process.stdout.write(
  `${JSON.stringify({
    generalHasSchedule,
    generalTitle,
    policyDialogBlocksHorizontalOverflow,
    policyDialogCentered,
    policyTableScrollable,
    matchingDefaultPolicyVisible,
    scheduleControlsAligned,
    scheduleGroupsCompact,
    scheduleSide,
    scheduleTitle,
    scheduleUsesUnifiedTransition,
  })}\n`
)

await unmountSettingsSurface(scheduleSurface)
domWindow.close()
