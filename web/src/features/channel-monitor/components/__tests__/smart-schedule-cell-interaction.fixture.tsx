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

try {
  await act(async () => {
    root.render(
      <ChannelMonitorSmartScheduleCell
        channelName='测试渠道'
        routes={[
          createSmartScheduleCellRoute({
            state: {
              temporary_traffic_kind: 'insufficient_samples',
              temporary_traffic_since: 1_752_700_000,
              temporary_traffic_target_percent: 3,
              last_schedule_error: '样本不足，临时提升到最高优先级',
            },
          }),
        ]}
        selectedGroupModel={{ group: 'default', model: 'model-a' }}
        pending={false}
        onUpdate={() => undefined}
      />
    )
  })

  const trigger = container.querySelector<HTMLButtonElement>(
    'button[aria-label^="查看当前调度状态详情"]'
  )
  assert.ok(trigger)
  await act(async () => {
    trigger.click()
  })

  assert.ok(document.body.textContent?.includes('当前调度状态'))
  assert.ok(document.body.textContent?.includes('目标流量'))
  assert.ok(
    document.body.textContent?.includes('样本不足，临时提升到最高优先级')
  )
} finally {
  await act(async () => root.unmount())
  container.remove()
}
