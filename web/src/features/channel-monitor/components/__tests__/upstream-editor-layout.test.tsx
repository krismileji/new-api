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
import type { ReactNode } from 'react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UpstreamAccount } from '../../api-upstream-accounts'
import type { ChannelMonitorVariableGroup } from '../../api-variable-groups'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { ChannelMonitorVariableGroupsDialog } from '../channel-monitor-variable-groups-dialog'
import UpstreamAccountsDialog from '../upstream-accounts-dialog'
import UpstreamAutomationsDialog from '../upstream-automations-dialog'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

function renderEditor(element: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}

function sharedGroup(): ChannelMonitorVariableGroup {
  return {
    id: 3,
    name: '共用登录',
    base_url: 'https://upstream.example',
    proxy: '',
    request_timeout: 30,
    revision: 1,
    variable_requests:
      customVariableChannel().upstream?.custom_config?.variable_requests ?? [],
  }
}

test.each(['共享配置', '账户关联', '自动任务'] as const)(
  '%s 进入编辑后铺满视口，操作按钮位于内容滚动区外，返回后恢复列表',
  async (kind) => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const user = userEvent.setup()
    if (kind === '共享配置') {
      renderEditor(
        <ChannelMonitorVariableGroupsDialog onOpenChange={vi.fn()} />
      )
    }
    if (kind === '账户关联') {
      renderEditor(
        <UpstreamAccountsDialog channels={[]} onOpenChange={vi.fn()} />
      )
    }
    if (kind === '自动任务') {
      renderEditor(
        <UpstreamAutomationsDialog channels={[]} onOpenChange={vi.fn()} />
      )
    }
    const createLabel = {
      共享配置: '新建共享配置',
      账户关联: '从渠道创建账户',
      自动任务: '新建自动任务',
    }[kind]
    const create = await screen.findByRole('button', { name: createLabel })
    await waitFor(() => expect(create).toBeEnabled())
    await user.click(create)
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveClass(
      'h-dvh',
      'max-w-none',
      'sm:max-w-none',
      'rounded-none',
      'overflow-hidden'
    )
    const content = screen.getByRole('region', { name: '配置内容' })
    expect(content).toHaveClass(
      'min-h-0',
      'overflow-y-auto',
      'overscroll-contain'
    )
    expect(screen.getByRole('navigation', { name: '配置目录' })).toHaveClass(
      'overflow-x-auto',
      'md:flex-col'
    )
    const saveLabel = {
      共享配置: '保存共享配置',
      账户关联: '确认关联',
      自动任务: '保存任务',
    }[kind]
    expect(content).not.toContainElement(
      screen.getByRole('button', { name: saveLabel })
    )
    await user.click(
      screen.getByRole('button', {
        name: kind === '自动任务' ? '返回任务列表' : '返回列表',
      })
    )
    expect(screen.getByRole('dialog')).not.toHaveClass('h-dvh')
    expect(screen.getByRole('button', { name: createLabel })).toBeVisible()
  }
)

test.each(['custom', 'new_api'] as const)(
  '账户 %s 共享配置使用全屏分区和独立操作栏',
  (type) => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const channel = customVariableChannel()
    if (!channel.upstream) throw new Error('missing upstream fixture')
    const account: UpstreamAccount = {
      id: 8,
      revision: 1,
      name: '共享钱包',
      channel_ids: [channel.id],
      channel_revisions: {},
      upstream: {
        ...channel.upstream,
        type,
        auth_type: type === 'custom' ? 'custom' : 'public',
      },
      balance: null,
      has_balance_key: false,
      last_balance_time: 0,
      last_balance_error: '',
      proxy: '',
      refresh_interval_minutes: 5,
    }
    renderEditor(
      <UpstreamConfigDialog
        open
        channel={channel}
        account={account}
        onOpenChange={vi.fn()}
      />
    )
    expect(
      screen.getByRole('dialog', { name: '编辑账户共享配置' })
    ).toHaveClass('h-dvh', 'rounded-none')
    const content = screen.getByRole('region', { name: '配置内容' })
    expect(content).not.toContainElement(
      screen.getByRole('button', { name: '保存' })
    )
    expect(
      screen.getByRole('region', {
        name: type === 'custom' ? '余额查询' : '认证配置',
      })
    ).toBeVisible()
  }
)

test('键盘使用目录定位分区后保留已编辑字段，焦点进入目标分区', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: [] } })
  const scroll = vi.spyOn(HTMLElement.prototype, 'scrollIntoView')
  const user = userEvent.setup()
  renderEditor(
    <ChannelMonitorVariableGroupsDialog
      initialGroup={sharedGroup()}
      onOpenChange={vi.fn()}
    />
  )
  const name = screen.getByLabelText('共享配置名称')
  await user.clear(name)
  await user.type(name, '新的共享配置')
  const nav = screen.getByRole('navigation', { name: '配置目录' })
  const variablesLink = within(nav).getByRole('button', { name: /请求与变量/ })
  variablesLink.focus()
  await user.keyboard('{Enter}')
  const target = screen.getByRole('region', { name: '请求与变量' })
  expect(target).toHaveFocus()
  expect(scroll.mock.contexts.at(-1)).toBe(target)
  expect(variablesLink).toHaveAttribute('aria-current', 'location')
  await user.click(within(nav).getByRole('button', { name: /基本设置/ }))
  expect(name).toHaveValue('新的共享配置')
})

test('变量速查随编辑更新来源和变量名，敏感值不出现在速查区域', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: [] } })
  const user = userEvent.setup()
  renderEditor(
    <ChannelMonitorVariableGroupsDialog
      initialGroup={sharedGroup()}
      onOpenChange={vi.fn()}
    />
  )
  await user.clear(screen.getByLabelText('变量名'))
  await user.type(screen.getByLabelText('变量名'), 'session_token')
  await user.type(screen.getByLabelText('当前值'), 'secret-not-for-reference')
  const reference = screen.getByRole('region', { name: '变量速查' })
  expect(reference).toHaveTextContent('{{session_token}}')
  expect(reference).toHaveTextContent('登录获取凭据')
  expect(reference).toHaveTextContent('data.token')
  expect(reference).not.toHaveTextContent('secret-not-for-reference')
})
