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
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { Toaster, toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { useDataTable } from '@/components/data-table'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'

import type { User } from '../../types'
import { DataTableBulkActions } from '../data-table-bulk-actions'
import { UsersProvider } from '../users-provider'

const users: User[] = [2, 3].map((id) => ({
  id,
  username: `user-${id}`,
  display_name: '',
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  status: 1,
  role: 1,
}))

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

function UserList() {
  const { table } = useDataTable({
    data: users,
    columns: [{ accessorKey: 'username' }],
    enableRowSelection: true,
    getRowId: (row) => String(row.id),
  })

  return (
    <>
      {table.getRowModel().rows.map((row) => (
        <label key={row.id}>
          <input
            type='checkbox'
            aria-label={row.original.username}
            checked={row.getIsSelected()}
            onChange={row.getToggleSelectedHandler()}
          />
          {row.original.username}
        </label>
      ))}
      <DataTableBulkActions table={table} />
    </>
  )
}

async function setup() {
  const post = vi.spyOn(api, 'post').mockImplementation(async (_url, body) => {
    const { ids } = body as { ids: number[] }
    return {
      data: {
        success: true,
        data: { succeeded_ids: ids, failed_ids: [] },
      },
    }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <UsersProvider>
            <UserList />
          </UsersProvider>
        </TooltipProvider>
        <Toaster />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await screen.findByRole('checkbox', { name: 'user-2' })
  return { post, user: userEvent.setup() }
}

afterEach(() => {
  cleanup()
  toast.dismiss()
  vi.restoreAllMocks()
})

it.each([
  {
    action: 'disable',
    button: 'Disable selected users',
    title: 'Disable access for 1 selected users?',
    toast: 'Disabled 1 users',
  },
  {
    action: 'enable',
    button: 'Enable selected users',
    title: 'Enable access for 1 selected users?',
    toast: 'Enabled 1 users',
  },
  {
    action: 'delete',
    button: 'Delete selected users',
    title: 'Delete 1 selected users?',
    toast: 'Deleted 1 users',
  },
])(
  '$action sends the selected users only after confirmation',
  async ({ action, button, title, toast: successMessage }) => {
    const { post, user } = await setup()
    await user.click(screen.getByRole('checkbox', { name: 'user-2' }))
    await user.click(screen.getByRole('button', { name: button }))

    const dialog = await screen.findByRole('alertdialog')
    expect(dialog).toHaveAccessibleName(title)
    expect(post).not.toHaveBeenCalled()
    if (action === 'delete') {
      expect(dialog).toHaveTextContent(
        'Permanently delete selected users and their data.'
      )
    }

    await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/user/manage/batch', {
        ids: [2],
        action,
      })
    )
    await waitFor(() => expect(document.body).toHaveTextContent(successMessage))
    expect(screen.getByRole('checkbox', { name: 'user-3' })).not.toBeChecked()
  }
)

it('does not submit deletion when its confirmation is canceled', async () => {
  const { post, user } = await setup()
  await user.click(screen.getByRole('checkbox', { name: 'user-2' }))
  await user.click(
    screen.getByRole('button', { name: 'Delete selected users' })
  )
  const dialog = await screen.findByRole('alertdialog')
  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(post).not.toHaveBeenCalled()
  expect(screen.getByRole('checkbox', { name: 'user-2' })).toBeChecked()
})

it('keeps failed users selected after a partial result', async () => {
  const { post, user } = await setup()
  post.mockResolvedValueOnce({
    data: {
      success: true,
      data: { succeeded_ids: [2], failed_ids: [3] },
    },
  })
  await user.click(screen.getByRole('checkbox', { name: 'user-2' }))
  await user.click(screen.getByRole('checkbox', { name: 'user-3' }))
  await user.click(
    screen.getByRole('button', { name: 'Disable selected users' })
  )
  const dialog = await screen.findByRole('alertdialog')
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))

  await waitFor(() =>
    expect(document.body).toHaveTextContent(
      'Processed 1 of 2 users; 1 failed.'
    )
  )
  expect(screen.getByRole('checkbox', { name: 'user-2' })).not.toBeChecked()
  expect(screen.getByRole('checkbox', { name: 'user-3' })).toBeChecked()
})
