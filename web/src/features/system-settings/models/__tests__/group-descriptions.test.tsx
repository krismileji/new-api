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
import { afterEach, expect, test, vi } from 'vitest'

import * as settingsApi from '../../api'
import { SettingsPageProvider } from '../../components/settings-page-context'
import { RatioSettingsCard } from '../ratio-settings-card'

const initialGroups = {
  GroupRatio: '{"default":1}',
  GroupOrder: '["default"]',
  GroupDescriptions: '{}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{"default":"默认分组说明"}',
  GroupGroupRatio: '{}',
  AutoGroups: '[]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

const queryClients: QueryClient[] = []
const containers: HTMLElement[] = []

afterEach(() => {
  cleanup()
  for (const queryClient of queryClients) queryClient.clear()
  for (const container of containers) container.remove()
  queryClients.length = 0
  containers.length = 0
})

function renderSettings(groupDefaults = initialGroups) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  queryClients.push(queryClient)
  const container = document.createElement('div')
  const actionsContainer = document.createElement('div')
  const contentContainer = document.createElement('div')
  container.append(actionsContainer, contentContainer)
  document.body.append(container)
  containers.push(container)

  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <RatioSettingsCard
          modelDefaults={{
            ModelPrice: '{}',
            ModelRatio: '{}',
            CacheRatio: '{}',
            CreateCacheRatio: '{}',
            CompletionRatio: '{}',
            ImageRatio: '{}',
            AudioRatio: '{}',
            AudioCompletionRatio: '{}',
            ExposeRatioEnabled: false,
            BillingMode: '{}',
            BillingExpr: '{}',
          }}
          groupDefaults={groupDefaults}
          toolPricesDefault='{}'
          visibleTabs={['groups']}
        />
      </SettingsPageProvider>
    </QueryClientProvider>,
    { container: contentContainer, baseElement: container }
  )
}

test('取消用户可选后仍显示说明，并在重新勾选时恢复编辑', async () => {
  const user = userEvent.setup()
  renderSettings()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  expect(screen.getByDisplayValue('默认分组说明')).toBeDisabled()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  expect(screen.getByDisplayValue('默认分组说明')).toBeEnabled()
})

test('取消用户可选并切换 JSON 模式后，重新勾选仍保留说明', async () => {
  const user = userEvent.setup()
  renderSettings()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))

  expect(screen.getByDisplayValue('默认分组说明')).toBeEnabled()
})

test('保存取消可选的分组后，重新加载设置再勾选仍保留说明', async () => {
  const user = userEvent.setup()
  const saved: Record<string, string> = {}
  vi.spyOn(settingsApi, 'updateSystemOption').mockImplementation(
    async (request) => {
      saved[request.key] = String(request.value)
      return { success: true, message: '' }
    }
  )
  const view = renderSettings()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() => expect(saved.UserUsableGroups).toBe('{}'))
  expect(JSON.parse(saved.GroupDescriptions ?? '{}')).toEqual({
    default: '默认分组说明',
  })

  view.unmount()
  renderSettings({ ...initialGroups, ...saved })
  expect(
    screen.getByRole('checkbox', { name: 'User selectable' })
  ).not.toBeChecked()
  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  expect(screen.getByDisplayValue('默认分组说明')).toBeEnabled()
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() =>
    expect(JSON.parse(saved.UserUsableGroups)).toEqual({
      default: '默认分组说明',
    })
  )
})

test('在 JSON 模式更新说明后移除可选分组，再勾选时保留最新说明', async () => {
  const user = userEvent.setup()
  renderSettings()

  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  const editor = screen.getByRole('textbox', { name: 'Selectable groups' })
  fireEvent.input(editor, {
    target: { value: '{"default":"更新后的说明"}' },
  })
  fireEvent.input(editor, { target: { value: '{}' } })
  await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))

  expect(screen.getByDisplayValue('更新后的说明')).toBeEnabled()
})

test('显式清空说明后取消可选并保存，重新加载时不恢复旧说明', async () => {
  const user = userEvent.setup()
  const saved: Record<string, string> = {}
  vi.spyOn(settingsApi, 'updateSystemOption').mockImplementation(
    async (request) => {
      saved[request.key] = String(request.value)
      return { success: true, message: '' }
    }
  )
  const defaults = {
    ...initialGroups,
    GroupDescriptions: '{"default":"旧说明"}',
  }
  const view = renderSettings(defaults)

  await user.clear(screen.getByPlaceholderText('Group description'))
  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() => expect(saved.UserUsableGroups).toBe('{}'))
  expect(JSON.parse(saved.GroupDescriptions ?? '{}')).toEqual({ default: '' })

  view.unmount()
  renderSettings({ ...defaults, ...saved })
  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  expect(screen.getByPlaceholderText('Group description')).toHaveValue('')
})

test('说明保存失败时停止更新可选分组，避免删除仍未备份的说明', async () => {
  const user = userEvent.setup()
  const update = vi.spyOn(settingsApi, 'updateSystemOption').mockResolvedValue({
    success: false,
    message: '保存失败',
  })
  renderSettings()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() => expect(update).toHaveBeenCalled())
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Save group ratios' })
    ).toBeEnabled()
  )
  expect(update.mock.calls.map(([request]) => request.key)).toEqual([
    'GroupDescriptions',
  ])
})

test('取消用户可选后，分组详情仍展示说明', async () => {
  const user = userEvent.setup()
  renderSettings()

  await user.click(screen.getByRole('checkbox', { name: 'User selectable' }))
  await user.click(screen.getByRole('button', { name: 'Details' }))

  const details = screen.getByRole('dialog', { name: 'Group details: default' })
  expect(within(details).getByText('默认分组说明')).toBeVisible()
  expect(within(details).getByText('No')).toBeVisible()
})

test('分组改名后说明跟随新名称保存，其他分组的说明保持独立', async () => {
  const user = userEvent.setup()
  const saved: Record<string, string> = {}
  vi.spyOn(settingsApi, 'updateSystemOption').mockImplementation(
    async (request) => {
      saved[request.key] = String(request.value)
      return { success: true, message: '' }
    }
  )
  renderSettings({
    ...initialGroups,
    GroupRatio: '{"default":1,"vip":2}',
    GroupOrder: '["default","vip"]',
    UserUsableGroups: '{"default":"默认分组说明","vip":"VIP 说明"}',
  })

  const groupName = screen.getByDisplayValue('default')
  await user.clear(groupName)
  await user.type(groupName, 'renamed')
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() =>
    expect(JSON.parse(saved.UserUsableGroups ?? '{}')).toEqual({
      renamed: '默认分组说明',
      vip: 'VIP 说明',
    })
  )
  expect(JSON.parse(saved.GroupDescriptions)).toEqual({
    renamed: '默认分组说明',
    vip: 'VIP 说明',
  })
})

test('删除分组时一并删除保留的说明，重新添加分组不会复用旧说明', async () => {
  const user = userEvent.setup()
  const saved: Record<string, string> = {}
  vi.spyOn(settingsApi, 'updateSystemOption').mockImplementation(
    async (request) => {
      saved[request.key] = String(request.value)
      return { success: true, message: '' }
    }
  )
  renderSettings({
    ...initialGroups,
    GroupDescriptions: '{"default":"默认分组说明"}',
  })

  await user.click(screen.getByRole('button', { name: 'Delete' }))
  await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
  await waitFor(() => expect(saved.UserUsableGroups).toBe('{}'))
  expect(saved.GroupDescriptions).toBe('{}')

  await user.click(screen.getByRole('button', { name: 'Add group' }))
  const groupName = screen.getByDisplayValue('group_1')
  await user.clear(groupName)
  await user.type(groupName, 'default')
  expect(screen.getByPlaceholderText('Group description')).toHaveValue('')
})

test.each(['{}', ''])(
  '在 JSON 模式将分组清为 %j 并保存时，一并删除该分组保留的说明',
  async (emptyGroups) => {
    const user = userEvent.setup()
    const saved: Record<string, string> = {}
    vi.spyOn(settingsApi, 'updateSystemOption').mockImplementation(
      async (request) => {
        saved[request.key] = String(request.value)
        return { success: true, message: '' }
      }
    )
    renderSettings({
      ...initialGroups,
      GroupDescriptions: '{"default":"默认分组说明"}',
    })

    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    fireEvent.input(screen.getByRole('textbox', { name: 'Group ratios' }), {
      target: { value: emptyGroups },
    })
    fireEvent.input(
      screen.getByRole('textbox', { name: 'Selectable groups' }),
      {
        target: { value: '{}' },
      }
    )
    await user.click(screen.getByRole('button', { name: 'Save group ratios' }))
    await waitFor(() => expect(saved.UserUsableGroups).toBe('{}'))
    expect(saved.GroupDescriptions).toBe('{}')
  }
)
