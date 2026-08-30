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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { ChannelMonitorPageLayout } from '../channel-monitor-page-layout'

const actionLabels = [
  '渠道连通性测试',
  '倍率与余额更新记录',
  '智能调度执行记录',
  '渠道监控设置',
  '分组监控设置',
  '智能调度设置',
  '刷新',
] as const

function renderPageLayout() {
  return render(
    <ChannelMonitorPageLayout
      actions={actionLabels.map((label) => (
        <button key={label} type='button' aria-label={label}>
          {label}
        </button>
      ))}
      realtimeStatus={<span>Redis 正常</span>}
    >
      <div>监控主体</div>
    </ChannelMonitorPageLayout>
  )
}

describe('渠道监控页面响应式框架', () => {
  test('标题只包含页面名称，实时状态位于可滚动内容区', () => {
    const { container } = renderPageLayout()

    const heading = screen.getByRole('heading', {
      level: 2,
      name: '渠道监控',
    })
    const realtimeRegion = screen.getByRole('region', {
      name: '实时运行状态',
    })
    const content = container.querySelector(
      '[data-slot="section-page-layout-content"]'
    )
    const pageContent = container.querySelector(
      '[data-slot="channel-monitor-page-content"]'
    )

    expect(heading).toHaveTextContent(/^渠道监控$/)
    expect(heading).not.toContainElement(realtimeRegion)
    expect(content).toHaveClass('overflow-auto')
    expect(content).toContainElement(realtimeRegion)
    expect(pageContent?.firstElementChild).toBe(realtimeRegion)
    expect(content).toContainElement(screen.getByText('监控主体'))
  })

  test('具名工具栏按原顺序容纳全部七个操作按钮', () => {
    renderPageLayout()

    const toolbar = screen.getByRole('toolbar', { name: '渠道监控操作' })
    const buttons = within(toolbar).getAllByRole('button')

    expect(buttons).toHaveLength(7)
    expect(buttons.map((button) => button.getAttribute('aria-label'))).toEqual(
      actionLabels
    )
  })

  test('页面启用手机分行并在桌面恢复同行布局', () => {
    const { container } = renderPageLayout()

    const headerRow = container.querySelector(
      '[data-slot="section-page-layout-header-row"]'
    )
    const actions = container.querySelector(
      '[data-slot="section-page-layout-actions"]'
    )
    const toolbar = screen.getByRole('toolbar', { name: '渠道监控操作' })

    expect(headerRow).toHaveClass(
      'flex',
      'flex-col',
      'items-stretch',
      'sm:flex-row',
      'sm:items-center'
    )
    expect(actions).toHaveClass(
      'w-full',
      'justify-end',
      'sm:w-auto',
      'flex-wrap'
    )
    expect(toolbar).toHaveClass(
      'flex',
      'w-full',
      'flex-wrap',
      'justify-end',
      'sm:w-auto'
    )
  })
})
