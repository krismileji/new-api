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

import type { UpstreamAutomation } from '../../api-automations'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { emptyUpstreamAutomation } from '../../lib/automation'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
  createChannelMonitorVariableRequest,
} from '../../lib/custom-upstream'
import UpstreamAutomationsDialog from '../upstream-automations-dialog'

function taskFixture(): UpstreamAutomation {
  const task = emptyUpstreamAutomation()
  const config = createChannelMonitorCustomFormConfig(undefined)
  const action = createChannelMonitorCustomAction()
  action.id = 'reset'
  action.enabled = true
  action.triggerMode = 'repeat'
  config.actions = [action]
  return {
    ...task,
    id: 'account-one',
    revision: 2,
    name: '主账户',
    enabled: true,
    base_url: 'https://upstream.example',
    custom_config: createChannelMonitorCustomRequestConfig(config),
  }
}

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const channel = customVariableChannel()
  channel.status = 2
  return render(
    <QueryClientProvider client={client}>
      <UpstreamAutomationsDialog
        channels={[channel]}
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

describe('独立上游自动任务', () => {
  test('空列表可创建不关联渠道的任务并切换触发模式', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: taskFixture() } })
    const user = userEvent.setup()
    renderDialog()
    expect(await screen.findByText('尚无上游自动任务')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '新建自动任务' }))
    expect(screen.getByRole('dialog')).toHaveAccessibleName('编辑上游自动任务')
    await user.type(screen.getByLabelText('任务名称 / 上游账户'), '独立账户')
    await user.type(
      screen.getByLabelText('上游基础地址'),
      'https://upstream.example'
    )
    await user.click(screen.getByRole('switch', { name: '启用独立任务' }))
    await user.clear(screen.getByLabelText('检查间隔（分钟）'))
    await user.type(screen.getByLabelText('检查间隔（分钟）'), '1')
    await user.clear(screen.getByLabelText('请求超时（秒）'))
    await user.type(screen.getByLabelText('请求超时（秒）'), '30')
    await user.click(screen.getByRole('button', { name: '添加触发规则' }))
    const rule = screen.getByRole('group', { name: '触发规则 余额不足时重置' })
    await user.click(within(rule).getByRole('switch', { name: '启用规则' }))
    const repeat = within(rule).getByRole('button', { name: '持续满足' })
    expect(repeat).toHaveAttribute('aria-pressed', 'true')
    const edge = within(rule).getByRole('button', { name: '首次满足' })
    edge.focus()
    await user.keyboard('{Enter}')
    expect(edge).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: '保存任务' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][0]).toBe('/api/channel_monitor/automations')
    expect(put.mock.calls[0][1]).toMatchObject({
      name: '独立账户',
      enabled: true,
      interval_minutes: 1,
      request_timeout: 30,
      channel_ids: [],
      custom_config: { actions: [{ trigger_mode: 'edge' }] },
    })
  })

  test.each(['保存任务', '测试获取指标', '请求并回填变量'])(
    '编辑检查间隔和超时后%s提交整数',
    async (button) => {
      const task = taskFixture()
      const config = createChannelMonitorCustomFormConfig(task.custom_config)
      const request = createChannelMonitorVariableRequest('login', '登录请求')
      request.request.path = '/login'
      request.variables = [
        { name: 'token', valuePath: 'token', value: '', hasValue: false },
      ]
      config.variableRequests = [request]
      task.custom_config = createChannelMonitorCustomRequestConfig(config)
      vi.spyOn(api, 'get').mockImplementation(async (path) => ({
        data: {
          success: true,
          data: path === '/api/channel_monitor/automations' ? [task] : [],
        },
      }))
      const put = vi
        .spyOn(api, 'put')
        .mockResolvedValue({ data: { success: true, data: task } })
      const post = vi.spyOn(api, 'post').mockImplementation(async (path) => ({
        data: {
          success: true,
          data: String(path).endsWith('/variable/fetch')
            ? [{ name: 'token', value: 'test-token' }]
            : { ratio: 1, balance: { amount: 100 } },
        },
      }))
      const user = userEvent.setup()
      renderDialog()
      await user.click(await screen.findByRole('button', { name: '编辑任务' }))
      await user.clear(screen.getByLabelText('检查间隔（分钟）'))
      await user.type(screen.getByLabelText('检查间隔（分钟）'), '1')
      await user.clear(screen.getByLabelText('请求超时（秒）'))
      await user.type(screen.getByLabelText('请求超时（秒）'), '45')
      await user.click(screen.getByRole('button', { name: button }))
      const mutation = button === '保存任务' ? put : post
      await waitFor(() => expect(mutation).toHaveBeenCalled())
      expect(mutation.mock.calls[0][1]).toMatchObject({
        id: task.id,
        revision: task.revision,
        interval_minutes: 1,
        request_timeout: 45,
      })
    }
  )

  test('查询失败显示错误并可重试恢复列表', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({
        data: { success: false, message: '数据库暂时不可用' },
      })
      .mockResolvedValue({ data: { success: true, data: [] } })
    const user = userEvent.setup()
    renderDialog()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      '数据库暂时不可用'
    )
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('尚无上游自动任务')).toBeInTheDocument()
    expect(get).toHaveBeenCalledTimes(2)
  })

  test('单个旧渠道迁移失败时仍显示可操作的独立任务', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [taskFixture()],
        migration_warning: '渠道 9 配置无效',
      },
    })
    renderDialog()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      '渠道 9 配置无效'
    )
    expect(screen.getByText('主账户')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '立即检查' })).toBeEnabled()
  })

  test('显示跳过原因并在人工确认后仅提交指定执行记录', async () => {
    const task = taskFixture()
    task.state.actions.reset = {
      triggered: true,
      day: '2026-09-17',
      attempts: 1,
      last_attempt: 100,
      last_value: 0,
      status: 'failed',
      message: '接口结果未知',
      attempt_id: 'attempt-1',
      needs_confirmation: true,
      skip_reason: '上次执行结果待人工确认',
    }
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [task] },
    })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data: null } })
    const user = userEvent.setup()
    renderDialog()
    expect(
      await screen.findByText('上次执行结果待人工确认')
    ).toBeInTheDocument()
    await user.click(
      screen.getByRole('button', { name: '核对结果并解除触发限制' })
    )
    const confirmation = screen.getByRole('alertdialog')
    expect(confirmation).toHaveTextContent('今日次数与冷却时间保留')
    expect(post).not.toHaveBeenCalled()
    await user.click(
      within(confirmation).getByRole('button', { name: '已核对，解除限制' })
    )
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/channel_monitor/automations/account-one/actions/reset/acknowledge',
        { revision: 2, attempt_id: 'attempt-1' },
        expect.anything()
      )
    )
    expect(
      post.mock.calls.some(([path]) => String(path).endsWith('/run'))
    ).toBe(false)
  })

  test('正在执行时禁止重复检查编辑删除并保留长名称和记录入口', async () => {
    const task = taskFixture()
    task.name = '很长的上游账户名称'.repeat(8)
    task.state.lease_until = Math.floor(Date.now() / 1000) + 300
    task.state.history = [
      {
        id: 'check-1',
        time: 100,
        status: 'checked',
        message: '冷却期间未调用接口',
      },
    ]
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [task] },
    })
    renderDialog()
    expect(await screen.findByText(task.name)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '立即检查' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '编辑任务' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeDisabled()
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: '规则与记录（1）' }))
    expect(screen.getByText('最近检查记录')).toBeInTheDocument()
  })
})
