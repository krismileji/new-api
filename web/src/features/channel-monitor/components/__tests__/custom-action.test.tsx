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
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

function renderActionDialog(channel = customVariableChannel()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <UpstreamConfigDialog
        channel={channel}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

describe('条件触发接口编辑', () => {
  test('添加规则并修改阈值后保存请求与时段限制', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderActionDialog()
    await user.click(screen.getByRole('button', { name: '添加触发规则' }))
    const rule = screen.getByRole('group', { name: '触发规则 余额不足时重置' })
    expect(
      within(rule).getByRole('switch', { name: '启用规则' })
    ).not.toBeChecked()
    await user.click(within(rule).getByRole('switch', { name: '启用规则' }))
    const threshold = within(rule).getByLabelText('触发阈值')
    await user.clear(threshold)
    await user.type(threshold, '5')
    expect(within(rule).getByLabelText('截止时间（不含）')).toHaveValue('23:00')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: {
        actions: [
          {
            enabled: true,
            threshold: 5,
            metric: 'balance',
            operator: 'lt',
            timezone: 'Asia/Shanghai',
            end_time: '23:00',
            daily_limit: 1,
            request: { method: 'POST', path: '/api/reset' },
          },
        ],
      },
    })
  })

  test('删除规则只删除该规则并在保存时提交空列表', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderActionDialog()
    await user.click(screen.getByRole('button', { name: '添加触发规则' }))
    await user.click(
      screen.getByRole('button', { name: '删除触发规则 余额不足时重置' })
    )
    expect(screen.queryByLabelText('触发阈值')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: { actions: [], variable_requests: [{ id: 'login' }] },
    })
  })

  test('重新打开时显示失败原因和已消耗次数', () => {
    const channel = customVariableChannel()
    if (!channel.upstream) throw new Error('缺少测试上游')
    const config = createChannelMonitorCustomFormConfig(
      channel.upstream.custom_config
    )
    const action = createChannelMonitorCustomAction()
    config.actions = [action]
    channel.upstream.custom_config =
      createChannelMonitorCustomRequestConfig(config)
    channel.upstream.custom_action_states = {
      [action.id]: {
        triggered: true,
        day: '2026-09-13',
        attempts: 1,
        last_attempt: 1789308000,
        last_value: 5,
        status: 'failed',
        message: '接口调用失败或结果未知，未自动重试，请核对上游结果',
      },
    }
    renderActionDialog(channel)
    const rule = screen.getByRole('group', { name: '触发规则 余额不足时重置' })
    expect(within(rule).getByRole('status')).toHaveTextContent('未自动重试')
    expect(within(rule).getByRole('status')).toHaveTextContent(
      '2026-09-13 已调用 1 次'
    )
  })
})
