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
import { describe, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import { renderToStaticMarkup } from 'react-dom/server'

import { ChannelStatusProbeView } from '../channel-status-probe-view'

const domWindow = new Window()
Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: domWindow,
})
Object.defineProperty(globalThis, 'document', {
  configurable: true,
  value: domWindow.document,
})
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: domWindow.localStorage,
})

describe('状态监测筛选布局', () => {
  test('桌面端将筛选项紧凑右对齐', () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    queryClient.setQueryData(
      ['channel-monitor', 'status-probe', { model: '' }],
      {
        success: true,
        message: '',
        data: {
          server_now: 1_700_000_000,
          scan_interval_seconds: 1,
          summary: {
            unconfigured: 0,
            paused: 0,
            pending: 0,
            healthy: 0,
            partial: 0,
            unhealthy: 0,
            rate_limited: 0,
            stale: 0,
          },
          groups: ['default', 'vip'],
          models: ['model-a'],
          models_by_group: {
            default: ['model-a'],
            vip: [],
          },
          channels: [],
        },
      }
    )

    domWindow.document.body.innerHTML = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <ChannelStatusProbeView />
      </QueryClientProvider>
    )

    const statusFilters = domWindow.document.querySelector(
      '[data-slot="status-probe-status-filters"]'
    )
    const filterControls = domWindow.document.querySelector(
      '[data-slot="status-probe-filter-controls"]'
    )
    assert.ok(statusFilters)
    assert.ok(filterControls)
    assert.match(
      statusFilters.querySelector('[data-slot="toggle-group"]')?.className ??
        '',
      /ml-auto/
    )
    assert.match(filterControls.className, /sm:justify-end/)
    assert.match(filterControls.className, /gap-2/)
    assert.match(filterControls.className, /flex-col/)
    assert.match(filterControls.className, /sm:flex-row/)
    assert.doesNotMatch(filterControls.className, /grid-cols/)

    const controlWidths = [
      ['选择状态探测分组', 'sm:w-40'],
      ['选择状态探测模型', 'sm:w-48'],
      ['状态探测卡片排序方式', 'sm:w-56'],
    ] as const
    for (const [label, widthClass] of controlWidths) {
      const control = domWindow.document.querySelector(
        `[aria-label="${label}"]`
      )
      assert.ok(control)
      assert.ok(control.className.includes('w-full'))
      assert.ok(control.className.includes(widthClass))
    }
    const modelControl = domWindow.document.querySelector(
      '[aria-label="选择状态探测模型"]'
    ) as HTMLButtonElement | null
    assert.ok(modelControl)
    assert.equal(modelControl.disabled, true)
    assert.equal(
      domWindow.document.querySelector('[aria-label="按样本设置筛选渠道"]'),
      null
    )

    const search = domWindow.document.querySelector(
      '[aria-label="搜索状态探测渠道"]'
    )
    assert.ok(search)
    assert.equal(search.getAttribute('placeholder'), '搜索渠道、备注或 ID')
    assert.match(search.parentElement?.className ?? '', /sm:w-72/)
  })
})
