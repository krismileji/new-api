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
import assert from 'node:assert/strict'
import { test } from 'node:test'

import type { AxiosAdapter, AxiosRequestConfig } from 'axios'

import { api } from '@/lib/api'

import {
  getChannelMonitorCostOverview,
  updateChannelMonitorGroupChannels,
} from '../api'

test('requests only the lightweight cost summary for the monitor dashboard', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: { success: true, message: '', data: {} },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await getChannelMonitorCostOverview(2, undefined, 1, true)
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.deepEqual(requestConfig?.params, {
    days: 2,
    channel_id: undefined,
    page: 1,
    summary_only: true,
  })
})

test('normalizes nullable group membership change lists from older servers', async () => {
  const originalAdapter = api.defaults.adapter
  const adapter: AxiosAdapter = async (config) => ({
    data: {
      success: true,
      message: '',
      data: {
        group: 'vip',
        channel_ids: [7],
        added_channel_ids: null,
        removed_channel_ids: null,
      },
    },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  api.defaults.adapter = adapter

  try {
    const response = await updateChannelMonitorGroupChannels({
      group: 'vip',
      channelIds: [7],
    })

    assert.deepEqual(response.data.added_channel_ids, [])
    assert.deepEqual(response.data.removed_channel_ids, [])
  } finally {
    api.defaults.adapter = originalAdapter
  }
})
