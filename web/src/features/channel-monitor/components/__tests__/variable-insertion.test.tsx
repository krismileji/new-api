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
import { useForm } from 'react-hook-form'
import { describe, expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'
import { api } from '@/lib/api'

import type { ChannelMonitorVariableGroup } from '../../api-variable-groups'
import { customVariableFormValues } from '../../lib/__tests__/custom-variable.fixture'
import { createChannelMonitorCustomRequestConfig } from '../../lib/custom-upstream'
import type { UpstreamConfigFormValues } from '../../lib/schema'
import { ChannelMonitorCustomKeyValueEditor } from '../channel-monitor-custom-key-value-editor'

function sharedVariableGroup(
  id: number,
  name: string
): ChannelMonitorVariableGroup {
  return {
    id,
    name,
    base_url: `https://upstream-${id}.example`,
    proxy: '',
    request_timeout: 30,
    revision: 1,
    variable_requests:
      createChannelMonitorCustomRequestConfig(
        customVariableFormValues().customConfig
      ).variable_requests ?? [],
  }
}

function VariableParameterEditor(props: {
  values: UpstreamConfigFormValues
  onSubmit: (values: UpstreamConfigFormValues) => void
}) {
  const form = useForm<UpstreamConfigFormValues>({
    defaultValues: props.values,
  })
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(props.onSubmit)}>
        <ChannelMonitorCustomKeyValueEditor
          form={form}
          name='customConfig.ratio.request.headers'
          label='请求头'
          allowVariables
        />
        <button type='submit'>保存参数</button>
      </form>
    </Form>
  )
}

function renderParameterEditor(values = customVariableFormValues()) {
  const onSubmit = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <VariableParameterEditor values={values} onSubmit={onSubmit} />
    </QueryClientProvider>
  )
  return onSubmit
}

describe('渠道接口插入共享变量', () => {
  test('未选择共享配置时按来源列出同名变量，选中后绑定对应配置并填入参数', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [
          sharedVariableGroup(3, '上游 A'),
          sharedVariableGroup(8, '上游 B'),
        ],
      },
    })
    const values = customVariableFormValues()
    values.customConfig.variableRequests = []
    values.customConfig.ratio.request.headers[0].valueTemplate = 'Bearer '
    const onSubmit = renderParameterEditor(values)

    const insert = screen.getByRole('button', { name: '插入变量' })
    expect(insert).toBeEnabled()
    await user.click(insert)
    const sourceA = await screen.findByRole('group', {
      name: '上游 A · 登录获取凭据',
    })
    const sourceB = await screen.findByRole('group', {
      name: '上游 B · 登录获取凭据',
    })
    expect(
      within(sourceA).getByRole('menuitem', { name: '{{token}}' })
    ).toBeVisible()
    await user.click(
      within(sourceB).getByRole('menuitem', { name: '{{token}}' })
    )

    expect(screen.getByLabelText('请求头 1 变量模板')).toHaveValue(
      'Bearer {{token}}'
    )
    await user.click(screen.getByRole('button', { name: '保存参数' }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    expect(onSubmit.mock.calls[0][0].customConfig).toMatchObject({
      variableGroupId: 8,
      variableRequests: [],
    })
    await user.click(insert)
    expect(
      screen.queryByRole('group', { name: '上游 A · 登录获取凭据' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: '上游 B · 登录获取凭据' })
    ).toBeVisible()
  })

  test('已有共享引用重新打开后，键盘可插入该配置变量并保留值输入中的前缀', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [sharedVariableGroup(3, '上游 A')] },
    })
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 3
    values.customConfig.variableRequests = []
    values.customConfig.ratio.request.headers[0].valueTemplate = undefined
    values.customConfig.ratio.request.headers[0].value = 'Bearer '
    renderParameterEditor(values)

    const insert = screen.getByRole('button', { name: '插入变量' })
    insert.focus()
    await user.keyboard('{Enter}')
    const variable = await screen.findByRole('menuitem', { name: '{{token}}' })
    variable.focus()
    await user.keyboard('{Enter}')
    expect(screen.getByLabelText('请求头 1 变量模板')).toHaveValue(
      'Bearer {{token}}'
    )
    expect(
      screen.getByRole('switch', { name: '请求头 1 使用变量模板' })
    ).toBeChecked()
  })

  test('列表加载时显示状态，失败后可在插入菜单里重试', async () => {
    const user = userEvent.setup()
    let rejectRequest: (error: Error) => void = () => undefined
    vi.spyOn(api, 'get')
      .mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            rejectRequest = reject
          })
      )
      .mockResolvedValue({
        data: { success: true, data: [sharedVariableGroup(3, '上游 A')] },
      })
    const values = customVariableFormValues()
    values.customConfig.variableRequests = []
    renderParameterEditor(values)
    await user.click(screen.getByRole('button', { name: '插入变量' }))
    expect(screen.getByText('正在加载共享变量…')).toBeVisible()
    await act(async () => {
      rejectRequest(new Error('离线'))
    })
    await user.click(
      await screen.findByRole('menuitem', { name: '加载失败，点击重试' })
    )
    expect(
      await screen.findByRole('menuitem', { name: '{{token}}' })
    ).toBeVisible()
  })

  test('引用不存在时说明原因，不列出其他配置的同名变量', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [sharedVariableGroup(3, '上游 A')] },
    })
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 99
    values.customConfig.variableRequests = []
    renderParameterEditor(values)
    await user.click(screen.getByRole('button', { name: '插入变量' }))
    expect(
      await screen.findByText('引用的共享配置不存在，请重新选择')
    ).toBeVisible()
    expect(
      screen.queryByRole('menuitem', { name: '{{token}}' })
    ).not.toBeInTheDocument()
  })

  test('还没有共享配置时仍能打开菜单并看到创建指引', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const values = customVariableFormValues()
    values.customConfig.variableRequests = []
    renderParameterEditor(values)

    await user.click(screen.getByRole('button', { name: '插入变量' }))
    expect(
      await screen.findByText(
        '暂无可用变量，请先在「共享请求与变量」中创建配置'
      )
    ).toBeVisible()
  })

  test('所选配置没有有效变量时提示补充变量，不混入其他配置', async () => {
    const user = userEvent.setup()
    const emptyGroup = sharedVariableGroup(8, '上游 B')
    emptyGroup.variable_requests = []
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: [sharedVariableGroup(3, '上游 A'), emptyGroup],
      },
    })
    const values = customVariableFormValues()
    values.customConfig.variableGroupId = 8
    values.customConfig.variableRequests = []
    renderParameterEditor(values)

    await user.click(screen.getByRole('button', { name: '插入变量' }))
    expect(
      await screen.findByText('当前配置没有可用变量，请先添加有效的变量名')
    ).toBeVisible()
    expect(
      screen.queryByRole('menuitem', { name: '{{token}}' })
    ).not.toBeInTheDocument()
  })
})
