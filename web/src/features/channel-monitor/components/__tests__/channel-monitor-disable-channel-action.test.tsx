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

import type { AxiosAdapter, AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, test } from 'vitest'

import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient } = await import('@tanstack/react-query')
const { toast } = await import('sonner')
const { api } = await import('@/lib/api')
const { channelsQueryKeys } =
  await import('@/features/channels/lib/channel-actions')
const { ChannelMonitorDisableChannelAction } =
  await import('../channel-monitor-disable-channel-action')

const originalAdapter = api.defaults.adapter
let host: HTMLDivElement | null = null
let root: ReturnType<typeof createRoot> | null = null
let queryClient: InstanceType<typeof QueryClient>

function success(config: AxiosRequestConfig) {
  return {
    data: { success: true },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  }
}

async function renderAction(props?: {
  channelStatus?: number
  onStatusChanged?: () => void
}) {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await act(async () => {
    root?.render(
      <ChannelMonitorDisableChannelAction
        channelId={42}
        channelStatus={props?.channelStatus ?? 1}
        queryClient={queryClient}
        onStatusChanged={props?.onStatusChanged}
      />
    )
  })
}

function getButton(label: string) {
  const button = document.querySelector<HTMLButtonElement>(
    `button[aria-label="${label}"]`
  )
  assert.ok(button, `Expected button "${label}"`)
  return button
}

beforeEach(() => {
  document.body.replaceChildren()
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
})

afterEach(async () => {
  api.defaults.adapter = originalAdapter
  if (root) {
    await act(async () => root?.unmount())
  }
  root = null
  host?.remove()
  host = null
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  document.body.replaceChildren()
})

describe('渠道监控渠道状态操作', () => {
  test('点击后立即以手动禁用状态更新渠道并刷新监测数据', async () => {
    const requests: AxiosRequestConfig[] = []
    let refreshCount = 0
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      return success(config)
    }) as AxiosAdapter
    await renderAction({
      onStatusChanged: () => {
        refreshCount++
      },
    })
    queryClient.setQueryData(channelsQueryKeys.lists(), { items: [] })

    assert.match(getButton('禁用渠道').className, /text-destructive/)
    assert.ok(
      getButton('禁用渠道').querySelector('svg.lucide-power-off'),
      'Expected the standard disable icon'
    )
    await act(async () => {
      getButton('禁用渠道').click()
      await Promise.resolve()
    })

    const request = requests.find(
      (candidate) => candidate.url === '/api/channel/42/status'
    )
    assert.ok(request)
    assert.equal(request.method, 'post')
    assert.deepEqual(JSON.parse(String(request.data)), { status: 2 })
    assert.equal(refreshCount, 1)
    assert.equal(
      queryClient.getQueryState(channelsQueryKeys.lists())?.isInvalidated,
      true
    )
    assert.doesNotMatch(document.body.textContent ?? '', /确认禁用/)
  })

  test('接口拒绝禁用时不刷新数据且不显示确认弹层', async () => {
    let refreshCount = 0
    api.defaults.adapter = (async (config) => ({
      data: { success: false, message: '渠道状态已变更' },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })) as AxiosAdapter
    await renderAction({
      onStatusChanged: () => {
        refreshCount++
      },
    })

    await act(async () => {
      getButton('禁用渠道').click()
      await Promise.resolve()
    })

    assert.doesNotMatch(document.body.textContent ?? '', /确认禁用/)
    assert.equal(refreshCount, 0)
  })

  test('已禁用渠道点击后立即启用并恢复为启用状态', async () => {
    const requests: AxiosRequestConfig[] = []
    let refreshCount = 0
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      return success(config)
    }) as AxiosAdapter
    await renderAction({
      channelStatus: 2,
      onStatusChanged: () => {
        refreshCount++
      },
    })

    assert.equal(getButton('启用渠道').disabled, false)
    assert.match(getButton('启用渠道').className, /text-success/)
    assert.ok(
      getButton('启用渠道').querySelector('svg.lucide-power'),
      'Expected the standard enable icon'
    )
    await act(async () => {
      getButton('启用渠道').click()
      await Promise.resolve()
    })

    const request = requests.find(
      (candidate) => candidate.url === '/api/channel/42/status'
    )
    assert.ok(request)
    assert.deepEqual(JSON.parse(String(request.data)), { status: 1 })
    assert.equal(refreshCount, 1)
  })
})
