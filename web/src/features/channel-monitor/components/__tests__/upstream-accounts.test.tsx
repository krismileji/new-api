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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { UpstreamAccountEditor } from '../upstream-account-editor'
import UpstreamAccountsDialog from '../upstream-accounts-dialog'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

describe('共享上游账户', () => {
  test('创建账户先预览多渠道差异，修改选择后必须重新预览', async () => {
    const channel = customVariableChannel()
    const second = { ...channel, id: 22, name: '第二渠道', ratio: 2 }
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: [
          { channel_id: channel.id, revision: 3, fields: [] },
          { channel_id: 22, revision: 5, fields: ['余额查询及共享变量'] },
        ],
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    const saved = vi.fn()
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <UpstreamAccountEditor
          channels={[channel, second]}
          onClose={() => undefined}
          onSaved={saved}
        />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('账户名称'), '共享钱包')
    await user.selectOptions(
      screen.getByLabelText('配置来源渠道'),
      String(channel.id)
    )
    expect(
      screen.getByRole('checkbox', { name: new RegExp(channel.name) })
    ).toHaveAttribute('aria-disabled', 'true')
    await user.click(screen.getByRole('checkbox', { name: /第二渠道/ }))
    expect(screen.getByRole('button', { name: '确认关联' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '预览配置差异' }))
    await screen.findByText(/统一余额查询及共享变量/)
    expect(screen.queryByText(/合并前暂停执行/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认关联' })).toBeEnabled()
    await user.type(screen.getByLabelText('账户名称'), '更新')
    expect(screen.getByRole('button', { name: '确认关联' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '预览配置差异' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '确认关联' })).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: '确认关联' }))
    await waitFor(() => expect(saved).toHaveBeenCalledOnce())
    expect(put.mock.calls[0][1]).toMatchObject({
      name: '共享钱包更新',
      source_channel_id: channel.id,
      channel_ids: [channel.id, 22],
      channel_revisions: { [channel.id]: 3, 22: 5 },
      refresh_interval_minutes: 5,
    })
  })

  test('账户加载失败可重试，空列表显示创建入口', async () => {
    vi.spyOn(api, 'get')
      .mockRejectedValueOnce(new Error('暂时不可用'))
      .mockResolvedValue({ data: { success: true, data: [] } })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UpstreamAccountsDialog channels={[]} onOpenChange={() => undefined} />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    expect(await screen.findByRole('alert')).toHaveTextContent('账户加载失败')
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('尚无上游账户')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '从渠道创建账户' })).toBeEnabled()
  })

  test('已有关联渠道的账户仅提供账户管理操作，不再提供任务合并入口', async () => {
    const channel = customVariableChannel()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [
          {
            id: 8,
            revision: 1,
            name: '共享钱包',
            channel_ids: [channel.id],
            refresh_interval_minutes: 5,
            balance: null,
            upstream: channel.upstream,
          },
        ],
      },
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <UpstreamAccountsDialog
          channels={[channel]}
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
    expect(await screen.findByText('共享钱包')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '管理关联' })).toBeEnabled()
    expect(
      screen.queryByRole('button', { name: '合并关联任务' })
    ).not.toBeInTheDocument()
  })

  test('已关联渠道的共享地址和阈值不可编辑，倍率配置仍可用', () => {
    const channel = customVariableChannel()
    expect(channel.upstream).toBeDefined()
    if (!channel.upstream) throw new Error('fixture requires upstream settings')
    channel.upstream = { ...channel.upstream, upstream_account_id: 8 }
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    render(
      <QueryClientProvider client={client}>
        <UpstreamConfigDialog
          channel={channel}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
    expect(screen.getByLabelText('接口基础地址')).toBeDisabled()
    expect(screen.getByLabelText('余额预警值')).toBeDisabled()
    expect(screen.getByRole('group', { name: /倍率/ })).toBeInTheDocument()
    expect(
      screen.queryByRole('group', { name: '余额配置' })
    ).not.toBeInTheDocument()
  })
})
