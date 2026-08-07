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

import type { ChannelMonitorSmartScheduleRouteState } from '../../types'
import { createSmartScheduleCellRoute } from './smart-schedule-cell-test-data'
import './test-dom'

const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelMonitorSmartScheduleCell } =
  await import('../channel-monitor-smart-schedule-cell')

const queryClient = new QueryClient()
const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

async function renderClearableState(
  state: Partial<ChannelMonitorSmartScheduleRouteState>
) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <ChannelMonitorSmartScheduleCell
          channelName='测试渠道'
          routes={[
            createSmartScheduleCellRoute({
              state,
            }),
          ]}
          selectedGroupModel={{ group: 'default', model: 'model-a' }}
          pending={false}
          onUpdate={() => undefined}
        />
      </QueryClientProvider>
    )
  })
}

async function cancelDialog() {
  const cancelButton = document.body.querySelector<HTMLButtonElement>(
    '[data-slot="alert-dialog-cancel"]'
  )
  assert.ok(cancelButton)
  await act(async () => {
    cancelButton.click()
  })
  assert.equal(
    document.body.querySelector('[data-slot="alert-dialog-content"]'),
    null
  )
}

try {
  await renderClearableState({ stability_state: 'degraded' })

  const degradedButton = container.querySelector<HTMLButtonElement>(
    'button[aria-label="解除 测试渠道 default model-a 的稳定性降级保护"]'
  )
  assert.ok(degradedButton)
  await act(async () => {
    degradedButton.click()
  })
  assert.ok(document.body.textContent?.includes('确认解除智能调度保护？'))
  assert.ok(document.body.textContent?.includes('稳定性降级'))
  await cancelDialog()

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <ChannelMonitorSmartScheduleCell
          channelName='测试渠道'
          routes={[createSmartScheduleCellRoute()]}
          selectedGroupModel={{ group: 'default', model: 'model-a' }}
          pending={false}
          onUpdate={() => undefined}
        />
      </QueryClientProvider>
    )
  })
  assert.equal(container.querySelector('button[aria-label^="解除 "]'), null)

  await renderClearableState({ stability_state: 'probing' })

  const probingButton = container.querySelector<HTMLButtonElement>(
    'button[aria-label="解除 测试渠道 default model-a 的稳定性释放"]'
  )
  assert.ok(probingButton)
  await act(async () => {
    probingButton.click()
  })
  assert.ok(document.body.textContent?.includes('确认解除智能调度保护？'))
  assert.ok(document.body.textContent?.includes('稳定性试放'))
  await cancelDialog()

  await renderClearableState({
    temporary_traffic_kind: 'insufficient_samples',
    temporary_traffic_target_percent: 3,
  })

  const explorationButton = container.querySelector<HTMLButtonElement>(
    'button[aria-label="解除 测试渠道 default model-a 的探索流量"]'
  )
  assert.ok(explorationButton)
  await act(async () => {
    explorationButton.click()
  })
  assert.ok(document.body.textContent?.includes('确认解除探索流量？'))
  assert.ok(document.body.textContent?.includes('探索流量状态'))
  await cancelDialog()

  await renderClearableState({
    temporary_traffic_kind: 'priority_sampling',
    temporary_traffic_target_percent: 2,
  })
  assert.equal(container.querySelector('button[aria-label^="解除 "]'), null)
} finally {
  await act(async () => root.unmount())
  queryClient.clear()
  container.remove()
}
