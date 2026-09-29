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
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SystemTasksPanel } from '../components/system-tasks-panel'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('offers downstream task types and sends the selected type when listing and cleaning history', async () => {
  const task = {
    id: 1,
    task_id: 'detection-history',
    type: 'channel_model_detection',
    status: 'succeeded',
    created_at: 100,
    updated_at: 200,
    locked_by: 'detection-runner',
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (_url, config) => ({
    data: {
      success: true,
      data:
        config?.params?.scope === 'active'
          ? [
              {
                ...task,
                task_id: 'active',
                status: 'running',
                locked_by: 'active-runner',
              },
            ]
          : [task],
      total: 1,
    },
  }))
  const remove = vi.spyOn(api, 'delete').mockResolvedValue({
    data: { success: true, data: { deleted_count: 1 } },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  try {
    render(
      <QueryClientProvider client={client}>
        <SystemTasksPanel />
      </QueryClientProvider>
    )
    const types = await screen.findByRole('combobox', { name: 'Type' })
    for (const label of [
      '渠道监控',
      '渠道模型检测',
      '智能调度',
      '智能调度探测',
      '监控历史清理',
      '上游账户余额',
      '上游自动任务',
      '本地响应退款',
    ]) {
      expect(within(types).getByRole('option', { name: label })).toBeVisible()
    }
    const row = await screen.findByRole('row', { name: /detection-runner/ })
    expect(within(row).getByText('渠道模型检测')).toBeVisible()
    await userEvent.selectOptions(types, 'channel_model_detection')
    await waitFor(() =>
      expect(get).toHaveBeenLastCalledWith(
        '/api/system-task/list',
        expect.objectContaining({
          params: expect.objectContaining({
            scope: 'history',
            type: 'channel_model_detection',
            offset: 0,
          }),
        })
      )
    )
    expect(screen.getByText('active-runner')).toBeVisible()
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Clean task history' })
      ).toBeEnabled()
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Clean task history' })
    )
    expect(remove).not.toHaveBeenCalled()
    await userEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Delete',
      })
    )
    await waitFor(() =>
      expect(remove).toHaveBeenCalledWith('/api/system-task/history', {
        params: { type: 'channel_model_detection', status: '' },
      })
    )
  } finally {
    cleanup()
    client.clear()
  }
})
