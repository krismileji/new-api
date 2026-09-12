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

import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

afterEach(() => {
  toast.dismiss()
})

function renderCustomVariableDialog(channel = customVariableChannel()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const rendered = render(
    <QueryClientProvider client={queryClient}>
      <UpstreamConfigDialog
        channel={channel}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
  return { ...rendered, queryClient }
}

describe('独立请求与变量交互', () => {
  test('多个请求卡片分别保存策略，并保留折叠前填写的多个变量', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderCustomVariableDialog()
    await user.type(screen.getByLabelText('当前值'), 'first-token')
    await user.click(screen.getByRole('button', { name: '添加独立请求' }))
    const second = screen.getByRole('group', { name: '独立请求 请求 2' })
    await user.type(within(second).getByLabelText('变量名'), 'balance_token')
    await user.type(
      within(second).getByLabelText('JSON 取值路径'),
      'data.access_token'
    )
    await user.type(within(second).getByLabelText('当前值'), 'second-token')
    await user.click(
      within(second).getByRole('button', { name: '每次更新前获取' })
    )
    await user.click(within(second).getByRole('button', { name: '添加变量' }))
    await user.type(within(second).getAllByLabelText('变量名')[1], 'account_id')
    await user.type(
      within(second).getAllByLabelText('JSON 取值路径')[1],
      'data.id'
    )
    await user.type(within(second).getAllByLabelText('当前值')[1], '42')
    await user.click(
      screen.getByRole('button', { name: '配置请求 登录获取凭据' })
    )
    expect(
      screen.getByRole('button', { name: '配置请求 请求 2' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByLabelText('当前值')).toHaveValue('first-token')
    await user.click(screen.getByRole('button', { name: '配置请求 请求 2' }))
    expect(screen.getAllByLabelText('当前值')[1]).toHaveValue('42')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: {
        variable_requests: [
          {
            refresh_policy: 'on_failure',
            variables: [{ name: 'token', value: 'first-token' }],
          },
          {
            refresh_policy: 'always',
            variables: [
              { name: 'balance_token', value: 'second-token' },
              { name: 'account_id', value: '42' },
            ],
          },
        ],
      },
    })
  })

  test('一次回填请求的全部映射，不改变其他请求的变量', async () => {
    const user = userEvent.setup()
    const channel = customVariableChannel()
    const requests = channel.upstream?.custom_config?.variable_requests
    if (!requests) throw new Error('测试渠道缺少独立请求')
    requests[0].variables[0].value = 'keep-token'
    requests.push({
      ...requests[0],
      id: 'balance-login',
      name: '余额认证',
      variables: [
        {
          name: 'balance_token',
          value_path: 'data.token',
          value: '',
          has_value: false,
        },
        {
          name: 'account_id',
          value_path: 'data.id',
          value: '',
          has_value: false,
        },
      ],
    })
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: {
          request_id: 'balance-login',
          variables: [
            { name: 'balance_token', value: 'new-balance-token' },
            { name: 'account_id', value: '42' },
          ],
        },
      },
    })
    renderCustomVariableDialog(channel)
    await user.click(screen.getByRole('button', { name: '配置请求 余额认证' }))
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(screen.getAllByLabelText('当前值')[0]).toHaveValue(
        'new-balance-token'
      )
    )
    expect(screen.getAllByLabelText('当前值')[1]).toHaveValue('42')
    expect(post.mock.calls[0][1]).toMatchObject({ request_id: 'balance-login' })
    await user.click(
      screen.getByRole('button', { name: '配置请求 登录获取凭据' })
    )
    expect(screen.getByLabelText('当前值')).toHaveValue('keep-token')
  })

  test('插入变量菜单标明来源，并可将多个变量放进同一参数', async () => {
    const user = userEvent.setup()
    const channel = customVariableChannel()
    const request = channel.upstream?.custom_config?.variable_requests?.[0]
    if (!request) throw new Error('测试渠道缺少独立请求')
    request.variables.push({
      name: 'account_id',
      value_path: 'data.id',
      value: '42',
      has_value: false,
    })
    renderCustomVariableDialog(channel)
    const ratioFields = screen.getByRole('group', { name: '上游倍率来源' })
    await user.click(
      within(ratioFields).getByRole('button', { name: '插入变量' })
    )
    expect(
      within(screen.getByRole('menu')).getByText('登录获取凭据')
    ).toBeVisible()
    await user.click(screen.getByRole('menuitem', { name: '{{account_id}}' }))
    expect(within(ratioFields).getByLabelText('请求头 1 变量模板')).toHaveValue(
      'Bearer {{token}}{{account_id}}'
    )
  })

  test('倍率接口尚未填写时仍可先获取变量', async () => {
    const user = userEvent.setup()
    const channel = customVariableChannel()
    const ratioRequest = channel.upstream?.custom_config?.ratio.request
    expect(ratioRequest).toBeDefined()
    if (!ratioRequest) throw new Error('测试渠道缺少倍率接口配置')
    ratioRequest.path = ''
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: {
          request_id: 'login',
          variables: [{ name: 'token', value: 'draft-token' }],
        },
      },
    })
    renderCustomVariableDialog(channel)
    const variableFields = screen.getByRole('group', {
      name: '独立请求 登录获取凭据',
    })
    expect(
      within(variableFields).queryByLabelText('结果乘数')
    ).not.toBeInTheDocument()
    expect(within(variableFields).getByLabelText('JSON 取值路径')).toBeEnabled()
    await user.click(
      within(variableFields).getByRole('button', { name: '文本' })
    )
    expect(
      within(variableFields).getByLabelText('JSON 取值路径')
    ).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(screen.getByLabelText('当前值')).toHaveValue('draft-token')
    )
  })

  test('独立请求进行中更换上游地址会丢弃旧响应', async () => {
    const user = userEvent.setup()
    let resolveRequest!: (value: unknown) => void
    vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve
        })
    )
    const warningToast = vi.spyOn(toast, 'warning')
    renderCustomVariableDialog()
    await user.type(screen.getByLabelText('当前值'), 'manual-token')
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: '正在获取变量…' })
      ).toBeDisabled()
    )
    const baseUrl = screen.getByDisplayValue('https://upstream.example')
    await user.clear(baseUrl)
    await user.type(baseUrl, 'https://changed.example')
    await act(async () => {
      resolveRequest({
        data: {
          success: true,
          data: {
            request_id: 'login',
            variables: [{ name: 'token', value: 'stale-token' }],
          },
        },
      })
    })
    await waitFor(() =>
      expect(warningToast).toHaveBeenCalledWith(
        '独立请求配置已修改，请重新获取变量'
      )
    )
    expect(screen.getByLabelText('当前值')).toHaveValue('manual-token')
  })

  test('手填变量并选每次更新后可直接保存，不触发独立请求', async () => {
    const user = userEvent.setup()
    const post = vi.spyOn(api, 'post')
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderCustomVariableDialog()
    await user.type(screen.getByLabelText('当前值'), 'manual-token')
    await user.click(screen.getByRole('button', { name: '每次更新前获取' }))
    expect(
      screen.getByRole('button', { name: '每次更新前获取' })
    ).toHaveAttribute('aria-pressed', 'true')
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: {
        variable_requests: [
          { variables: [{ value: 'manual-token' }], refresh_policy: 'always' },
        ],
      },
    })
    expect(post).not.toHaveBeenCalled()
  })

  test('请求期间禁用保存，成功回填后仍需保存', async () => {
    const user = userEvent.setup()
    let resolveRequest!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve
        })
    )
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: {} } })
    renderCustomVariableDialog()
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: '正在获取变量…' })
      ).toBeDisabled()
    )
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
    expect(post.mock.calls[0][0]).toBe(
      '/api/channel_monitor/channel/7/upstream/variable/fetch'
    )
    await act(async () => {
      resolveRequest({
        data: {
          success: true,
          data: {
            request_id: 'login',
            variables: [{ name: 'token', value: 'fetched-token' }],
          },
        },
      })
    })
    await waitFor(() =>
      expect(screen.getByLabelText('当前值')).toHaveValue('fetched-token')
    )
    expect(put).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      custom_config: {
        variable_requests: [{ variables: [{ value: 'fetched-token' }] }],
      },
    })
  })

  test('独立请求失败显示错误并保留手填值', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'post').mockRejectedValue(new Error('独立请求失败'))
    const errorToast = vi.spyOn(toast, 'error')
    renderCustomVariableDialog()
    await user.type(screen.getByLabelText('当前值'), 'manual-token')
    await user.click(screen.getByRole('button', { name: '请求并回填变量' }))
    await waitFor(() => expect(errorToast).toHaveBeenCalledWith('独立请求失败'))
    expect(screen.getByLabelText('当前值')).toHaveValue('manual-token')
    expect(screen.getByRole('button', { name: '请求并回填变量' })).toBeEnabled()
  })

  test('参数值可切换为变量模板并校验引用名称', async () => {
    const user = userEvent.setup()
    const put = vi.spyOn(api, 'put')
    renderCustomVariableDialog()
    const ratioFields = screen.getByRole('group', { name: '上游倍率来源' })
    const template = within(ratioFields).getByLabelText('请求头 1 变量模板')
    expect(template).toHaveValue('Bearer {{token}}')
    expect(template).toHaveAttribute('type', 'text')
    expect(
      within(ratioFields).getByRole('switch', { name: '请求头 1 使用敏感值' })
    ).toHaveAttribute('aria-disabled', 'true')
    await user.clear(template)
    await user.paste('{{unknown}}')
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(
      await screen.findByText('请使用已配置的变量，例如 {{token}}')
    ).toBeVisible()
    expect(put).not.toHaveBeenCalled()
    await user.click(
      within(ratioFields).getByRole('switch', { name: '请求头 1 使用变量模板' })
    )
    expect(within(ratioFields).getByLabelText('请求头 1 值')).toHaveAttribute(
      'type',
      'password'
    )
  })
})
