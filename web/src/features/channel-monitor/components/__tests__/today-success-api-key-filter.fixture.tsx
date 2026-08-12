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

import type { ChannelMonitorTodaySuccessResult } from '../../types'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelMonitorTodaySuccessCard } =
  await import('../channel-monitor-today-success-card')

const result: ChannelMonitorTodaySuccessResult = {
  days: 1,
  generated_at: 1_752_777_845,
  day_start: 1_752_681_600,
  detail_date: '2026-07-23',
  success_metrics_available: true,
  cache_write_metrics_available: true,
  summary: {
    actual_success_count: 9,
    actual_failure_count: 1,
    actual_sample_count: 10,
    actual_success_rate: 0.9,
    final_success_count: 9,
    final_failure_count: 1,
    final_sample_count: 10,
    final_success_rate: 0.9,
    cache_hit_count: 1,
    cache_sample_count: 2,
    cache_hit_rate: 0.5,
  },
  channel_items: [],
  api_key_items: [
    {
      api_key_id: 21,
      api_key_name: '生产 Key',
      actual_success_count: 3,
      actual_failure_count: 1,
      actual_sample_count: 4,
      actual_success_rate: 0.75,
      final_success_count: 3,
      final_failure_count: 1,
      final_sample_count: 4,
      final_success_rate: 0.75,
      cache_hit_count: 1,
      cache_sample_count: 4,
      cache_hit_rate: 0.25,
    },
  ],
  cache_write_items: [],
  chart_items: [],
}

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)
let detailOpenCount = 0

try {
  await act(async () => {
    root.render(
      <ChannelMonitorTodaySuccessCard
        result={result}
        isLoading={false}
        isError={false}
        onOpen={() => {
          detailOpenCount += 1
        }}
      />
    )
  })

  const cacheRate = container.querySelector<HTMLElement>(
    '[data-slot="today-cache-rate"]'
  )
  const trigger = container.querySelector<HTMLButtonElement>(
    '[aria-label="选择缓存率 API Key"]'
  )
  assert.ok(cacheRate)
  assert.ok(trigger)
  assert.equal(cacheRate.textContent, '50%')
  assert.ok(trigger.textContent?.includes('全部 API Key'))

  const detailHeader = container.querySelector<HTMLElement>(
    '[data-slot="card-header"]'
  )
  assert.ok(detailHeader)
  for (const [key, code] of [
    ['Enter', 'Enter'],
    [' ', 'Space'],
  ]) {
    await act(async () => {
      detailHeader.dispatchEvent(
        new KeyboardEvent('keydown', {
          key,
          code,
          bubbles: true,
        })
      )
    })
  }
  assert.equal(detailOpenCount, 2)

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
  const productionKeyOption = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ].find((option) => option.textContent?.trim() === '生产 Key · ID 21')
  assert.ok(productionKeyOption)
  await act(async () => productionKeyOption.click())

  assert.equal(cacheRate.textContent, '25%')
  assert.ok(trigger.textContent?.includes('生产 Key · ID 21'))
  assert.ok(container.textContent?.includes('4 次缓存样本'))
  assert.equal(detailOpenCount, 2)
} finally {
  await act(async () => root.unmount())
  container.remove()
}

process.stdout.write('ok\n')
