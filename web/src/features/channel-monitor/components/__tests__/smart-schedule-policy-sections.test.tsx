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

const enabledFeatures = {
  sampleMode: 'traffic',
  fastFailureSameChannelRetryCount: 4,
  stabilityEnabled: true,
  immediateEjectionEnabled: true,
  degradedProbeEnabled: true,
  jitterEnabled: true,
  adaptiveSamplingEnabled: true,
} as const

function PolicySectionsForm(props: {
  values?: Partial<ChannelMonitorSmartSchedulePolicyFormValues>
  onSubmit?: (values: ChannelMonitorSmartSchedulePolicyFormValues) => void
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
      ...enabledFeatures,
      ...props.values,
    },
  })

  return (
    <Form {...form}>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => props.onSubmit?.(values))}
      >
        <ChannelMonitorSmartScheduleGroupPolicyFields
          form={form}
          modelOptions={['model-a']}
        />
        <button type='submit'>保存策略</button>
      </form>
    </Form>
  )
}

const sections = [
  {
    title: '样本补充',
    toggle: '启用样本补充',
    field: 'sampleMode',
    off: 'off',
  },
  {
    title: '快速失败重试',
    toggle: '启用快速失败重试',
    field: 'fastFailureSameChannelRetryCount',
    off: 0,
  },
  {
    title: '稳定性保护',
    toggle: '稳定性保护',
    field: 'stabilityEnabled',
    off: false,
  },
  {
    title: '立即摘除',
    toggle: '立即摘除',
    field: 'immediateEjectionEnabled',
    off: false,
  },
  {
    title: '降级期间定时探测',
    toggle: '降级期间定时探测',
    field: 'degradedProbeEnabled',
    off: false,
  },
  {
    title: '成功延迟抖动',
    toggle: '成功延迟抖动',
    field: 'jitterEnabled',
    off: false,
  },
  {
    title: '自适应备援采样',
    toggle: '自适应备援采样',
    field: 'adaptiveSamplingEnabled',
    off: false,
  },
] as const

describe('smart schedule policy sections', () => {
  test('turning off immediate ejection after clearing a threshold allows saving with a valid default', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(<PolicySectionsForm onSubmit={onSubmit} />)

    await user.clear(screen.getByRole('spinbutton', { name: '连续失败阈值' }))
    await user.click(screen.getByRole('switch', { name: '立即摘除' }))
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      immediateEjectionEnabled: false,
      stabilityEnabled: true,
      consecutiveFailureThreshold:
        CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_TEMPLATE.consecutiveFailureThreshold,
    })
  })

  test('turning off immediate ejection hides only its thresholds and keyboard re-enabling preserves them', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <PolicySectionsForm
        onSubmit={onSubmit}
        values={{ consecutiveFailureThreshold: 7 }}
      />
    )

    const group = screen.getByRole('group', { name: '立即摘除' })
    expect(
      within(group).getByRole('spinbutton', { name: '连续失败阈值' })
    ).toHaveValue(7)
    expect(
      within(screen.getByRole('group', { name: '稳定性保护' })).getByRole(
        'spinbutton',
        { name: '恢复探测成功次数' }
      )
    ).toBeVisible()

    const toggle = within(group).getByRole('switch', { name: '立即摘除' })
    await user.click(toggle)
    expect(within(group).queryByRole('spinbutton')).not.toBeInTheDocument()
    expect(screen.getByRole('switch', { name: '稳定性保护' })).toBeChecked()
    await user.keyboard(' ')
    expect(toggle).toBeChecked()
    expect(
      within(group).getByRole('spinbutton', { name: '连续失败阈值' })
    ).toHaveValue(7)
    await user.click(screen.getByRole('button', { name: '保存策略' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      immediateEjectionEnabled: true,
      consecutiveFailureThreshold: 7,
    })
  })

  test('stability disabled explains that temporary traffic cannot be ejected while preserving the ejection configuration', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <PolicySectionsForm
        onSubmit={onSubmit}
        values={{
          stabilityEnabled: false,
          consecutiveFailureThreshold: 7,
          burstFailureWindowMinutes: 3,
        }}
      />
    )

    const group = screen.getByRole('group', { name: '立即摘除' })
    expect(
      within(group).getByText(
        '需要先开启稳定性保护；关闭后临时流量也不会按失败阈值摘除'
      )
    ).toBeVisible()
    await user.click(within(group).getByRole('switch', { name: '立即摘除' }))
    await user.click(screen.getByRole('button', { name: '保存策略' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      stabilityEnabled: false,
      immediateEjectionEnabled: false,
      consecutiveFailureThreshold: 7,
      burstFailureWindowMinutes: 3,
    })
  })

  test('keeps each optional feature in its own named group with one switch', () => {
    render(<PolicySectionsForm />)

    for (const section of sections) {
      const group = screen.getByRole('group', { name: section.title })
      expect(within(group).getAllByRole('switch')).toHaveLength(1)
      expect(
        within(group).getByRole('switch', { name: section.toggle })
      ).toBeChecked()
    }
    expect(screen.getByRole('group', { name: '基础调度' })).toBeVisible()
    expect(screen.getByRole('group', { name: '评分与流量' })).toBeVisible()
  })

  test.each(sections)(
    'turning off $title saves only that feature as disabled',
    async (section) => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      render(<PolicySectionsForm onSubmit={onSubmit} />)

      await user.click(screen.getByRole('switch', { name: section.toggle }))
      expect(
        screen.getByRole('switch', { name: section.toggle })
      ).not.toBeChecked()
      await user.click(screen.getByRole('button', { name: '保存策略' }))

      await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
      expect(onSubmit.mock.calls[0][0]).toMatchObject({
        ...enabledFeatures,
        [section.field]: section.off,
      })
    }
  )

  test('re-enabling sampling and retries with the keyboard restores the current editor choices', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <PolicySectionsForm
        values={{ sampleMode: 'probe', probeIntervalMinutes: 17 }}
        onSubmit={onSubmit}
      />
    )

    const sampling = screen.getByRole('switch', { name: '启用样本补充' })
    await user.click(sampling)
    expect(sampling).not.toBeChecked()
    await user.keyboard(' ')
    expect(sampling).toBeChecked()

    const retry = screen.getByRole('switch', { name: '启用快速失败重试' })
    await user.click(retry)
    expect(
      screen.getByRole('spinbutton', { name: '同渠道快速重试' })
    ).toHaveValue(0)
    expect(
      screen.getByRole('spinbutton', { name: '同渠道快速重试' })
    ).toBeDisabled()
    expect(
      screen.getByRole('spinbutton', { name: '快速重试间隔' })
    ).toBeDisabled()
    expect(
      screen.getByRole('spinbutton', { name: '快速失败界限' })
    ).toBeEnabled()
    await user.keyboard(' ')
    expect(retry).toBeChecked()
    expect(
      screen.getByRole('spinbutton', { name: '同渠道快速重试' })
    ).toHaveValue(4)
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      sampleMode: 'probe',
      probeIntervalMinutes: 17,
      fastFailureSameChannelRetryCount: 4,
    })
  })

  test('keeps dependent groups visible and preserves their choices while stability is disabled', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <PolicySectionsForm
        values={{ degradedProbeEnabled: false, jitterEnabled: false }}
        onSubmit={onSubmit}
      />
    )

    const stability = screen.getByRole('switch', { name: '稳定性保护' })
    await user.click(stability)
    for (const name of ['降级期间定时探测', '成功延迟抖动']) {
      const group = screen.getByRole('group', { name })
      const toggle = within(group).getByRole('switch', { name })
      expect(group).toBeVisible()
      expect(toggle).toHaveAttribute('aria-disabled', 'true')
      expect(toggle).not.toBeChecked()
      await user.click(toggle)
      expect(toggle).not.toBeChecked()
      expect(within(group).getByText(/需要先开启稳定性保护/)).toBeVisible()
    }
    expect(
      screen.getByRole('switch', { name: '自适应备援采样' })
    ).not.toHaveAttribute('aria-disabled', 'true')
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      stabilityEnabled: false,
      degradedProbeEnabled: false,
      jitterEnabled: false,
      adaptiveSamplingEnabled: true,
    })
    await user.click(stability)
    expect(
      screen.getByRole('switch', { name: '降级期间定时探测' })
    ).not.toBeChecked()
    expect(
      screen.getByRole('switch', { name: '降级期间定时探测' })
    ).not.toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByRole('switch', { name: '成功延迟抖动' })
    ).not.toBeChecked()
    expect(
      screen.getByRole('switch', { name: '成功延迟抖动' })
    ).not.toHaveAttribute('aria-disabled', 'true')
  })

  test('enabling sampling in weight mode selects probe and exposes the adaptive prerequisite', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <PolicySectionsForm
        values={{
          applyMode: 'weight',
          sampleMode: 'off',
          adaptiveSamplingEnabled: false,
          fastFailureSameChannelRetryCount: 0,
        }}
        onSubmit={onSubmit}
      />
    )

    expect(
      screen.getByRole('switch', { name: '自适应备援采样' })
    ).toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByText('自适应备援采样需要先将调整方式设为“优先级分层 + 权重”')
    ).toBeVisible()
    await user.click(screen.getByRole('switch', { name: '启用样本补充' }))
    await user.click(screen.getByRole('switch', { name: '启用快速失败重试' }))
    await user.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      sampleMode: 'probe',
      fastFailureSameChannelRetryCount: 3,
      adaptiveSamplingEnabled: false,
    })
  })
})
