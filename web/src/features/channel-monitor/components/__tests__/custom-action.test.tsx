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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

afterEach(() => vi.useRealTimers())

function channelWithAction() {
  const channel = customVariableChannel()
  if (!channel.upstream) throw new Error('缺少测试上游')
  const config = createChannelMonitorCustomFormConfig(
    channel.upstream.custom_config
  )
  const action = createChannelMonitorCustomAction()
  action.id = 'reset'
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
  return { ...channel, upstream: channel.upstream }
}

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
    expect(
      within(rule).getByRole('button', { name: '重置今日次数' })
    ).toBeDisabled()
    expect(
      within(rule).getByText('保存规则后开始统计调用次数。')
    ).toBeInTheDocument()
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

  test('修改上限后保留今日次数且保存不会请求重置', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-13T15:59:00Z'))
    const user = userEvent.setup()
    const channel = channelWithAction()
    const post = vi.spyOn(api, 'post')
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderActionDialog(channel)
    const rule = screen.getByRole('group', { name: '触发规则 余额不足时重置' })
    expect(within(rule).getByRole('status')).toHaveTextContent('未自动重试')
    expect(
      within(rule).getByText('今日已调用 1 / 1 次，剩余 0 次')
    ).toBeInTheDocument()
    await user.clear(within(rule).getByLabelText('每日最多调用次数'))
    await user.type(within(rule).getByLabelText('每日最多调用次数'), '3')
    expect(
      within(rule).getByText('今日已调用 1 / 1 次，剩余 0 次')
    ).toBeInTheDocument()
    expect(
      within(rule).getByText('保存后上限为 3 次，已调用次数保留。')
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: { actions: [{ id: 'reset', daily_limit: 3 }] },
    })
    expect(post).not.toHaveBeenCalled()
  })

  test('确认重置后立即显示剩余次数并保留触发状态和执行记录', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-13T15:59:00Z'))
    const user = userEvent.setup()
    const channel = channelWithAction()
    const state = channel.upstream.custom_action_states?.reset
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { ...state, attempts: 0 } },
    })
    const put = vi.spyOn(api, 'put')
    renderActionDialog(channel)
    await user.click(screen.getByRole('button', { name: '重置今日次数' }))
    const confirmation = screen.getByRole('alertdialog')
    expect(confirmation).toHaveTextContent(
      '冷却时间、已触发状态和执行记录均保留'
    )
    expect(post).not.toHaveBeenCalled()
    await user.click(
      within(confirmation).getByRole('button', { name: '确认重置' })
    )
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(post).toHaveBeenCalledWith(
      '/api/channel_monitor/channel/7/upstream/actions/reset/reset-count',
      { day: '2026-09-13', attempts: 1, last_attempt: 1789308000 },
      expect.objectContaining({
        skipBusinessError: true,
        skipErrorHandler: true,
      })
    )
    expect(
      screen.getByText('今日已调用 0 / 1 次，剩余 1 次')
    ).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('未自动重试')
    expect(
      screen.getByText('等待指标恢复到触发条件外后重新检测。')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重置今日次数' })).toBeDisabled()
    expect(put).not.toHaveBeenCalled()
  })

  test('执行时区跨日后今日次数为零并保留昨日失败记录', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-13T16:01:00Z'))
    renderActionDialog(channelWithAction())
    expect(
      screen.getByText('今日已调用 0 / 1 次，剩余 1 次')
    ).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('未自动重试')
    expect(screen.getByRole('button', { name: '重置今日次数' })).toBeDisabled()
  })

  test('调用记录发生变化时显示重置失败并保留原计数', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-13T15:59:00Z'))
    const user = userEvent.setup()
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: '调用记录已变化，请刷新后重试' },
    })
    renderActionDialog(channelWithAction())
    await user.click(screen.getByRole('button', { name: '重置今日次数' }))
    await user.click(screen.getByRole('button', { name: '确认重置' }))
    await waitFor(() =>
      expect(
        within(screen.getByRole('alertdialog')).getByRole('alert')
      ).toHaveTextContent('调用记录已变化')
    )
    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(
      screen.getByText('今日已调用 1 / 1 次，剩余 0 次')
    ).toBeInTheDocument()
  })

  test('接口仍在执行时禁止重置今日次数', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-13T15:59:00Z'))
    const channel = channelWithAction()
    if (!channel.upstream.custom_action_states) throw new Error('缺少调用记录')
    channel.upstream.custom_action_states.reset.status = 'running'
    renderActionDialog(channel)
    expect(screen.getByRole('button', { name: '重置今日次数' })).toBeDisabled()
    expect(
      screen.getByText('接口正在执行或结果尚未确认，暂不能重置次数。')
    ).toBeInTheDocument()
  })
})
