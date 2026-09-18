import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'

import type { UpstreamAccount } from '../../api-upstream-accounts'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import {
  emptyUpstreamAutomation,
  type AutomationMetadata,
} from '../../lib/automation'
import { UpstreamAutomationMetadata } from '../upstream-automation-metadata'

function MetadataFixture(props: {
  onSubmit: (values: AutomationMetadata) => void
}) {
  const form = useForm<AutomationMetadata>({
    defaultValues: emptyUpstreamAutomation(),
  })
  const channel = customVariableChannel()
  if (!channel.upstream) throw new Error('fixture requires upstream')
  const account: UpstreamAccount = {
    id: 8,
    name: '共享钱包',
    revision: 1,
    channel_ids: [channel.id],
    channel_revisions: {},
    upstream: channel.upstream,
    balance: 70,
    has_balance_key: false,
    last_balance_time: 0,
    last_balance_error: '',
    proxy: '',
    refresh_interval_minutes: 5,
  }
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(props.onSubmit)}>
        <UpstreamAutomationMetadata
          form={form}
          channels={[channel, { ...channel, id: 99, name: '其他账户渠道' }]}
          accounts={[account]}
        />
        <button type='submit'>保存</button>
      </form>
    </Form>
  )
}

test('任务下拉框支持键盘选账户和所属渠道，切回自定义后清除倍率来源', async () => {
  const onSubmit = vi.fn()
  const user = userEvent.setup()
  render(<MetadataFixture onSubmit={onSubmit} />)
  const mode = screen.getByRole('combobox', { name: '任务配置方式' })
  expect(mode).toHaveAttribute('aria-expanded', 'false')
  mode.focus()
  await user.keyboard('{Enter}{ArrowDown}{Enter}')
  expect(mode).toHaveTextContent('继承整套账户配置：共享钱包')
  const ratio = screen.getByRole('combobox', {
    name: '倍率来源渠道（倍率规则必选）',
  })
  await user.click(ratio)
  expect(
    screen.queryByRole('option', { name: '其他账户渠道' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('option', { name: '测试渠道' }))
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ account_id: 8, ratio_channel_id: 7 }),
      expect.anything()
    )
  )
  await user.click(mode)
  await user.click(
    screen.getByRole('option', { name: '自定义（可单独关联账户余额）' })
  )
  expect(
    screen.queryByRole('combobox', { name: '倍率来源渠道（倍率规则必选）' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() =>
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({ account_id: 0, ratio_channel_id: 0 }),
      expect.anything()
    )
  )
})
