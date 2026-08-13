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

import { createSmartScheduleCellRoute } from './smart-schedule-cell-test-data'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelMonitorSmartScheduleCell } =
  await import('../channel-monitor-smart-schedule-cell')

const container = document.createElement('div')
document.body.append(container)
const root = createRoot(container)

function waitForTooltip(): Promise<HTMLElement> {
  const current = document.body.querySelector<HTMLElement>(
    '[data-slot="tooltip-content"]'
  )
  if (current) return Promise.resolve(current)

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      observer.disconnect()
      reject(new Error('部分参与模型 Tooltip 未在预期时间内显示'))
    }, 2000)
    const observer = new MutationObserver(() => {
      const tooltip = document.body.querySelector<HTMLElement>(
        '[data-slot="tooltip-content"]'
      )
      if (!tooltip) return
      clearTimeout(timeout)
      observer.disconnect()
      resolve(tooltip)
    })
    observer.observe(document.body, { childList: true, subtree: true })
  })
}

try {
  await act(async () => {
    root.render(
      <ChannelMonitorSmartScheduleCell
        channelName='测试渠道'
        routes={[
          createSmartScheduleCellRoute(),
          createSmartScheduleCellRoute({
            group: 'vip',
            model: 'model-b',
            state: {
              group: 'vip',
              model: 'model-b',
              excluded: true,
            },
          }),
        ]}
        selectedGroupModel={{ group: 'default', model: 'model-a' }}
        pending={false}
        onUpdate={() => undefined}
      />
    )
  })

  const trigger = container.querySelector<HTMLElement>(
    '[aria-label="查看未参与智能调度的模型：vip / model-b"]'
  )
  assert.ok(trigger)
  assert.equal(container.textContent?.includes('vip / model-b'), false)

  await act(async () => trigger.focus())
  const tooltip = await waitForTooltip()
  assert.ok(tooltip.textContent?.includes('未参与智能调度的模型'))
  assert.ok(tooltip.textContent?.includes('vip / model-b'))
} finally {
  await act(async () => root.unmount())
  container.remove()
}
