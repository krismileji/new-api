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
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { ChannelMonitorSmartSchedulePrimaryManagement } from '../../types'
import { ChannelMonitorSmartSchedulePrimaryManagementDialog } from '../channel-monitor-smart-schedule-primary-management-dialog'

const originalAdapter = api.defaults.adapter
let client: QueryClient

afterEach(() => {
  cleanup()
  api.defaults.adapter = originalAdapter
  client?.clear()
})
Object.defineProperty(Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})

function management(): ChannelMonitorSmartSchedulePrimaryManagement {
  return {
    channel_rules: [
      {
        channel_id: 11,
        channel_name: '固定渠道甲',
        group: 'vip',
        manual_primary_until: 2 ** 52,
        allow_stability_degrade: true,
        updated_at: 1_800_000_000,
        routes: [
          {
            group: 'vip',
            model: 'model-b',
            status: 'channel_rule_conflict',
            current_primary_channel_id: 12,
            current_primary_channel_name: '固定渠道乙',
          },
        ],
      },
    ],
    route_primaries: [
      {
        channel_id: 21,
        channel_name: '单模型渠道',
        group: 'default',
        model: 'model-a',
        manual_primary_until: 2 ** 52,
        allow_stability_degrade: false,
      },
    ],
  }
}

function mount(
  data = management(),
  mutate?: (
    config: AxiosRequestConfig,
    body: Record<string, unknown>
  ) => unknown
) {
  const requests: Array<{ url?: string; body: Record<string, unknown> }> = []
  api.defaults.adapter = async (config) => {
    const body =
      typeof config.data === 'string'
        ? (JSON.parse(config.data) as Record<string, unknown>)
        : {}
    if (config.method === 'put') requests.push({ url: config.url, body })
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data:
        config.method === 'get'
          ? { success: true, data: structuredClone(data) }
          : (mutate?.(config, body) ?? { success: true, data: null }),
    }
  }
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const onActionComplete = vi.fn(async () => {})
  const onOpenChange = vi.fn()
  const view = render(
    <QueryClientProvider client={client}>
      <ChannelMonitorSmartSchedulePrimaryManagementDialog
        open
        onOpenChange={onOpenChange}
        onActionComplete={onActionComplete}
      />
    </QueryClientProvider>
  )
  return { ...view, requests, onActionComplete, onOpenChange }
}

test('长列表保留独立滚动和安全高度，分类切换不会展开所有编辑表单', async () => {
  mount()
  await screen.findByText('固定渠道甲')
  const dialog = screen.getByRole('dialog')
  expect(dialog).not.toHaveClass('overflow-hidden')
  expect(dialog).toHaveClass(
    'max-h-[var(--dialog-available-height,calc(100dvh-2rem))]',
    'overflow-y-auto'
  )
  expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
  expect(screen.getAllByRole('rowgroup')[1]).toHaveClass('max-sm:[&>tr]:h-auto')
  expect(
    screen.queryByText('被渠道固定覆盖：固定渠道乙')
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: /单模型固定/ }))
  expect(screen.getByText('单模型渠道')).toBeVisible()
  await waitFor(() =>
    expect(screen.queryByText('固定渠道甲')).not.toBeInTheDocument()
  )
  expect(screen.getByText('永久')).toBeVisible()
  const close = screen.getByRole('button', { name: '关闭' })
  close.focus()
  expect(close).toHaveFocus()
})

test('搜索路由后按需展开生效原因，编辑仅改变降级设置时保留到期时间', async () => {
  const { requests, onActionComplete } = mount()
  const user = userEvent.setup()
  await screen.findByText('固定渠道甲')
  await user.type(
    screen.getByRole('searchbox', { name: '搜索固定项' }),
    'model-b'
  )
  await user.click(screen.getByRole('button', { name: '0/1 已生效 · 详情' }))
  expect(screen.getByText('被渠道固定覆盖：固定渠道乙')).toBeVisible()
  await user.click(screen.getByRole('button', { name: '编辑 固定渠道甲' }))
  expect(screen.getByRole('button', { name: '保存更改' })).toBeDisabled()
  await user.click(screen.getByRole('switch', { name: '允许稳定性降级' }))
  await user.click(screen.getByRole('button', { name: '保存更改' }))
  await waitFor(() =>
    expect(requests).toEqual([
      {
        url: '/api/channel_monitor/channel/11/schedule/primary-rule',
        body: {
          group: 'vip',
          duration_minutes: null,
          preserve_until: true,
          allow_stability_degrade: false,
        },
      },
    ])
  )
  await waitFor(() => expect(onActionComplete).toHaveBeenCalledOnce())
  expect(
    await screen.findByRole('searchbox', { name: '搜索固定项' })
  ).toHaveValue('model-b')
  expect(screen.getByRole('button', { name: '编辑 固定渠道甲' })).toHaveFocus()
})

test('单模型支持自定义时长校验、保护确认与失败保留编辑内容', async () => {
  let attempts = 0
  const { requests } = mount(management(), (_config, body) => {
    attempts++
    if (!body.confirm_stability_override) {
      return {
        success: false,
        code: 'smart_schedule_route_stability_confirmation_required',
        message: '需要解除稳定性保护',
      }
    }
    return attempts === 2
      ? { success: false, message: '保存失败，请重试' }
      : { success: true, data: null }
  })
  const user = userEvent.setup()
  await screen.findByText('固定渠道甲')
  await user.click(screen.getByRole('tab', { name: /单模型固定/ }))
  await user.click(
    screen.getByRole('button', { name: '编辑 单模型渠道 / default / model-a' })
  )
  await user.click(screen.getByRole('button', { name: '自定义' }))
  fireEvent.change(
    screen.getByRole('spinbutton', { name: '固定时长（分钟）' }),
    { target: { value: '0' } }
  )
  expect(screen.getByRole('button', { name: '保存更改' })).toBeDisabled()
  fireEvent.change(
    screen.getByRole('spinbutton', { name: '固定时长（分钟）' }),
    { target: { value: '120' } }
  )
  await user.click(screen.getByRole('button', { name: '保存更改' }))
  expect(await screen.findByRole('alertdialog')).toHaveTextContent(
    '解除稳定性保护并固定'
  )
  await user.click(screen.getByRole('button', { name: '解除保护并保存' }))
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(screen.getByRole('spinbutton')).toHaveValue(120)
  expect(requests[1].body).toMatchObject({
    duration_minutes: 120,
    confirm_stability_override: true,
  })
  expect(requests[0].body).not.toHaveProperty('preserve_until')
})

test('批量解除仅发送所选项，部分失败保留可重试项并刷新成功结果', async () => {
  const data = management()
  data.channel_rules.push({
    ...data.channel_rules[0],
    channel_id: 12,
    channel_name: '固定渠道乙',
  })
  const { requests, onActionComplete } = mount(data, (config) => {
    if (config.url?.includes('/12/')) {
      return { success: false, message: '渠道暂不可用' }
    }
    data.channel_rules = data.channel_rules.filter(
      (rule) => rule.channel_id !== 11
    )
    return { success: true, data: null }
  })
  const user = userEvent.setup()
  await screen.findByText('固定渠道甲')
  await user.click(screen.getByRole('checkbox', { name: '选择当前筛选结果' }))
  await user.click(screen.getByRole('button', { name: '解除所选固定' }))
  const confirmation = screen.getByRole('alertdialog')
  expect(confirmation).toHaveTextContent('解除 2 项固定')
  expect(confirmation).toHaveTextContent('不会删除单模型固定')
  expect(requests).toHaveLength(0)
  await user.click(
    within(confirmation).getByRole('button', { name: '确认解除' })
  )
  expect(
    await screen.findByRole('button', { name: '重试失败项' })
  ).toBeEnabled()
  expect(screen.getByRole('alert')).toHaveTextContent('固定渠道乙')
  expect(screen.getByRole('alert')).toHaveTextContent('渠道暂不可用')
  expect(requests.map((request) => request.url)).toEqual([
    '/api/channel_monitor/channel/11/schedule/primary-rule',
    '/api/channel_monitor/channel/12/schedule/primary-rule',
  ])
  await waitFor(() => expect(onActionComplete).toHaveBeenCalledOnce())
  expect(screen.queryByText('固定渠道甲')).not.toBeInTheDocument()
})

test('筛选变化清除选择，未保存修改关闭时需确认且取消后保留输入', async () => {
  const { onOpenChange } = mount()
  const user = userEvent.setup()
  await screen.findByText('固定渠道甲')
  await user.click(screen.getByRole('checkbox', { name: '选择 固定渠道甲' }))
  expect(screen.getByRole('button', { name: '解除所选固定' })).toBeEnabled()
  await user.type(screen.getByRole('searchbox'), 'model-b')
  expect(
    screen.queryByRole('button', { name: '解除所选固定' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '编辑 固定渠道甲' }))
  await user.click(screen.getByRole('button', { name: '6 小时' }))
  await user.click(screen.getByRole('button', { name: 'Close' }))
  expect(screen.getByRole('alertdialog')).toHaveTextContent('放弃未保存的修改')
  await user.click(screen.getByRole('button', { name: '继续编辑' }))
  expect(screen.getByRole('button', { name: '6 小时' })).toHaveAttribute(
    'aria-pressed',
    'true'
  )
  expect(onOpenChange).not.toHaveBeenCalled()
})

test('无固定项与搜索无结果时显示可理解的空状态', async () => {
  const { unmount } = mount({ channel_rules: [], route_primaries: [] })
  expect(await screen.findByText('暂无固定项')).toBeVisible()
  unmount()
  client.clear()
  mount()
  await screen.findByText('固定渠道甲')
  await userEvent.type(screen.getByRole('searchbox'), '不存在的渠道')
  expect(screen.getByText('没有匹配的固定项')).toBeVisible()
})

test('分组和生效状态筛选只保留匹配规则，切换筛选清除批量选择', async () => {
  const data = management()
  data.channel_rules.push({
    ...data.channel_rules[0],
    channel_id: 12,
    channel_name: '已生效渠道',
    group: 'default',
    routes: [
      {
        group: 'default',
        model: 'model-c',
        status: 'active',
        current_primary_channel_id: 12,
        current_primary_channel_name: '已生效渠道',
      },
    ],
  })
  mount(data)
  const user = userEvent.setup()
  await screen.findByText('固定渠道甲')
  await user.click(screen.getByRole('checkbox', { name: '选择当前筛选结果' }))
  await user.click(screen.getByRole('combobox', { name: '按分组筛选' }))
  await user.click(screen.getByRole('option', { name: 'default' }))
  expect(screen.queryByText('固定渠道甲')).not.toBeInTheDocument()
  expect(screen.getByText('已生效渠道')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: '解除所选固定' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('combobox', { name: '按生效情况筛选' }))
  await user.click(screen.getByRole('option', { name: '未生效' }))
  expect(screen.getByText('没有匹配的固定项')).toBeVisible()
})
