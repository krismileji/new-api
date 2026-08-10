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
  ChannelMonitorSmartScheduleStabilityConfirmationRequiredError,
  clearChannelMonitorSmartScheduleRouteExploration,
  clearChannelMonitorSmartScheduleRouteStability,
  getChannelMonitorCostOverview,
  getChannelMonitorSmartScheduleRoutes,
  getChannelMonitorTodaySuccess,
  previewChannelMonitorNotificationEmail,
  updateChannelMonitorSmartScheduleChannelConfig,
  updateChannelMonitorSmartScheduleGroupPause,
  updateChannelMonitorSmartScheduleRoutePrimary,
  updateChannelMonitorSmartScheduleRouteConfig,
} from '../api'

test('previews an email with exactly the selected notification types', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: {
        success: true,
        message: '',
        data: {
          subject: '渠道监控：1 个余额预警，1 项更新失败',
          html: '<h3>上游余额预警</h3><h3>定时更新任务失败</h3>',
          notification_types: ['balance_warning', 'task_failed'],
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    const response = await previewChannelMonitorNotificationEmail({
      notificationTypes: ['balance_warning', 'task_failed'],
    })
    assert.deepEqual(response.data.notification_types, [
      'balance_warning',
      'task_failed',
    ])
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requestConfig?.url,
    '/api/channel_monitor/settings/email-preview'
  )
  assert.equal(requestConfig?.method, 'post')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    notification_types: ['balance_warning', 'task_failed'],
  })
})

test('updates all group-model route participation for one channel', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: { success: true, message: '', data: { excluded: false } },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await updateChannelMonitorSmartScheduleChannelConfig({
      channelId: 7,
      excluded: false,
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requestConfig?.url,
    '/api/channel_monitor/channel/7/schedule/routes'
  )
  assert.equal(requestConfig?.method, 'put')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    excluded: false,
  })
})

test('loads and updates one group-model scheduling route', async () => {
  const originalAdapter = api.defaults.adapter
  const requests: AxiosRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config)
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
    await getChannelMonitorSmartScheduleRoutes()
    await updateChannelMonitorSmartScheduleRouteConfig({
      channelId: 7,
      group: 'vip',
      model: 'model-a',
      excluded: true,
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requests[0]?.url, '/api/channel_monitor/schedule')
  assert.equal(requests[0]?.method, 'get')
  assert.equal(
    requests[1]?.url,
    '/api/channel_monitor/channel/7/schedule/route'
  )
  assert.equal(requests[1]?.method, 'put')
  assert.deepEqual(JSON.parse(String(requests[1]?.data)), {
    group: 'vip',
    model: 'model-a',
    excluded: true,
  })
})

test('sets one channel group-model route traffic pause duration', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: {
        success: true,
        message: '',
        data: {
          channel_id: 7,
          group: 'vip',
          model: 'gpt-4o-mini',
          duration_minutes: 90,
          paused_until: 4_102_444_800,
          affected_routes: 1,
          changed: true,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    const response = await updateChannelMonitorSmartScheduleGroupPause({
      channelId: 7,
      group: 'vip',
      model: 'gpt-4o-mini',
      durationMinutes: 90,
    })
    assert.equal(response.data.affected_routes, 1)
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requestConfig?.url,
    '/api/channel_monitor/channel/7/schedule/route/pause'
  )
  assert.equal(requestConfig?.method, 'put')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    group: 'vip',
    model: 'gpt-4o-mini',
    duration_minutes: 90,
  })
})

test('sets and clears a fixed primary route with its stability option', async () => {
  const originalAdapter = api.defaults.adapter
  const requests: AxiosRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config)
    return {
      data: {
        success: true,
        message: '',
        data: {
          channel_id: 7,
          group: 'vip',
          model: 'model-a',
          duration_minutes: 0,
          allow_stability_degrade: false,
          manual_primary_until: 0,
          routing_changed: true,
          task: null,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await updateChannelMonitorSmartScheduleRoutePrimary({
      channelId: 7,
      group: 'vip',
      model: 'model-a',
      durationMinutes: 90,
      allowStabilityDegrade: true,
    })
    await updateChannelMonitorSmartScheduleRoutePrimary({
      channelId: 7,
      group: 'vip',
      model: 'model-a',
      durationMinutes: 0,
      allowStabilityDegrade: false,
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requests[0]?.url,
    '/api/channel_monitor/channel/7/schedule/route/primary'
  )
  assert.deepEqual(JSON.parse(String(requests[0]?.data)), {
    group: 'vip',
    model: 'model-a',
    duration_minutes: 90,
    allow_stability_degrade: true,
  })
  assert.deepEqual(JSON.parse(String(requests[1]?.data)), {
    group: 'vip',
    model: 'model-a',
    duration_minutes: 0,
    allow_stability_degrade: false,
  })
})

test('requires and sends explicit confirmation before overriding stability protection', async () => {
  const originalAdapter = api.defaults.adapter
  const requests: AxiosRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config)
    const request = JSON.parse(String(config.data)) as {
      confirm_stability_override?: boolean
    }
    return {
      data: request.confirm_stability_override
        ? {
            success: true,
            message: '',
            data: {
              channel_id: 7,
              group: 'vip',
              model: 'model-a',
              duration_minutes: 90,
              allow_stability_degrade: true,
              manual_primary_until: 1_900_000_000,
              stability_protection_cleared: true,
              routing_changed: true,
              task: null,
            },
          }
        : {
            success: false,
            code: 'smart_schedule_route_stability_confirmation_required',
            message: '该路由处于稳定性保护状态，请确认后继续',
            data: null,
          },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  const request = {
    channelId: 7,
    group: 'vip',
    model: 'model-a',
    durationMinutes: 90,
    allowStabilityDegrade: true,
  }
  try {
    await assert.rejects(
      () => updateChannelMonitorSmartScheduleRoutePrimary(request),
      ChannelMonitorSmartScheduleStabilityConfirmationRequiredError
    )
    const response = await updateChannelMonitorSmartScheduleRoutePrimary({
      ...request,
      confirmStabilityOverride: true,
    })
    assert.equal(response.data.stability_protection_cleared, true)
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requests.length, 2)
  assert.equal(
    JSON.parse(String(requests[0]?.data)).confirm_stability_override,
    undefined
  )
  assert.equal(
    JSON.parse(String(requests[1]?.data)).confirm_stability_override,
    true
  )
})

test('posts manual stability clear for one group-model route', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: {
        success: true,
        message: '',
        data: {
          cleared: true,
          previous_state: 'probing',
          priority: 90,
          weight: 35,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await clearChannelMonitorSmartScheduleRouteStability({
      channelId: 7,
      group: 'vip',
      model: 'model-a',
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requestConfig?.url,
    '/api/channel_monitor/channel/7/schedule/route/stability/clear'
  )
  assert.equal(requestConfig?.method, 'post')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    group: 'vip',
    model: 'model-a',
  })
})

test('posts manual exploration clear for one group-model route', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: {
        success: true,
        message: '',
        data: {
          cleared: true,
          previous_kind: 'insufficient_samples',
          priority: 80,
          weight: 60,
        },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await clearChannelMonitorSmartScheduleRouteExploration({
      channelId: 7,
      group: 'vip',
      model: 'model-a',
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requestConfig?.url,
    '/api/channel_monitor/channel/7/schedule/route/exploration/clear'
  )
  assert.equal(requestConfig?.method, 'post')
  assert.deepEqual(JSON.parse(String(requestConfig?.data)), {
    group: 'vip',
    model: 'model-a',
  })
})

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
    date: undefined,
  })
})

test('requests channel and API Key cost details for the selected Beijing date', async () => {
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
    await getChannelMonitorCostOverview(30, undefined, 2, false, '2026-07-23')
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.deepEqual(requestConfig?.params, {
    days: 30,
    channel_id: undefined,
    page: 2,
    summary_only: undefined,
    date: '2026-07-23',
  })
})

test('requests the Beijing-day success summary for the monitor dashboard', async () => {
  const originalAdapter = api.defaults.adapter
  let requestUrl = ''
  const adapter: AxiosAdapter = async (config) => {
    requestUrl = config.url ?? ''
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
    await getChannelMonitorTodaySuccess()
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requestUrl, '/api/channel_monitor/success/today')
})

test('requests daily success and cache insights for the selected date', async () => {
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
    await getChannelMonitorTodaySuccess({
      days: 30,
      date: '2026-07-23',
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.deepEqual(requestConfig?.params, {
    days: 30,
    date: '2026-07-23',
  })
})
