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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ComponentProps } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { ChannelMonitorPageLayout } from '../channel-monitor-page-layout'
import { ChannelMonitorToolbar } from '../channel-monitor-toolbar'

function renderToolbar(
  overrides: Partial<ComponentProps<typeof ChannelMonitorToolbar>> = {}
) {
  const props = {
    actions: {
      batchTest: vi.fn(),
      taskHistory: vi.fn(),
      smartScheduleHistory: vi.fn(),
      settings: vi.fn(),
      variableGroups: vi.fn(),
      limitGroups: vi.fn(),
      upstreamAccounts: vi.fn(),
      automations: vi.fn(),
      tokenProtection: vi.fn(),
      groupMonitorSettings: vi.fn(),
      smartScheduleSettings: vi.fn(),
      refresh: vi.fn(),
    },
    autoUpdateLabel: '自动更新：每 5 分钟',
    smartScheduleLabel: '智能调度：已关闭',
    refreshPending: false,
    refreshDisabled: false,
    onPrefetchTaskHistory: vi.fn(),
    onPrefetchSmartScheduleHistory: vi.fn(),
    ...overrides,
  }
  const view = render(
    <ChannelMonitorPageLayout
      actions={<ChannelMonitorToolbar {...props} />}
      realtimeStatus={null}
    >
      <div>监控主体</div>
    </ChannelMonitorPageLayout>
  )
  return { ...view, props, user: userEvent.setup() }
}

describe('渠道监控顶部操作', () => {
  test('默认只展示四个文字入口，记录和配置在菜单内按需展开', () => {
    renderToolbar()
    const toolbar = screen.getByRole('toolbar', { name: '渠道监控操作' })
    const buttons = within(toolbar).getAllByRole('button')

    expect(buttons.map((button) => button.textContent).filter(Boolean)).toEqual(
      ['运行记录', '配置管理', '连通性测试', '刷新']
    )
    expect(
      within(toolbar).getByRole('button', { name: '隐藏敏感信息' })
    ).toHaveAttribute('aria-pressed', 'false')
    expect(screen.queryByRole('menuitem')).not.toBeInTheDocument()
    for (const name of ['运行记录', '配置管理']) {
      expect(screen.getByRole('button', { name })).toHaveAttribute(
        'aria-expanded',
        'false'
      )
    }
    expect(toolbar).toHaveClass('flex-wrap')
  })

  test.each([
    ['运行记录', '倍率与余额更新记录', 'taskHistory'],
    ['运行记录', '智能调度执行记录', 'smartScheduleHistory'],
    ['配置管理', '渠道监控设置', 'settings'],
    ['配置管理', '分组监控设置', 'groupMonitorSettings'],
    ['配置管理', '智能调度设置', 'smartScheduleSettings'],
    ['配置管理', '上游账户', 'upstreamAccounts'],
    ['配置管理', '上游自动任务', 'automations'],
    ['配置管理', '共享请求与变量', 'variableGroups'],
    ['配置管理', '共享限流组', 'limitGroups'],
    ['配置管理', 'API Key 自动禁用', 'tokenProtection'],
  ] as const)(
    '从%s选择%s时打开对应功能并关闭菜单',
    async (menu, label, action) => {
      const { props, user } = renderToolbar()
      const trigger = screen.getByRole('button', { name: menu })
      await user.click(trigger)
      expect(trigger).toHaveAttribute('aria-expanded', 'true')

      await user.click(await screen.findByRole('menuitem', { name: label }))

      expect(props.actions[action]).toHaveBeenCalledOnce()
      await waitFor(() =>
        expect(trigger).toHaveAttribute('aria-expanded', 'false')
      )
    }
  )

  test('打开配置管理时按用途分组并展示当前更新和调度状态', async () => {
    const { props, user } = renderToolbar()
    await user.click(screen.getByRole('button', { name: '配置管理' }))

    for (const name of ['监控与调度', '上游与共享资源', '安全防护']) {
      expect(screen.getByRole('group', { name })).toBeVisible()
    }
    expect(screen.getByText(props.autoUpdateLabel)).toBeVisible()
    expect(screen.getByText(props.smartScheduleLabel)).toBeVisible()
  })

  test('键盘打开运行记录后可预加载并选择历史入口', async () => {
    const { props, user } = renderToolbar()
    await user.tab()
    expect(screen.getByRole('button', { name: '运行记录' })).toHaveFocus()
    await user.keyboard('{ArrowDown}')
    const history = await screen.findByRole('menuitem', {
      name: '倍率与余额更新记录',
    })
    await waitFor(() => expect(history).toHaveFocus())
    expect(props.onPrefetchTaskHistory).toHaveBeenCalled()
    await user.keyboard('{Enter}')

    expect(props.actions.taskHistory).toHaveBeenCalledOnce()
  })

  test('连通性测试和刷新无需打开菜单即可执行', async () => {
    const { props, user } = renderToolbar()
    await user.click(screen.getByRole('button', { name: '渠道连通性测试' }))
    expect(props.actions.batchTest).toHaveBeenCalledOnce()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    expect(props.actions.refresh).toHaveBeenCalledOnce()
  })

  test.each([
    { refreshPending: true, refreshDisabled: false, label: '刷新中…' },
    { refreshPending: false, refreshDisabled: true, label: '刷新' },
  ])('刷新状态为 $label 且请求未完成时阻止重复提交', async (state) => {
    const { props, user } = renderToolbar(state)
    const refresh = screen.getByRole('button', { name: '刷新' })
    expect(refresh).toBeDisabled()
    expect(refresh).toHaveTextContent(state.label)
    expect(refresh).toHaveAttribute('aria-busy', String(state.refreshPending))

    await user.click(refresh)
    expect(props.actions.refresh).not.toHaveBeenCalled()
  })
})
