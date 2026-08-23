/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ChannelsPrimaryButtons } from '../channels-primary-buttons'
import { ChannelsProvider } from '../channels-provider'

describe('ChannelsPrimaryButtons layout', () => {
  beforeEach(() => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'admin',
      role: ROLE.SUPER_ADMIN,
    })
  })

  afterEach(() => {
    useAuthStore.getState().auth.reset()
  })

  test('preserves existing sm controls while keeping the actions menu accessible', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsProvider>
          <ChannelsPrimaryButtons />
        </ChannelsProvider>
      </QueryClientProvider>
    )

    expect(
      screen.queryByRole('button', { name: '同渠道配置' })
    ).not.toBeInTheDocument()
    const moreButton = screen.getByRole('button', { name: 'More' })
    expect(moreButton).toHaveAccessibleName('More')

    for (const name of ['Batch Operations', 'Tag Mode', 'Sort by ID']) {
      expect(screen.getByRole('switch', { name }).closest('div')).toHaveClass(
        'hidden',
        'sm:flex'
      )
    }

    await user.click(moreButton)

    for (const name of ['Batch Operations', 'Tag Mode', 'Sort by ID']) {
      expect(screen.getByRole('menuitemcheckbox', { name })).toHaveClass(
        'sm:hidden'
      )
    }
    expect(
      screen
        .getAllByRole('separator')
        .some((separator) => separator.classList.contains('sm:hidden'))
    ).toBe(true)
  })
})
