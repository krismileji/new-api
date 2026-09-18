import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
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
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelLimitGroup } from '../../api-limit-groups'
import { emptyChannelLimitGroup } from '../../lib/limit-group'
import type { ChannelMonitorItem } from '../../types'
import { ChannelLimitGroupEditor } from '../channel-limit-group-editor'

const originalAdapter = api.defaults.adapter
const channels = [
  { id: 1, name: '重要渠道' },
  { id: 2, name: '普通渠道' },
] as ChannelMonitorItem[]
let client: QueryClient
afterEach(() => {
  cleanup()
  api.defaults.adapter = originalAdapter
  client?.clear()
})
function mount(group: ChannelLimitGroup, groups: ChannelLimitGroup[] = []) {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ChannelLimitGroupEditor
        group={group}
        groups={groups}
        channels={channels}
        onDone={() => {}}
      />
    </QueryClientProvider>
  )
}
test('新建组保存后直接启用成员等级与总额度', async () => {
  const user = userEvent.setup()
  const bodies: unknown[] = []
  api.defaults.adapter = async (config) => {
    bodies.push(JSON.parse(config.data as string))
    return {
      data: { success: true, data: { id: 3 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  mount(emptyChannelLimitGroup())
  fireEvent.change(screen.getByLabelText('组名称'), {
    target: { value: '上游账号 A' },
  })
  fireEvent.change(screen.getByLabelText('总并发限制'), {
    target: { value: '10' },
  })
  fireEvent.click(screen.getByRole('checkbox', { name: /重要渠道/ }))
  const priority = screen.getByRole('combobox', {
    name: '重要渠道 的资源优先级',
  })
  expect(priority).toHaveTextContent(/^0$/)
  expect(priority).toHaveAttribute('aria-expanded', 'false')
  await user.click(priority)
  expect(priority).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByRole('listbox')).toBeVisible()
  expect(screen.getByRole('option', { name: '0' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  await user.click(screen.getByRole('option', { name: '100' }))
  expect(priority).toHaveTextContent(/^100$/)
  expect(priority).toHaveAttribute('aria-expanded', 'false')
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(bodies).toHaveLength(1))
  expect(bodies[0]).toMatchObject({
    name: '上游账号 A',
    enabled: true,
    concurrency_limit: 10,
    members: [{ channel_id: 1, priority: 100 }],
  })
})
test('启用组可以在线修改成员等级和预留结构', async () => {
  const user = userEvent.setup()
  const bodies: unknown[] = []
  api.defaults.adapter = async (config) => {
    bodies.push(JSON.parse(config.data as string))
    return {
      data: { success: true, data: { id: 1 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const group = {
    ...emptyChannelLimitGroup(),
    id: 1,
    name: '运行中的共享组',
    concurrency_limit: 10,
    enabled: true,
    members: [{ channel_id: 1, priority: 100 }],
  }
  mount(group)
  expect(
    screen.getByRole('checkbox', { name: /重要渠道/ })
  ).not.toHaveAttribute('aria-disabled', 'true')
  expect(screen.getAllByLabelText('预留并发')[0]).not.toBeDisabled()
  const priority = screen.getByRole('combobox', {
    name: '重要渠道 的资源优先级',
  })
  expect(priority).not.toBeDisabled()
  await user.click(priority)
  await user.click(screen.getByRole('option', { name: '0' }))
  expect(priority).toHaveTextContent(/^0$/)
  expect(screen.getByLabelText('组名称')).not.toBeDisabled()
  expect(screen.getByLabelText('总并发限制')).not.toBeDisabled()
  expect(screen.getByLabelText('总 RPM 限制')).not.toBeDisabled()
  fireEvent.change(screen.getAllByLabelText('预留并发')[0], {
    target: { value: '2' },
  })
  await user.click(screen.getByRole('checkbox', { name: /普通渠道/ }))
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(bodies).toHaveLength(1))
  expect(bodies[0]).toMatchObject({
    enabled: true,
    tiers: [
      { priority: 100, reserved_concurrency: 2 },
      { priority: 0, reserved_concurrency: 0 },
    ],
    members: [
      { channel_id: 1, priority: 0 },
      { channel_id: 2, priority: 0 },
    ],
  })
})
test('成员等级下拉框支持键盘切换到零优先级并保留选中状态', async () => {
  const user = userEvent.setup()
  mount({
    ...emptyChannelLimitGroup(),
    members: [{ channel_id: 1, priority: 100 }],
  })
  const priority = screen.getByRole('combobox', {
    name: '重要渠道 的资源优先级',
  })
  priority.focus()
  await user.keyboard('{Enter}{ArrowDown}{Enter}')
  expect(priority).toHaveTextContent(/^0$/)
  expect(priority).toHaveAttribute('aria-expanded', 'false')
  expect(priority).toHaveFocus()
  await user.click(priority)
  expect(screen.getByRole('option', { name: '0' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
})
test('保存期间禁用成员等级下拉框，保存结束后恢复编辑', async () => {
  const user = userEvent.setup()
  let completeRequest: () => void = () => {}
  const pendingRequest = new Promise<void>((resolve) => {
    completeRequest = resolve
  })
  api.defaults.adapter = async (config) => {
    await pendingRequest
    return {
      data: { success: true, data: { id: 3 } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  mount({
    ...emptyChannelLimitGroup(),
    name: '上游账号 A',
    members: [{ channel_id: 1, priority: 100 }],
  })
  const priority = screen.getByRole('combobox', {
    name: '重要渠道 的资源优先级',
  })
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(priority).toBeDisabled())
  await user.click(priority)
  expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  completeRequest()
  await waitFor(() => expect(priority).not.toBeDisabled())
})
test('属于其他组的渠道不可重复勾选', () => {
  const other = {
    ...emptyChannelLimitGroup(),
    id: 7,
    members: [{ channel_id: 2, priority: 0 }],
  }
  mount(emptyChannelLimitGroup(), [other])
  expect(screen.getByRole('checkbox', { name: /普通渠道/ })).toHaveAttribute(
    'aria-disabled',
    'true'
  )
})
test('服务器版本冲突显示错误并保留表单', async () => {
  api.defaults.adapter = async (config) => {
    throw new AxiosError(
      'Request failed with status code 409',
      'ERR_BAD_REQUEST',
      config,
      undefined,
      {
        data: { success: false, message: '共享限流配置已变化，请刷新后重试' },
        status: 409,
        statusText: 'Conflict',
        headers: {},
        config,
      }
    )
  }
  mount({
    ...emptyChannelLimitGroup(),
    name: '原配置',
    members: [{ channel_id: 1, priority: 100 }],
  })
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() =>
    expect(screen.getByText('共享限流配置已变化，请刷新后重试')).toBeVisible()
  )
  expect(screen.getByLabelText('组名称')).toHaveValue('原配置')
})
