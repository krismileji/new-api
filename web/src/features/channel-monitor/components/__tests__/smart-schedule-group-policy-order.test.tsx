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
import { describe, test } from 'vitest'

import { Form } from '@/components/ui/form'

import type { ChannelMonitorSettingsFormValues } from '../../lib/schema'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
  createChannelMonitorSmartScheduleGroupPolicy,
} from '../../lib/smart-schedule-group-policy'
import { ChannelMonitorSmartScheduleGroupPolicies } from '../channel-monitor-smart-schedule-group-policies'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')

function getRenderedGroups(container: HTMLElement): string[] {
  return [
    ...container.querySelectorAll<HTMLElement>(
      '[data-slot="group-policy-summary"]'
    ),
  ].map((element) => element.dataset.group ?? '')
}

describe('smart schedule group policy order', () => {
  test('keeps the saved order and updates it with accessible move controls', async () => {
    const initialPolicies = ['vip', 'default', 'batch'].map((group) =>
      createChannelMonitorSmartScheduleGroupPolicy(
        group,
        CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE
      )
    )
    let currentForm: UseFormReturn<ChannelMonitorSettingsFormValues> | undefined
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    function Fixture() {
      const form = useForm<ChannelMonitorSettingsFormValues>({
        defaultValues: {
          smartScheduleGroupPolicies: initialPolicies,
        } as ChannelMonitorSettingsFormValues,
      })
      currentForm = form
      return (
        <Form {...form}>
          <ChannelMonitorSmartScheduleGroupPolicies
            form={form}
            groupOptions={['batch', 'default', 'vip']}
            modelOptionsByGroup={new Map()}
          />
        </Form>
      )
    }

    await act(async () => root.render(<Fixture />))

    assert.deepEqual(getRenderedGroups(container), ['vip', 'default', 'batch'])
    const firstUpButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="上移分组策略 vip"]'
    )
    const lastDownButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="下移分组策略 batch"]'
    )
    assert.ok(firstUpButton?.disabled)
    assert.ok(lastDownButton?.disabled)

    const defaultUpButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="上移分组策略 default"]'
    )
    assert.ok(defaultUpButton)
    await act(async () => defaultUpButton.click())

    assert.deepEqual(getRenderedGroups(container), ['default', 'vip', 'batch'])
    assert.deepEqual(
      currentForm
        ?.getValues('smartScheduleGroupPolicies')
        .map((policy) => policy.group),
      ['default', 'vip', 'batch']
    )

    await act(async () => root.unmount())
    container.remove()
  })
})
