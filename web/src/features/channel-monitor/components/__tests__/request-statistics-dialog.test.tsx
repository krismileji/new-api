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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type {
  RequestStatsQuery,
  RequestStatsResult,
  RequestStatsRow,
} from '../../lib/request-statistics'
import RequestStatisticsDialog from '../request-statistics-dialog'

const { getRequestStatsMock, getRequestStatsSettingsMock } = vi.hoisted(() => ({
  getRequestStatsMock: vi.fn(),
  getRequestStatsSettingsMock: vi.fn(),
}))

vi.mock('../../lib/request-statistics', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/request-statistics')>()),
  getRequestStats: getRequestStatsMock,
  getRequestStatsSettings: getRequestStatsSettingsMock,
}))

const userRow: RequestStatsRow = {
  id: 7,
  name: 'alice',
  user_id: 7,
  api_key_id: 0,
  request_count: 3,
  stream_request_count: 1,
  non_stream_request_count: 2,
  unclassified_stream_request_count: 0,
  known_input_request_count: 3,
  unknown_input_request_count: 0,
  low_input_count: 2,
  low_input_rate: 66.67,
  low_input_auto_disabled_count: 0,
}

const apiKeyRow: RequestStatsRow = {
  ...userRow,
  id: 9,
  name: 'key-a',
  api_key_id: 9,
}

const channelRow: RequestStatsRow = {
  ...apiKeyRow,
  id: 12,
  name: 'channel-a',
}

function result(items: RequestStatsRow[]): RequestStatsResult {
  return {
    low_input_threshold_k: 10,
    revision: 1,
    source: 'redis_daily',
    coverage_partial: false,
    data_cutoff_at: 1,
    processed_at: 1,
    summary: userRow,
    items,
    total: items.length,
  }
}

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return render(
    <QueryClientProvider client={client}>
      <RequestStatisticsDialog onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
}

describe('用户请求统计弹窗', () => {
  test('按用户、API Key、渠道逐层下钻并保留父级筛选', async () => {
    const user = userEvent.setup()
    getRequestStatsMock.mockImplementation(async (query: RequestStatsQuery) => {
      if (query.level === 'user') return result([userRow])
      if (query.level === 'api_key') return result([apiKeyRow])
      return result([channelRow])
    })
    getRequestStatsSettingsMock.mockResolvedValue({
      revision: 1,
      low_input_threshold_k: 10,
      enabled: false,
      disable_rate_percent: 80,
      minimum_known_input_requests: 100,
      started_at: 1,
    })

    renderDialog()

    await user.click(
      (await screen.findAllByRole('button', { name: /alice/ }))[0]
    )
    await user.click(
      (await screen.findAllByRole('button', { name: /key-a/ }))[0]
    )

    expect(await screen.findByText(/channel-a/)).toBeVisible()
    await waitFor(() => {
      const query = getRequestStatsMock.mock.calls.at(
        -1
      )?.[0] as RequestStatsQuery
      expect(query).toMatchObject({
        level: 'channel',
        user_id: 7,
        api_key_id: 9,
      })
    })
  })
})
