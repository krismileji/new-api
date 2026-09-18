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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'
import { api } from '@/lib/api'

import { customVariableFormValues } from '../../lib/__tests__/custom-variable.fixture'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorVariableRequest,
} from '../../lib/custom-upstream'
import type { UpstreamConfigFormValues } from '../../lib/schema'
import { ChannelMonitorCustomActionFields } from '../channel-monitor-custom-action-fields'
import { ChannelMonitorCustomVariableFields } from '../channel-monitor-custom-variable-fields'

function CollectionEditor(props: {
  kind: 'requests' | 'actions'
  onSave: (values: UpstreamConfigFormValues) => void
}) {
  const values = customVariableFormValues()
  const second = createChannelMonitorVariableRequest('profile', '账户信息')
  second.variables = [
    { name: 'account_id', valuePath: 'data.id', value: '', hasValue: false },
  ]
  values.customConfig.variableRequests.push(second)
  values.customConfig.actions = [
    { ...createChannelMonitorCustomAction(), id: 'first', name: '余额重置' },
    { ...createChannelMonitorCustomAction(), id: 'second', name: '额度提醒' },
  ]
  const form = useForm<UpstreamConfigFormValues>({ defaultValues: values })
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(props.onSave)}>
        {props.kind === 'requests' ? (
          <ChannelMonitorCustomVariableFields
            workspace
            form={form}
            pending={false}
            onFetch={vi.fn()}
          />
        ) : (
          <ChannelMonitorCustomActionFields
            workspace
            independent
            form={form}
            disabled={false}
            onReset={async () => undefined}
          />
        )}
        <button type='submit'>保存草稿</button>
      </form>
    </Form>
  )
}

test('从请求目录定位关闭的请求后自动展开，切换回来仍保留修改并提交全部请求', async () => {
  const onSave = vi.fn()
  const scroll = vi.spyOn(HTMLElement.prototype, 'scrollIntoView')
  const user = userEvent.setup()
  render(<CollectionEditor kind='requests' onSave={onSave} />)
  await user.type(screen.getByLabelText('当前值'), 'edited-token')
  const locate = screen.getByRole('button', { name: '定位请求 账户信息' })
  locate.focus()
  await user.keyboard('{Enter}')
  const second = await screen.findByRole('group', { name: '独立请求 账户信息' })
  expect(second).toBeVisible()
  expect(
    screen.getByRole('button', { name: '配置请求 账户信息' })
  ).toHaveAttribute('aria-expanded', 'true')
  await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: 'start' }))
  await user.type(within(second).getByLabelText('当前值'), 'account-42')
  await user.click(
    screen.getByRole('button', { name: '定位请求 登录获取凭据' })
  )
  await screen.findByRole('group', { name: '独立请求 登录获取凭据' })
  expect(screen.getByLabelText('当前值')).toHaveValue('edited-token')
  await user.click(screen.getByRole('button', { name: '保存草稿' }))
  await waitFor(() => expect(onSave).toHaveBeenCalled())
  expect(onSave.mock.calls[0][0].customConfig.variableRequests).toEqual([
    expect.objectContaining({
      id: 'login',
      variables: [expect.objectContaining({ value: 'edited-token' })],
    }),
    expect.objectContaining({
      id: 'profile',
      variables: [expect.objectContaining({ value: 'account-42' })],
    }),
  ])
})

test('删除当前请求后目录和展开项跟随剩余请求，最后一项删除后显示空状态', async () => {
  const user = userEvent.setup()
  render(<CollectionEditor kind='requests' onSave={vi.fn()} />)
  await user.click(
    screen.getByRole('button', { name: '删除请求 登录获取凭据' })
  )
  expect(
    screen.queryByRole('navigation', { name: '请求快速定位' })
  ).not.toBeInTheDocument()
  expect(
    await screen.findByRole('group', { name: '独立请求 账户信息' })
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: '删除请求 账户信息' }))
  expect(screen.getByText('尚未配置独立请求')).toBeVisible()
})

test('规则目录可键盘定位长表单内的指定规则，删除规则同步移除目录项', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: [] } })
  const scroll = vi.spyOn(HTMLElement.prototype, 'scrollIntoView')
  const user = userEvent.setup()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CollectionEditor kind='actions' onSave={vi.fn()} />
    </QueryClientProvider>
  )
  const locate = screen.getByRole('button', { name: '定位规则 额度提醒' })
  locate.focus()
  await user.keyboard('{Enter}')
  const rule = screen.getByRole('group', { name: '触发规则 额度提醒' })
  expect(rule).toHaveFocus()
  expect(scroll.mock.contexts.at(-1)).toBe(rule)
  expect(locate).toHaveAttribute('aria-pressed', 'true')
  await user.click(
    within(rule).getByRole('button', { name: '删除触发规则 额度提醒' })
  )
  expect(
    screen.queryByRole('button', { name: '定位规则 额度提醒' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('group', { name: '触发规则 余额重置' })).toBeVisible()
})
