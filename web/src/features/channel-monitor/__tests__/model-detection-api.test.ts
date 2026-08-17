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

import { getChannelModelDetectionOverview } from '../api'
import {
  cancelChannelModelDetectionRun,
  getChannelModelDetectionRun,
  getChannelModelDetectionRuns,
  isChannelModelDetectionInfrastructureConflict,
  startChannelModelDetectionRun,
} from '../lib/model-detection-channel-api'

test('模型检测总览使用独立只读管理端点', async () => {
  const originalAdapter = api.defaults.adapter
  let requestConfig: AxiosRequestConfig | undefined
  const adapter: AxiosAdapter = async (config) => {
    requestConfig = config
    return {
      data: {
        success: true,
        message: '',
        data: { channels: [] },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await getChannelModelDetectionOverview()
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(requestConfig?.url, '/api/channel_monitor/model_detection')
  assert.equal(requestConfig?.method, 'get')
  assert.equal(requestConfig?.skipBusinessError, true)
  assert.equal(requestConfig?.skipErrorHandler, true)
})

test('模型检测运行控制使用专属启动、详情和取消端点', async () => {
  const originalAdapter = api.defaults.adapter
  const requests: AxiosRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config)
    let data: unknown = {
      run_id: 'run-1',
      status: 'queued',
      preset: 'high',
      preset_source: 'manual_selected',
    }
    if (config.url?.endsWith('/cancel')) {
      data = { run_id: 'run-1', status: 'canceling' }
    } else if (config.method === 'get') {
      data = { run: { run_id: 'run-1', status: 'running' }, executions: [] }
    }
    return {
      data: { success: true, message: '', data },
      status: config.method === 'post' ? 202 : 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await startChannelModelDetectionRun(801, {
      preset: 'high',
      confirm_high_cost: true,
    })
    await getChannelModelDetectionRun('run-1')
    await cancelChannelModelDetectionRun('run-1')
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requests[0]?.url,
    '/api/channel_monitor/model_detection/channel/801/run'
  )
  assert.equal(requests[0]?.method, 'post')
  assert.deepEqual(JSON.parse(String(requests[0]?.data)), {
    preset: 'high',
    confirm_high_cost: true,
  })
  assert.equal(
    requests[1]?.url,
    '/api/channel_monitor/model_detection/runs/run-1'
  )
  assert.equal(requests[1]?.method, 'get')
  assert.equal(
    requests[2]?.url,
    '/api/channel_monitor/model_detection/runs/run-1/cancel'
  )
  assert.equal(requests[2]?.method, 'post')
  for (const request of requests) {
    assert.equal(request.skipBusinessError, true)
    assert.equal(request.skipErrorHandler, true)
  }
})

test('HTTP 409 被识别为运行基础设施冲突', () => {
  assert.equal(
    isChannelModelDetectionInfrastructureConflict({
      isAxiosError: true,
      response: { status: 409 },
    }),
    true
  )
  assert.equal(
    isChannelModelDetectionInfrastructureConflict({
      isAxiosError: true,
      response: { status: 400 },
    }),
    false
  )
})

test('模型检测历史请求保留分页并省略空筛选参数', async () => {
  const originalAdapter = api.defaults.adapter
  const requests: AxiosRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config)
    return {
      data: {
        success: true,
        message: '',
        data: { page: 2, page_size: 20, total: 0, items: [] },
      },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter

  try {
    await getChannelModelDetectionRuns(801, {
      page: 2,
      page_size: 20,
      trigger: '',
      status: 'completed',
      model: '',
      outcome: 'juice_pass_fingerprint_strong',
    })
  } finally {
    api.defaults.adapter = originalAdapter
  }

  assert.equal(
    requests[0]?.url,
    '/api/channel_monitor/model_detection/channel/801/runs'
  )
  assert.equal(requests[0]?.method, 'get')
  assert.deepEqual(requests[0]?.params, {
    page: 2,
    page_size: 20,
    trigger: undefined,
    status: 'completed',
    model: undefined,
    outcome: 'juice_pass_fingerprint_strong',
  })
  assert.equal(requests[0]?.skipBusinessError, true)
  assert.equal(requests[0]?.skipErrorHandler, true)
})
