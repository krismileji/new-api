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
import { emptyUpstreamAutomation } from '../../lib/automation'
import {
  createChannelMonitorCustomAction,
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from '../../lib/custom-upstream'
import type { ChannelMonitorUpstreamRequest } from '../../types'
import { UpstreamAutomationEditor } from '../upstream-automation-editor'
import { UpstreamConfigDialog } from '../upstream-config-dialog'

afterEach(() => {
  toast.dismiss()
})

test.each(['渠道配置', '上游自动任务'])(
  '%s 中的小数乘数完整显示，编辑保存并重新打开后精度不变',
  async (editor) => {
    const user = userEvent.setup()
    const channel = customVariableChannel()
    const config = createChannelMonitorCustomFormConfig(undefined)
    config.ratio.source = 'http'
    config.balance.source = 'http'
    config.ratio.result.multiplier = 2e-7
    config.balance.result.multiplier = 1.23456789e-9
    config.actions =
      editor === '上游自动任务' ? [createChannelMonitorCustomAction()] : []
    const customConfig = createChannelMonitorCustomRequestConfig(config)
    if (!channel.upstream) throw new Error('测试渠道缺少上游配置')
    channel.upstream.custom_config = customConfig
    const task = {
      ...emptyUpstreamAutomation(),
      id: 'decimal-multiplier',
      name: '小数乘数任务',
      base_url: 'https://upstream.example',
      custom_config: customConfig,
    }
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: [] },
    })
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true, data: task },
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        {editor === '渠道配置' ? (
          <UpstreamConfigDialog
            channel={channel}
            open
            onOpenChange={() => undefined}
          />
        ) : (
          <UpstreamAutomationEditor
            task={task}
            channels={[]}
            onSaved={() => undefined}
            onCancel={() => undefined}
          />
        )}
      </QueryClientProvider>
    )
    const ratioInput = within(
      screen.getByRole('group', { name: '上游倍率来源' })
    ).getByLabelText('结果乘数')
    const balanceInput = within(
      screen.getByRole('group', { name: '上游余额来源' })
    ).getByLabelText('结果乘数')
    expect(ratioInput).toHaveProperty('value', '0.0000002')
    expect(balanceInput).toHaveProperty('value', '0.00000000123456789')

    await user.clear(ratioInput)
    await user.type(ratioInput, '0.0000003')
    await user.click(
      screen.getByRole('button', {
        name: editor === '渠道配置' ? '保存' : '保存任务',
      })
    )
    await waitFor(() => expect(put).toHaveBeenCalledOnce())
    const payload = put.mock.calls[0][1] as ChannelMonitorUpstreamRequest
    expect(payload.custom_config).toMatchObject({
      ratio: { result: { multiplier: 3e-7 } },
      balance: { result: { multiplier: 1.23456789e-9 } },
    })
    if (!payload.custom_config) throw new Error('保存结果缺少自定义上游配置')

    view.unmount()
    channel.upstream.custom_config = payload.custom_config
    render(
      <QueryClientProvider client={client}>
        {editor === '渠道配置' ? (
          <UpstreamConfigDialog
            channel={channel}
            open
            onOpenChange={() => undefined}
          />
        ) : (
          <UpstreamAutomationEditor
            task={{ ...task, custom_config: payload.custom_config }}
            channels={[]}
            onSaved={() => undefined}
            onCancel={() => undefined}
          />
        )}
      </QueryClientProvider>
    )
    expect(
      within(
        screen.getByRole('group', { name: '上游倍率来源' })
      ).getByLabelText('结果乘数')
    ).toHaveProperty('value', '0.0000003')
    expect(
      within(
        screen.getByRole('group', { name: '上游余额来源' })
      ).getByLabelText('结果乘数')
    ).toHaveProperty('value', '0.00000000123456789')
    client.clear()
  }
)
