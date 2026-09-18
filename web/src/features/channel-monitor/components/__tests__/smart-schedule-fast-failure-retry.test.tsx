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
import { zodResolver } from '@hookform/resolvers/zod'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm, type Resolver } from 'react-hook-form'
import { describe, expect, test, vi } from 'vitest'
import type { ZodType } from 'zod'

import { Form } from '@/components/ui/form'

import {
  createChannelMonitorSmartSchedulePolicySchema,
  type ChannelMonitorSmartSchedulePolicyFormValues,
} from '../../lib/schema'
import { CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE } from '../../lib/smart-schedule-group-policy'
import { ChannelMonitorSmartScheduleGroupPolicyFields } from '../channel-monitor-smart-schedule-group-policy-fields'

function PolicyForm(props: {
  stabilityEnabled: boolean
  onSubmit: (values: ChannelMonitorSmartSchedulePolicyFormValues) => void
}) {
  const form = useForm<ChannelMonitorSmartSchedulePolicyFormValues>({
    resolver: zodResolver(
      createChannelMonitorSmartSchedulePolicySchema() as unknown as ZodType<
        ChannelMonitorSmartSchedulePolicyFormValues,
        ChannelMonitorSmartSchedulePolicyFormValues
      >
    ) as unknown as Resolver<ChannelMonitorSmartSchedulePolicyFormValues>,
    defaultValues: {
      ...CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE,
      stabilityEnabled: props.stabilityEnabled,
      fastFailureSeconds: 2.5,
      fastFailureSameChannelRetryCount: 2,
      fastFailureSameChannelRetryDelayMs: 750,
    },
  })

  return (
    <Form {...form}>
      <form noValidate onSubmit={form.handleSubmit(props.onSubmit)}>
        <ChannelMonitorSmartScheduleGroupPolicyFields
          form={form}
          modelOptions={[]}
        />
        <button type='submit'>保存策略</button>
      </form>
    </Form>
  )
}

describe('independent fast failure retry controls', () => {
  test('allows editing and saving retries while stability protection is disabled', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<PolicyForm stabilityEnabled={false} onSubmit={onSubmit} />)
    const retry = within(screen.getByRole('group', { name: '快速失败重试' }))
    const threshold = retry.getByRole('spinbutton', { name: '快速失败界限' })
    const count = retry.getByRole('spinbutton', { name: '同渠道快速重试' })
    const delay = retry.getByRole('spinbutton', { name: '快速重试间隔' })

    expect(threshold).toBeEnabled()
    expect(count).toBeEnabled()
    expect(delay).toBeEnabled()
    expect(screen.getByRole('switch', { name: '稳定性保护' })).not.toBeChecked()
    expect(screen.queryByText('快速失败惩罚')).not.toBeInTheDocument()
    await user.clear(threshold)
    await user.type(threshold, '15')
    await user.clear(count)
    await user.type(count, '4')
    await user.clear(delay)
    await user.type(delay, '500')
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      stabilityEnabled: false,
      fastFailureSeconds: 15,
      fastFailureSameChannelRetryCount: 4,
      fastFailureSameChannelRetryDelayMs: 500,
    })
  })

  test('retains retry values across stability toggles and allows disabling retries with zero', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<PolicyForm stabilityEnabled onSubmit={onSubmit} />)
    const stability = screen.getByRole('switch', { name: '稳定性保护' })

    await user.click(stability)
    await user.click(stability)
    await user.click(stability)

    expect(
      screen.getByRole('spinbutton', { name: '快速失败界限' })
    ).toHaveValue(2.5)
    const count = screen.getByRole('spinbutton', { name: '同渠道快速重试' })
    const delay = screen.getByRole('spinbutton', { name: '快速重试间隔' })
    expect(count).toHaveValue(2)
    expect(delay).toHaveValue(750)
    await user.clear(count)
    await user.type(count, '0')
    await user.clear(delay)
    await user.type(delay, '0')
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      stabilityEnabled: false,
      fastFailureSeconds: 2.5,
      fastFailureSameChannelRetryCount: 0,
      fastFailureSameChannelRetryDelayMs: 0,
    })
  })

  test('shows an accessible retry validation error with stability protection disabled', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<PolicyForm stabilityEnabled={false} onSubmit={onSubmit} />)
    const count = screen.getByRole('spinbutton', { name: '同渠道快速重试' })

    await user.clear(count)
    await user.type(count, '11')
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    expect(
      await screen.findByText('快速失败同渠道重试次数不能超过 10 次')
    ).toBeVisible()
    expect(count).toHaveAttribute('aria-invalid', 'true')
    expect(onSubmit).not.toHaveBeenCalled()
  })
})
