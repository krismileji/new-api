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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UpstreamAutomation } from '../../api-automations'
import { emptyUpstreamAutomation } from '../../lib/automation'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import UpstreamAutomationsDialog from '../upstream-automations-dialog'

function savedTask(enabled = false): UpstreamAutomation {
  const config = createChannelMonitorCustomFormConfig(undefined)
  const action = createChannelMonitorCustomAction()
  action.id = 'reset'
  action.request.headers = [
    {
      key: 'Authorization',
      value: '',
      valueTemplate: '',
      secret: true,
      hasValue: true,
    },
  ]
  config.actions = [action]
  return {
    ...emptyUpstreamAutomation(),
    id: 'daily',
    revision: 3,
    name: '每日额度重置',
    enabled,
    base_url: 'https://upstream.example',
    interval_minutes: 10,
    custom_config: createChannelMonitorCustomRequestConfig(config),
  }
}

function renderList() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UpstreamAutomationsDialog channels={[]} onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
}

afterEach(() => toast.dismiss())

test('列表键盘启用及点击暂停任务后立即更新状态，保留配置并使用最新修订号', async () => {
  let task = savedTask()
  const original = structuredClone(task)
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: [task] },
  }))
  const put = vi
    .spyOn(api, 'put')
    .mockImplementation(async (_url, submitted) => {
      task = {
        ...(submitted as UpstreamAutomation),
        revision: task.revision + 1,
      }
      return { data: { success: true, data: task } }
    })
  const post = vi.spyOn(api, 'post')
  const user = userEvent.setup()
  renderList()
  const toggle = await screen.findByRole('switch', {
    name: '启用任务：每日额度重置',
  })
  expect(toggle).not.toBeChecked()
  expect(screen.getByRole('button', { name: '立即检查' })).toBeDisabled()
  toggle.focus()
  await user.keyboard(' ')
  await waitFor(() => expect(toggle).toBeChecked())
  await waitFor(() =>
    expect(toggle).not.toHaveAttribute('aria-disabled', 'true')
  )
  expect(put.mock.calls[0][0]).toBe('/api/channel_monitor/automations')
  expect(put.mock.calls[0][1]).toEqual({ ...original, enabled: true })
  expect(screen.getByText('已启用')).toBeVisible()
  expect(screen.getByRole('button', { name: '立即检查' })).toBeEnabled()
  expect(screen.getByRole('dialog')).toHaveAccessibleName('上游自动任务')

  await user.click(toggle)
  await waitFor(() => expect(toggle).not.toBeChecked())
  expect(put.mock.calls[1][1]).toEqual({
    ...original,
    enabled: false,
    revision: 4,
  })
  expect(screen.getByText('已暂停')).toBeVisible()
  expect(screen.getByRole('button', { name: '立即检查' })).toBeDisabled()
  expect(post).not.toHaveBeenCalled()
})

test('列表切换保存中显示加载状态并阻止重复提交，成功后恢复操作', async () => {
  let task = savedTask(true)
  let resolve: (value: {
    data: { success: boolean; data: UpstreamAutomation }
  }) => void = () => undefined
  const response = new Promise<{
    data: { success: boolean; data: UpstreamAutomation }
  }>((done) => {
    resolve = done
  })
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: [task] },
  }))
  const put = vi.spyOn(api, 'put').mockReturnValue(response)
  const user = userEvent.setup()
  renderList()
  const toggle = await screen.findByRole('switch', {
    name: '启用任务：每日额度重置',
  })
  await user.click(toggle)
  expect(toggle).toHaveAttribute('aria-busy', 'true')
  expect(toggle).toHaveAttribute('aria-disabled', 'true')
  expect(toggle).toBeChecked()
  expect(screen.getByLabelText('正在保存任务状态')).toBeVisible()
  expect(screen.getByRole('button', { name: '编辑任务' })).toBeDisabled()
  await user.click(toggle)
  expect(put).toHaveBeenCalledTimes(1)
  task = { ...task, enabled: false, revision: 4 }
  await act(async () => {
    resolve({ data: { success: true, data: task } })
    await response
  })
  await waitFor(() =>
    expect(toggle).not.toHaveAttribute('aria-disabled', 'true')
  )
  expect(toggle).not.toBeChecked()
  expect(screen.queryByLabelText('正在保存任务状态')).not.toBeInTheDocument()
})

test.each(['接口拒绝', '网络失败'] as const)(
  '%s 时保留已保存的启用状态，显示错误并允许重试',
  async (failure) => {
    const task = savedTask(true)
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [task] },
    })
    const put = vi.spyOn(api, 'put')
    if (failure === '接口拒绝') {
      put.mockResolvedValue({
        data: {
          success: false,
          message: '任务正在执行或配置已变化，请刷新后重试',
        },
      })
    } else {
      put.mockRejectedValue(new Error('网络连接失败'))
    }
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    const user = userEvent.setup()
    renderList()
    const toggle = await screen.findByRole('switch', {
      name: '启用任务：每日额度重置',
    })
    await user.click(toggle)
    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith(
        failure === '接口拒绝'
          ? '任务正在执行或配置已变化，请刷新后重试'
          : '网络连接失败'
      )
    )
    await waitFor(() =>
      expect(toggle).not.toHaveAttribute('aria-disabled', 'true')
    )
    expect(toggle).toBeChecked()
    expect(screen.getByText('已启用')).toBeVisible()
    expect(successToast).not.toHaveBeenCalled()
  }
)

test('任务正在检查时列表开关禁用，不提交可能与执行冲突的配置', async () => {
  const task = savedTask(true)
  task.state.lease_until = Math.floor(Date.now() / 1000) + 3600
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: [task] },
  })
  const put = vi.spyOn(api, 'put')
  const user = userEvent.setup()
  renderList()
  const toggle = await screen.findByRole('switch', {
    name: '启用任务：每日额度重置',
  })
  expect(toggle).toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByText('正在检查')).toBeVisible()
  await user.click(toggle)
  expect(put).not.toHaveBeenCalled()
})
