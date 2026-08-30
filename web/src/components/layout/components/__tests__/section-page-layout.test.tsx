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
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { SectionPageLayout } from '../section-page-layout'

function renderLayout(stackHeaderOnMobile?: boolean) {
  return render(
    <SectionPageLayout stackHeaderOnMobile={stackHeaderOnMobile}>
      <SectionPageLayout.Title>渠道监控</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <button type='button'>刷新</button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>监控内容</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

describe('SectionPageLayout responsive header', () => {
  test('keeps the existing wrapping header layout by default', () => {
    const { container } = renderLayout()

    expect(
      screen.getByRole('heading', { level: 2, name: '渠道监控' })
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '刷新' })).toBeInTheDocument()

    const headerRow = container.querySelector(
      '[data-slot="section-page-layout-header-row"]'
    )
    const title = container.querySelector(
      '[data-slot="section-page-layout-title"]'
    )
    const actions = container.querySelector(
      '[data-slot="section-page-layout-actions"]'
    )
    const content = container.querySelector(
      '[data-slot="section-page-layout-content"]'
    )

    expect(headerRow).toHaveClass(
      'flex',
      'flex-wrap',
      'items-center',
      'justify-between',
      'gap-x-3',
      'gap-y-2',
      'sm:gap-x-4'
    )
    expect(headerRow).not.toHaveClass('flex-col', 'sm:flex-row')
    expect(title).toHaveClass('min-w-0', 'flex-1')
    expect(title).not.toHaveClass('w-full', 'sm:w-auto')
    expect(actions).toHaveClass(
      'flex',
      'shrink-0',
      'flex-wrap',
      'items-center',
      'justify-end',
      'gap-2',
      'sm:gap-x-4'
    )
    expect(actions).not.toHaveClass('w-full', 'sm:w-auto')
    expect(content).toHaveClass('flex-1', 'overflow-auto')
    expect(content).toHaveTextContent('监控内容')
  })

  test('stacks full-width title and actions on mobile when enabled', () => {
    const { container } = renderLayout(true)

    const headerRow = container.querySelector(
      '[data-slot="section-page-layout-header-row"]'
    )
    const title = container.querySelector(
      '[data-slot="section-page-layout-title"]'
    )
    const actions = container.querySelector(
      '[data-slot="section-page-layout-actions"]'
    )

    expect(headerRow).toHaveClass(
      'flex',
      'flex-col',
      'items-stretch',
      'justify-between',
      'gap-y-2',
      'sm:flex-row',
      'sm:flex-wrap',
      'sm:items-center',
      'sm:gap-x-4'
    )
    expect(title).toHaveClass('min-w-0', 'w-full', 'sm:w-auto', 'sm:flex-1')
    expect(actions).toHaveClass(
      'flex',
      'w-full',
      'shrink-0',
      'flex-wrap',
      'items-center',
      'justify-end',
      'gap-2',
      'sm:w-auto',
      'sm:gap-x-4'
    )
    expect(actions).not.toHaveClass('justify-start')
  })
})
