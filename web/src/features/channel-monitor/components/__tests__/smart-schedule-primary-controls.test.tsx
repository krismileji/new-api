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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import type { ChannelMonitorSmartScheduleRoute } from '../../types'
import {
  ChannelMonitorSmartSchedulePrimaryControls,
  ChannelMonitorSmartSchedulePrimaryStabilityField,
} from '../channel-monitor-smart-schedule-primary-controls'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const fixedRoute = {
  channel_id: 7,
  channel_name: '缓存主渠道',
  channel_status: 1,
  channel_priority: 100,
  channel_weight: 900,
  group: 'vip',
  model: 'cache-model',
  enabled: true,
  priority: 100,
  weight: 900,
  shared_samples: {
    id: 0,
    channel_id: 7,
    model: 'cache-model',
    window_start: 0,
    last_time: 0,
    last_success: false,
    last_error: '',
    sample_count: 0,
    success_count: 0,
    failure_duration_sample_count: 0,
    average_failure_duration_ms: null,
    first_token_sample_count: 0,
    average_first_token_ms: null,
    tps_sample_count: 0,
    average_tps: null,
  },
  state: {
    id: 1,
    channel_id: 7,
    group: 'vip',
    model: 'cache-model',
    participation_set: true,
    excluded: false,
    last_schedule_status: 'succeeded',
    last_schedule_error: '',
    last_schedule_score: 0.98,
    last_schedule_priority: 100,
    last_schedule_weight: 900,
    last_schedule_time: 1_752_777_845,
    stability_state: '',
    stability_until: 0,
    stability_since: 0,
    stability_saved_priority: 0,
    stability_saved_weight: 0,
    runtime_protection_until: 0,
    base_rank: 1,
    base_priority: 100,
    base_weight: 900,
    temporary_traffic_kind: '',
    temporary_traffic_since: 0,
    temporary_traffic_target_percent: 0,
    last_priority_sample_time: 0,
    manual_primary_until: 1_752_800_000,
    manual_primary_allow_stability_degrade: false,
  },
} satisfies ChannelMonitorSmartScheduleRoute

async function renderControls(options: {
  onEdit?: (route: ChannelMonitorSmartScheduleRoute) => void
  onClear?: (route: ChannelMonitorSmartScheduleRoute) => void
}) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <ChannelMonitorSmartSchedulePrimaryControls
        route={fixedRoute}
        disabled={false}
        onEdit={options.onEdit ?? (() => {})}
        onClear={options.onClear ?? (() => {})}
      />
    )
  })
  return { container, root }
}

describe('smart schedule fixed primary controls', () => {
  test('opens fixed-minute editing from an already fixed channel', async () => {
    const editedRoutes: ChannelMonitorSmartScheduleRoute[] = []
    const rendered = await renderControls({
      onEdit: (route) => editedRoutes.push(route),
    })

    assert.ok(rendered.container.textContent?.includes('管理员固定至'))
    assert.ok(rendered.container.textContent?.includes('固定期间不降级'))
    const editButton = rendered.container.querySelector<HTMLButtonElement>(
      '[aria-label="重新设置 缓存主渠道 的固定时长"]'
    )
    assert.ok(editButton)
    await act(async () => editButton.click())
    assert.deepEqual(editedRoutes, [fixedRoute])

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  test('clears the fixed primary channel from its visible action', async () => {
    const clearedRoutes: ChannelMonitorSmartScheduleRoute[] = []
    const rendered = await renderControls({
      onClear: (route) => clearedRoutes.push(route),
    })

    const clearButton = rendered.container.querySelector<HTMLButtonElement>(
      '[aria-label="解除 缓存主渠道 的主渠道固定"]'
    )
    assert.ok(clearButton)
    assert.equal(clearButton.textContent?.includes('解除固定'), true)
    await act(async () => clearButton.click())
    assert.deepEqual(clearedRoutes, [fixedRoute])

    await act(async () => rendered.root.unmount())
    rendered.container.remove()
  })

  test('turns off the default enabled stability degradation switch', async () => {
    const changes: boolean[] = []
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(
        <ChannelMonitorSmartSchedulePrimaryStabilityField
          checked
          onCheckedChange={(checked) => changes.push(checked)}
        />
      )
    })

    const stabilitySwitch = container.querySelector<HTMLButtonElement>(
      '[aria-label="固定期间允许稳定性降级"]'
    )
    assert.ok(stabilitySwitch)
    assert.equal(stabilitySwitch.getAttribute('aria-checked'), 'true')
    await act(async () => stabilitySwitch.click())
    assert.deepEqual(changes, [false])

    await act(async () => root.unmount())
    container.remove()
  })
})

after(() => {
  domWindow.close()
})
