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
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelMonitorVariableGroup } from '../../api-variable-groups'
import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { ChannelMonitorVariableGroupsDialog } from '../channel-monitor-variable-groups-dialog'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

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

function renderWithQueries(element: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}

afterEach(() => {
  toast.dismiss()
})

describe('共享请求与变量管理', () => {
  test('渠道选择共享配置后隐藏独立编辑器，只保存引用并提供共享变量', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [sharedGroup()] },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderWithQueries(
      <UpstreamConfigDialog
        open
        channel={customVariableChannel()}
        onOpenChange={vi.fn()}
      />
    )
    const selector = await screen.findByRole('combobox', {
      name: '共享请求与变量',
    })
    await waitFor(() => expect(selector).toBeEnabled())
    await user.click(selector)
    await user.click(screen.getByRole('option', { name: '共用登录' }))
    expect(screen.queryByLabelText('当前值')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '插入变量' })[0]).toBeEnabled()
    await user.click(screen.getAllByRole('button', { name: '插入变量' })[0])
    expect(screen.getByRole('menuitem', { name: '{{token}}' })).toBeVisible()
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: { variable_group_id: 3, variable_requests: [] },
    })
  })

  test('渠道未选共享配置时插入变量同步选择来源，保存引用和参数模板', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [sharedGroup()] },
    })
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true, data: {} },
    })
    const channel = customVariableChannel()
    const config = channel.upstream?.custom_config
    if (!config?.ratio.request?.headers) throw new Error('缺少渠道接口测试配置')
    config.variable_requests = []
    config.ratio.request.headers[0].value_template = 'Bearer '
    renderWithQueries(
      <UpstreamConfigDialog open channel={channel} onOpenChange={vi.fn()} />
    )

    const ratio = screen.getByRole('group', { name: '上游倍率来源' })
    await user.click(within(ratio).getByRole('button', { name: '插入变量' }))
    await user.click(await screen.findByRole('menuitem', { name: '{{token}}' }))
    expect(within(ratio).getByLabelText('请求头 1 变量模板')).toHaveValue(
      'Bearer {{token}}'
    )
    expect(
      screen.getByRole('combobox', { name: '共享请求与变量' })
    ).toHaveTextContent('共用登录')

    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: {
        variable_group_id: 3,
        variable_requests: [],
        ratio: {
          request: {
            headers: [
              { key: 'Authorization', value_template: 'Bearer {{token}}' },
            ],
          },
        },
      },
    })
  })

  test('无需渠道即可新建共享配置，保存名称、地址、请求和变量', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockImplementation(async (_url, payload) => ({
        data: {
          success: true,
          data: { ...(payload as object), id: 5, revision: 1 },
        },
      }))
    renderWithQueries(
      <ChannelMonitorVariableGroupsDialog onOpenChange={vi.fn()} />
    )
    expect(await screen.findByText('尚无共享配置')).toBeVisible()
    await user.click(screen.getByRole('button', { name: '新建共享配置' }))
    await user.type(screen.getByLabelText('共享配置名称'), '上游共享登录')
    await user.type(
      screen.getByLabelText('共享请求基础地址'),
      'https://upstream.example'
    )
    await user.click(screen.getByRole('button', { name: '添加独立请求' }))
    await user.type(screen.getByLabelText('变量名'), 'token')
    await user.type(screen.getByLabelText('JSON 取值路径'), 'data.token')
    await user.type(screen.getByLabelText('当前值'), 'shared-token')
    await user.click(screen.getByRole('button', { name: '保存共享配置' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][0]).toBe('/api/channel_monitor/variable_groups')
    expect(put.mock.calls[0][1]).toMatchObject({
      name: '上游共享登录',
      base_url: 'https://upstream.example',
      variable_requests: [
        { variables: [{ name: 'token', value: 'shared-token' }] },
      ],
    })
    expect(put.mock.calls[0][1]).not.toHaveProperty('custom_config')
  })

  test('从渠道另存为共享配置后选择新引用，保存渠道前不覆盖旧配置', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockImplementation(async (_url, payload) => ({
        data: {
          success: true,
          data: { ...(payload as object), id: 9, revision: 1 },
        },
      }))
    renderWithQueries(
      <UpstreamConfigDialog
        open
        channel={customVariableChannel()}
        onOpenChange={vi.fn()}
      />
    )
    await user.click(screen.getByRole('button', { name: '另存为共享配置' }))
    await user.click(screen.getByRole('button', { name: '保存共享配置' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put.mock.calls[0][0]).toBe('/api/channel_monitor/variable_groups')
    expect(put.mock.calls[0][1]).toMatchObject({
      source_channel_id: 7,
      variable_requests: [{ id: 'login' }],
    })
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: '保存共享配置' })
      ).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(2))
    expect(put.mock.calls[1][1]).toMatchObject({
      custom_config: { variable_group_id: 9, variable_requests: [] },
    })
  })

  test('共享配置获取变量时修改地址会丢弃响应，避免覆盖手填值', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    let resolve: (response: unknown) => void = () => undefined
    vi.spyOn(api, 'post').mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    const warning = vi.spyOn(toast, 'warning')
    const onOpenChange = vi.fn()
    renderWithQueries(
      <ChannelMonitorVariableGroupsDialog
        initialGroup={sharedGroup()}
        onOpenChange={onOpenChange}
      />
    )
    await user.type(screen.getByLabelText('当前值'), 'manual-token')
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: '保存共享配置' })
      ).toBeDisabled()
    )
    await user.keyboard('{Escape}')
    expect(onOpenChange).not.toHaveBeenCalled()
    await user.clear(screen.getByLabelText('共享请求基础地址'))
    await user.type(
      screen.getByLabelText('共享请求基础地址'),
      'https://changed.example'
    )
    await act(async () =>
      resolve({
        data: {
          success: true,
          data: {
            request_id: 'login',
            variables: [{ name: 'token', value: 'stale-token' }],
          },
        },
      })
    )
    await waitFor(() =>
      expect(warning).toHaveBeenCalledWith('独立请求配置已修改，请重新获取变量')
    )
    expect(screen.getByLabelText('当前值')).toHaveValue('manual-token')
  })

  test('共享请求提取成功后回填值，保存时才提交到共享配置', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: {
          request_id: 'login',
          variables: [{ name: 'token', value: 'new-shared-token' }],
        },
      },
    })
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: sharedGroup() } })
    renderWithQueries(
      <ChannelMonitorVariableGroupsDialog
        initialGroup={sharedGroup()}
        onOpenChange={vi.fn()}
      />
    )
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(screen.getByLabelText('当前值')).toHaveValue('new-shared-token')
    )
    expect(put).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '保存共享配置' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      id: 3,
      variable_requests: [
        { variables: [{ name: 'token', value: 'new-shared-token' }] },
      ],
    })
  })

  test('加载失败可重试，删除被引用的共享配置失败时保留列表', async () => {
    const user = userEvent.setup()
    const get = vi
      .spyOn(api, 'get')
      .mockRejectedValueOnce(new Error('离线'))
      .mockResolvedValue({ data: { success: true, data: [sharedGroup()] } })
    const remove = vi.spyOn(api, 'delete').mockResolvedValue({
      data: { success: false, message: '共享配置仍被渠道引用' },
    })
    const error = vi.spyOn(toast, 'error')
    renderWithQueries(
      <ChannelMonitorVariableGroupsDialog onOpenChange={vi.fn()} />
    )
    await user.click(await screen.findByRole('button', { name: '重试' }))
    const group = await screen.findByRole('region', { name: '共用登录' })
    expect(get).toHaveBeenCalledTimes(2)
    await user.click(within(group).getByRole('button', { name: '删除' }))
    const confirm = screen.getByRole('alertdialog')
    await user.click(within(confirm).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(remove).toHaveBeenCalled())
    await waitFor(() =>
      expect(error).toHaveBeenCalledWith('共享配置仍被渠道引用')
    )
    expect(group).toBeInTheDocument()
  })
})
