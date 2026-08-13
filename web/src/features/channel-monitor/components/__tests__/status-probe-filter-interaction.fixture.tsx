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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AxiosAdapter } from 'axios'

import { api } from '@/lib/api'

import type {
  ChannelMonitorApiResponse,
  ChannelStatusProbeChannel,
  ChannelStatusProbeOverview,
} from '../../types'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelStatusProbeView } = await import('../channel-status-probe-view')

function createChannel(
  id: number,
  name: string,
  groups: string[],
  models: string[]
): ChannelStatusProbeChannel {
  return {
    id,
    name,
    type: 1,
    channel_status: 1,
    remark: '',
    groups,
    cost_ratio: 1,
    supported_models: models,
    allows_custom_model: false,
    config: {
      id,
      channel_id: id,
      enabled: true,
      models,
      interval_seconds: 300,
      display_value: 60,
      display_unit: 'minute',
      record_sample: false,
      next_run_at: 0,
      manual_request_id: '',
      manual_requested_at: 0,
      revision: 1,
      running_trigger: '',
      running_run_id: '',
      running_started_at: 0,
      created_at: 1,
      updated_at: 1,
    },
    health_status: 'pending',
    running: false,
    latest: null,
    avg_first_token_ms: null,
    avg_tps: null,
    model_statuses: [],
    configured_model_count: models.length,
  }
}

const overview: ChannelMonitorApiResponse<ChannelStatusProbeOverview> = {
  success: true,
  message: '',
  data: {
    server_now: 1_700_000_000,
    scan_interval_seconds: 1,
    summary: {
      unconfigured: 1,
      paused: 0,
      pending: 2,
      healthy: 0,
      partial: 0,
      unhealthy: 0,
      rate_limited: 0,
      stale: 0,
    },
    groups: ['default', 'vip'],
    models: ['model-a', 'model-b', 'model-c'],
    models_by_group: {
      default: ['model-a'],
      vip: ['model-b', 'model-c'],
    },
    channels: [
      createChannel(1, '默认渠道', ['default'], ['model-a']),
      createChannel(2, 'VIP 渠道', ['vip'], ['model-b', 'model-c']),
      {
        ...createChannel(3, '未配置渠道', ['default'], []),
        config: null,
        health_status: 'unconfigured',
        configured_model_count: 0,
      },
    ],
  },
}

async function chooseOption(trigger: HTMLButtonElement, label: string) {
  await act(async () => {
    trigger.focus()
    trigger.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      })
    )
  })
  const option = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ].find((candidate) => candidate.textContent?.trim() === label)
  assert.ok(option)
  await act(async () => option.click())
}

let resolveModelRequest: (() => void) | undefined
const originalAdapter = api.defaults.adapter
const pendingAdapter: AxiosAdapter = (config) =>
  new Promise((resolve) => {
    assert.equal(config.url, '/api/channel_monitor/status')
    assert.equal(config.params?.model, 'model-b')
    resolveModelRequest = () =>
      resolve({
        config,
        data: overview,
        headers: {},
        status: 200,
        statusText: 'OK',
      })
  })
api.defaults.adapter = pendingAdapter

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
  },
})
queryClient.setQueryData(
  ['channel-monitor', 'status-probe', { model: '' }],
  overview
)

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

try {
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <ChannelStatusProbeView />
      </QueryClientProvider>
    )
  })

  const groupTrigger = container.querySelector<HTMLButtonElement>(
    '[aria-label="选择状态探测分组"]'
  )
  const modelTrigger = container.querySelector<HTMLButtonElement>(
    '[aria-label="选择状态探测模型"]'
  )
  assert.ok(groupTrigger)
  assert.ok(modelTrigger)
  const onlyConfigured = container.querySelector<HTMLElement>(
    '[aria-label="仅展示已配置的状态探测卡片"]'
  )
  assert.ok(onlyConfigured)
  assert.equal(onlyConfigured.getAttribute('aria-checked'), 'true')
  assert.equal(container.textContent?.includes('未配置渠道'), false)
  await act(async () => onlyConfigured.click())
  assert.equal(onlyConfigured.getAttribute('aria-checked'), 'false')
  assert.ok(container.textContent?.includes('未配置渠道'))
  assert.equal(modelTrigger.disabled, true)
  assert.ok(modelTrigger.textContent?.includes('请先选择分组'))

  await chooseOption(groupTrigger, 'vip')
  assert.ok(groupTrigger.textContent?.includes('vip'))
  assert.equal(modelTrigger.disabled, false)
  assert.equal(container.textContent?.includes('默认渠道'), false)
  assert.ok(container.textContent?.includes('VIP 渠道'))

  await act(async () => {
    modelTrigger.focus()
    modelTrigger.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      })
    )
  })
  const modelOptions = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ]
  assert.deepEqual(
    modelOptions.map((option) => option.textContent?.trim()),
    ['全部模型', 'model-b', 'model-c']
  )
  const modelBOption = modelOptions.find(
    (option) => option.textContent?.trim() === 'model-b'
  )
  assert.ok(modelBOption)
  await act(async () => modelBOption.click())
  assert.ok(resolveModelRequest)
  assert.ok(groupTrigger.textContent?.includes('vip'))
  assert.ok(modelTrigger.textContent?.includes('model-b'))
  assert.equal(container.textContent?.includes('默认渠道'), false)
  assert.ok(container.textContent?.includes('VIP 渠道'))

  await act(async () => {
    resolveModelRequest?.()
  })
  assert.ok(groupTrigger.textContent?.includes('vip'))
  assert.ok(modelTrigger.textContent?.includes('model-b'))

  await chooseOption(groupTrigger, 'default')
  assert.ok(groupTrigger.textContent?.includes('default'))
  assert.ok(modelTrigger.textContent?.includes('全部模型'))
  await act(async () => {
    modelTrigger.focus()
    modelTrigger.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      })
    )
  })
  const defaultGroupModelOptions = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ]
  assert.deepEqual(
    defaultGroupModelOptions.map((option) => option.textContent?.trim()),
    ['全部模型', 'model-a']
  )
} finally {
  api.defaults.adapter = originalAdapter
  await act(async () => root.unmount())
  container.remove()
  queryClient.clear()
}

process.stdout.write('ok\n')
