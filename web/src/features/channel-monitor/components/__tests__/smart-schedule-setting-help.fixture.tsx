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
/* eslint-disable react/only-export-components */
import assert from 'node:assert/strict'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
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
  'PointerEvent',
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
const { useForm } = await import('react-hook-form')
const { Form, FormField, FormItem } = await import('@/components/ui/form')
const { ChannelMonitorSettingLabel } =
  await import('../channel-monitor-setting-label')

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function SettingHelpFixture() {
  const form = useForm<{ primarySwitchThresholdPercent: number }>({
    defaultValues: { primarySwitchThresholdPercent: 3 },
  })

  return (
    <Form {...form}>
      <FormField
        control={form.control}
        name='primarySwitchThresholdPercent'
        render={() => (
          <FormItem>
            <ChannelMonitorSettingLabel
              label='主渠道切换分差'
              helpKey='primarySwitchThreshold'
            />
          </FormItem>
        )}
      />
    </Form>
  )
}

function waitForTooltip(): Promise<HTMLElement> {
  const current = document.body.querySelector<HTMLElement>(
    '[data-slot="tooltip-content"]'
  )
  if (current) return Promise.resolve(current)

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      observer.disconnect()
      reject(new Error('说明 Tooltip 未在预期时间内显示'))
    }, 2000)
    const observer = new MutationObserver(() => {
      const tooltip = document.body.querySelector<HTMLElement>(
        '[data-slot="tooltip-content"]'
      )
      if (!tooltip) return
      clearTimeout(timeout)
      observer.disconnect()
      resolve(tooltip)
    })
    observer.observe(document.body, { childList: true, subtree: true })
  })
}

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

await act(async () => root.render(<SettingHelpFixture />))

const trigger = container.querySelector<HTMLButtonElement>(
  'button[aria-label="查看“主渠道切换分差”说明"]'
)
assert.ok(trigger)
const triggerType = trigger.type

await act(async () => trigger.focus())
const tooltip = await waitForTooltip()
const focusShowsExplanation =
  tooltip.textContent?.includes('新渠道的最终得分') === true &&
  tooltip.textContent.includes('立即切换')

await act(async () => root.unmount())
container.remove()

process.stdout.write(
  `${JSON.stringify({
    focusShowsExplanation,
    triggerAriaLabel: trigger.getAttribute('aria-label'),
    triggerType,
  })}\n`
)

domWindow.close()
