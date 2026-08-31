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

import { useForm, type UseFormReturn } from 'react-hook-form'
import { test } from 'vitest'

import { Form } from '@/components/ui/form'

import type { ChannelMonitorSmartSchedulePolicyFormValues } from '../../lib/schema'
import { CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE } from '../../lib/smart-schedule-group-policy'
import { ChannelMonitorSmartScheduleGroupPolicyFields } from '../channel-monitor-smart-schedule-group-policy-fields'
import { domWindow } from './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')

function getRenderedModels(container: HTMLElement): string[] {
  return [...container.querySelectorAll<HTMLElement>('[data-model]')].map(
    (element) => element.dataset.model ?? ''
  )
}

async function selectModel(input: HTMLInputElement, model: string) {
  await act(async () => {
    input.focus()
    input.dispatchEvent(
      new domWindow.KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      }) as unknown as KeyboardEvent
    )
  })
  const option = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="combobox-content"][data-open] [role="option"]'
    ),
  ].find((candidate) => candidate.textContent?.trim() === model)
  assert.ok(option)
  await act(async () => option.click())
}

test('uses the model selection sequence as the default card order', async () => {
  let currentForm:
    | UseFormReturn<ChannelMonitorSmartSchedulePolicyFormValues>
    | undefined
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  function Fixture() {
    const form = useForm<ChannelMonitorSmartSchedulePolicyFormValues>({
      defaultValues: CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
    })
    currentForm = form
    return (
      <Form {...form}>
        <ChannelMonitorSmartScheduleGroupPolicyFields
          form={form}
          modelOptions={['model-alpha', 'model-beta', 'model-gamma']}
        />
      </Form>
    )
  }

  await act(async () => root.render(<Fixture />))

  const modelInput = container.querySelector<HTMLInputElement>(
    'input[aria-label="全部模型"]'
  )
  assert.ok(modelInput)
  await selectModel(modelInput, 'model-beta')
  await selectModel(modelInput, 'model-alpha')

  assert.deepEqual(currentForm?.getValues('models'), [
    'model-beta',
    'model-alpha',
  ])
  assert.deepEqual(currentForm?.getValues('modelOrder'), [
    'model-beta',
    'model-alpha',
  ])
  assert.deepEqual(getRenderedModels(container), ['model-beta', 'model-alpha'])

  await act(async () => root.unmount())
  container.remove()
})
