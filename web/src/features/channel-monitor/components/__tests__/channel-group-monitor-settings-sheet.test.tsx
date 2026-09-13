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
import { afterEach, expect, test } from 'vitest'

import type { ChannelGroupMonitorSettingsResponse } from '@/features/group-monitor/types'
import { api } from '@/lib/api'

import { ChannelGroupMonitorSettingsSheet } from '../channel-group-monitor-settings-sheet'

type SaveRequest = {
  categories: string[]
  groups: ChannelGroupMonitorSettingsResponse['settings']['groups']
  revision: number
}

const originalAdapter = api.defaults.adapter
let testQueryClient: QueryClient | undefined

afterEach(() => {
  api.defaults.adapter = originalAdapter
  testQueryClient?.clear()
  testQueryClient = undefined
})

function renderSettings(
  groups: ChannelGroupMonitorSettingsResponse['settings']['groups'] = [],
  categories?: string[],
  candidateModelsByGroup: Record<string, string[]> = {
    default: ['gpt-4.1'],
    vip: ['gpt-4.1'],
  }
) {
  const data: ChannelGroupMonitorSettingsResponse = {
    settings: {
      enabled: false,
      groups,
      categories,
      interval_seconds: 60,
      display_value: 60,
      display_unit: 'minute',
      next_run_at: 0,
      manual_request_id: '',
      manual_requested_at: 0,
      revision: 3,
      running_trigger: '',
      running_run_id: '',
      running_started_at: 0,
      updated_at: 0,
    },
    candidate_models_by_group: candidateModelsByGroup,
  }
  const requests: SaveRequest[] = []
  const network = { fail: false }
  testQueryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  testQueryClient.setQueryData(['pricing', 'group-monitor'], {
    data: { items: [] },
  })
  api.defaults.adapter = async (config) => {
    const body = JSON.parse(String(config.data)) as SaveRequest
    requests.push(body)
    if (network.fail) throw new Error('保存失败')
    return {
      data: {
        success: true,
        data: { ...data.settings, ...body, revision: body.revision + 1 },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  render(
    <QueryClientProvider client={testQueryClient}>
      <ChannelGroupMonitorSettingsSheet
        data={data}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
  return { requests, network, queryClient: testQueryClient }
}

test('首次配置先创建并命名分类，再在该分类下添加分组', async () => {
  const user = userEvent.setup()
  const { requests } = renderSettings()
  expect(screen.getByText('尚未创建分类')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: /中添加分组/ })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '添加分类' }))
  expect(
    screen.getByRole('button', { name: '在 新分类 中添加分组' })
  ).toBeDisabled()
  await user.type(
    screen.getByRole('textbox', { name: '第 1 个分类名称' }),
    '通用模型'
  )
  await user.click(
    screen.getByRole('button', { name: '在 通用模型 中添加分组' })
  )
  expect(
    within(screen.getByRole('region', { name: '分类 通用模型' })).getByRole(
      'combobox',
      { name: 'default的探测模型' }
    )
  ).toHaveTextContent('gpt-4.1')
  expect(screen.getByRole('button', { name: '立即探测' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() =>
    expect(requests[0]).toMatchObject({
      categories: ['通用模型'],
      groups: [
        {
          group_name: 'default',
          probe_model: 'gpt-4.1',
          display_initial: '',
          category: '通用模型',
        },
      ],
    })
  )
})

test('旧配置按分类恢复，分类重命名和排序会同步保存组内顺序', async () => {
  const user = userEvent.setup()
  const { requests, queryClient } = renderSettings([
    { group_name: 'default', probe_model: 'gpt-4.1', category: '通用模型' },
    { group_name: 'vip', probe_model: 'gpt-4.1', category: '编程模型' },
    {
      group_name: 'coding-basic',
      probe_model: 'gpt-4.1',
      category: '编程模型',
    },
  ])
  const category = screen.getByRole('textbox', { name: '第 2 个分类名称' })
  await user.clear(category)
  await user.type(category, '  编程服务  ')
  await user.click(screen.getByRole('button', { name: '上移分类 编程服务' }))
  await user.click(screen.getByRole('button', { name: '上移 coding-basic' }))
  expect(
    screen
      .getAllByRole('textbox', { name: /个分类名称/ })
      .map((input) => (input as HTMLInputElement).value.trim())
  ).toEqual(['编程服务', '通用模型'])
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() =>
    expect(
      requests[0]?.groups.map((group) => [group.group_name, group.category])
    ).toEqual([
      ['coding-basic', '编程服务'],
      ['vip', '编程服务'],
      ['default', '通用模型'],
    ])
  )
  expect(requests[0].categories).toEqual(['编程服务', '通用模型'])
  await waitFor(() =>
    expect(
      queryClient.getQueryState(['pricing', 'group-monitor'])?.isInvalidated
    ).toBe(true)
  )
})

test('分组可以移动到另一分类，清空后的分类可以删除', async () => {
  const user = userEvent.setup()
  const { requests } = renderSettings(
    [{ group_name: 'default', probe_model: 'gpt-4.1', category: '通用模型' }],
    ['通用模型', '编程模型']
  )
  expect(
    screen.getByRole('button', { name: '删除分类 通用模型' })
  ).toBeDisabled()
  await user.click(
    screen.getByRole('combobox', { name: '移动 default 到分类' })
  )
  await user.click(screen.getByRole('option', { name: '编程模型' }))
  expect(
    within(screen.getByRole('region', { name: '分类 编程模型' })).getByRole(
      'combobox',
      { name: 'default的探测模型' }
    )
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: '删除分类 通用模型' }))
  expect(
    screen.queryByRole('region', { name: '分类 通用模型' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() =>
    expect(requests[0]).toMatchObject({
      categories: ['编程模型'],
      groups: [{ group_name: 'default', category: '编程模型' }],
    })
  )
})

test('空分类可以独立保存，重新加载后仍保留名称', async () => {
  const user = userEvent.setup()
  const { requests } = renderSettings()
  await user.click(screen.getByRole('button', { name: '添加分类' }))
  await user.type(
    screen.getByRole('textbox', { name: '第 1 个分类名称' }),
    '预留分类'
  )
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() =>
    expect(requests[0]).toMatchObject({ categories: ['预留分类'], groups: [] })
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: '保存配置' })).toBeEnabled()
  )
  expect(screen.getByRole('textbox', { name: '第 1 个分类名称' })).toHaveValue(
    '预留分类'
  )
  expect(screen.getByRole('button', { name: '立即探测' })).toBeDisabled()
})

test.each([
  { name: '', error: '请填写分类名称' },
  { name: '类'.repeat(65), error: '分类名称不能超过 64 个字符' },
  { name: ' 编程模型 ', error: '分类名称不能重复' },
])('分类名称无效时阻止保存：$error', async ({ name, error }) => {
  const user = userEvent.setup()
  const { requests } = renderSettings([], ['通用模型', '编程模型'])
  const category = screen.getByRole('textbox', { name: '第 1 个分类名称' })
  await user.clear(category)
  if (name) await user.type(category, name)
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  expect(await screen.findByText(error)).toBeVisible()
  expect(requests).toEqual([])
})

test('保存失败后保留分类修改和分组，重试仍使用原配置版本', async () => {
  const user = userEvent.setup()
  const { requests, network } = renderSettings([
    { group_name: 'default', probe_model: 'gpt-4.1' },
  ])
  expect(screen.getByRole('textbox', { name: '第 1 个分类名称' })).toHaveValue(
    '未分类'
  )
  await user.clear(screen.getByRole('textbox', { name: '第 1 个分类名称' }))
  await user.type(
    screen.getByRole('textbox', { name: '第 1 个分类名称' }),
    '通用模型'
  )
  network.fail = true
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(requests).toHaveLength(1))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: '保存配置' })).toBeEnabled()
  )
  expect(screen.getByRole('textbox', { name: '第 1 个分类名称' })).toHaveValue(
    '通用模型'
  )
  expect(
    screen.getByRole('combobox', { name: 'default的探测模型' })
  ).toBeVisible()
  expect(screen.getByRole('button', { name: '立即探测' })).toBeDisabled()
  network.fail = false
  await user.click(screen.getByRole('button', { name: '保存配置' }))
  await waitFor(() => expect(requests).toHaveLength(2))
  expect(requests[1]).toEqual(requests[0])
  expect(requests[1].revision).toBe(3)
})

test('已有配置迁入未分类后保留暂不可用模型、展示字和立即探测能力', () => {
  renderSettings(
    [
      {
        group_name: 'default',
        probe_model: 'gpt-5.6-sol',
        display_initial: 'D',
      },
    ],
    undefined,
    { default: [] }
  )
  expect(screen.getByRole('textbox', { name: '第 1 个分类名称' })).toHaveValue(
    '未分类'
  )
  expect(
    screen.getByRole('combobox', { name: 'default的探测模型' })
  ).toHaveTextContent('gpt-5.6-sol')
  expect(screen.getByRole('textbox', { name: 'default的展示字' })).toHaveValue(
    'D'
  )
  expect(screen.getByRole('button', { name: '立即探测' })).toBeEnabled()
})
