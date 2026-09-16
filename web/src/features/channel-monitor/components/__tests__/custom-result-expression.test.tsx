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
import { toast } from 'sonner'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { customVariableChannel } from '../../lib/__tests__/custom-variable.fixture'
import type { ChannelMonitorUpstreamRequest } from '../../types'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

afterEach(() => {
  toast.dismiss()
})

function renderCustomResultDialog(channel = customVariableChannel()) {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: [] } })
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
  return rendered
}

test('复用接口时分别保存倍率和余额表达式与乘数，重新打开仍能编辑', async () => {
  const user = userEvent.setup()
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: {} } })
  const rendered = renderCustomResultDialog()
  const ratioFields = within(
    screen.getByRole('group', { name: '上游倍率来源' })
  )
  const balanceFields = within(
    screen.getByRole('group', { name: '上游余额来源' })
  )
  const ratioPath = ratioFields.getByLabelText('JSON 取值路径 / 表达式')
  const balancePath = balanceFields.getByLabelText('JSON 取值路径 / 表达式')
  expect(ratioPath).toHaveValue('data.ratio')
  expect(balancePath).toHaveValue('data.balance')
  expect(balancePath).toHaveAccessibleDescription(/计算后再乘结果乘数/)

  await user.clear(ratioPath)
  await user.type(ratioPath, '=json("data.price") / json("data.base")')
  await user.clear(ratioFields.getByLabelText('结果乘数'))
  await user.type(ratioFields.getByLabelText('结果乘数'), '2')
  await user.clear(balancePath)
  await user.type(balancePath, '=json("data.total") - json("data.used")')
  await user.clear(balanceFields.getByLabelText('结果乘数'))
  await user.type(balanceFields.getByLabelText('结果乘数'), '0.01')
  await user.click(screen.getByRole('switch', { name: '余额复用倍率接口' }))
  expect(balancePath).toBeEnabled()
  await user.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  const request = put.mock.calls[0][1] as ChannelMonitorUpstreamRequest
  expect(request.custom_config).toMatchObject({
    balance_reuse_ratio_request: true,
    ratio: {
      result: {
        response_type: 'json',
        value_path: '=json("data.price") / json("data.base")',
        multiplier: 2,
      },
    },
    balance: {
      request: undefined,
      result: {
        response_type: 'json',
        value_path: '=json("data.total") - json("data.used")',
        multiplier: 0.01,
      },
    },
  })

  rendered.unmount()
  const channel = customVariableChannel()
  if (!channel.upstream) throw new Error('测试渠道缺少上游配置')
  channel.upstream.custom_config = request.custom_config
  renderCustomResultDialog(channel)
  const reopened = within(screen.getByRole('group', { name: '上游余额来源' }))
  expect(reopened.getByLabelText('JSON 取值路径 / 表达式')).toHaveValue(
    '=json("data.total") - json("data.used")'
  )
  expect(reopened.getByLabelText('结果乘数')).toHaveValue(0.01)
})

test('切换文本响应会禁用表达式并提示直接取值，切回 JSON 保留已填写的公式', async () => {
  const user = userEvent.setup()
  renderCustomResultDialog()
  const fields = within(screen.getByRole('group', { name: '上游余额来源' }))
  const input = fields.getByLabelText('JSON 取值路径 / 表达式')
  const expression = '=(json("data.total") - json("data.used")) / 100'
  await user.clear(input)
  await user.type(input, expression)
  await user.click(fields.getByRole('button', { name: '文本' }))
  expect(input).toBeDisabled()
  expect(input).toHaveAccessibleDescription(
    '文本响应直接取数字，再乘结果乘数。'
  )
  expect(fields.getByLabelText('结果乘数')).toBeEnabled()
  await user.click(fields.getByRole('button', { name: 'JSON' }))
  expect(input).toBeEnabled()
  expect(input).toHaveValue(expression)
  expect(input).toHaveAccessibleDescription(/json\("路径"\)/)
})

test.each([
  { name: '空输入', value: '', message: '请输入 JSON 取值路径或表达式' },
  {
    name: '超过长度限制',
    value: `=${'1+'.repeat(256)}1`,
    message: 'JSON 取值路径或表达式不能超过 512 个字符',
  },
])('路径或表达式$name时阻止保存并提示', async ({ value, message }) => {
  const user = userEvent.setup()
  const put = vi.spyOn(api, 'put')
  renderCustomResultDialog()
  const fields = within(screen.getByRole('group', { name: '上游余额来源' }))
  const input = fields.getByLabelText('JSON 取值路径 / 表达式')
  await user.clear(input)
  if (value) {
    await user.click(input)
    await user.paste(value)
  }
  expect(input).toHaveValue(value)
  await user.click(screen.getByRole('button', { name: '保存' }))
  expect(await fields.findByText(message)).toBeVisible()
  expect(input).toHaveAttribute('aria-invalid', 'true')
  expect(put).not.toHaveBeenCalled()
})
