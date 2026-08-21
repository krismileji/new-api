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
import { test } from 'vitest'

import type { AxiosAdapter, AxiosRequestConfig } from 'axios'

import { api } from '@/lib/api'

import {
  getPricingGroupMonitor,
  updateChannelGroupMonitorSettings,
} from '../api'

test('loads user-visible group monitoring from the pricing endpoint', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: { success: true, message: '', data: { enabled: true, items: [] } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await getPricingGroupMonitor()
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requestConfig?.url, '/api/pricing/group-monitor')
  assert.equal(requestConfig?.method, 'get')
})

test('saves monitoring groups in the configured order with revision control', async () => {
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
    await updateChannelGroupMonitorSettings({
      enabled: false,
      groups: [
        { group_name: 'vip', probe_model: 'gpt-4.1' },
        { group_name: 'default', probe_model: 'gpt-4.1-mini' },
      ],
      intervalSeconds: 300,
      displayValue: 12,
      displayUnit: 'hour',
      revision: 7,
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requestConfig?.url, '/api/channel_monitor/group_monitor/settings')
  assert.equal(requestConfig?.method, 'put')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    enabled: false,
    groups: [
      { group_name: 'vip', probe_model: 'gpt-4.1' },
      { group_name: 'default', probe_model: 'gpt-4.1-mini' },
    ],
    interval_seconds: 300,
    display_value: 12,
    display_unit: 'hour',
    revision: 7,
  })
})
