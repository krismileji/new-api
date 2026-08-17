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
import { afterAll, describe, test } from 'vitest'

import { Window } from 'happy-dom'
import { renderToStaticMarkup } from 'react-dom/server'

import {
  ChannelMonitorSmartScheduleExecutionAdjustments,
  ChannelMonitorSmartScheduleExecutionLayout,
} from '../channel-monitor-smart-schedule-execution-layout'

const domWindow = new Window()

describe('smart schedule execution responsive layout', () => {
  test('keeps mobile task selection bounded while the complete detail pane remains scrollable', () => {
    domWindow.document.body.innerHTML = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleExecutionLayout
        taskList={<div>执行批次</div>}
      >
        <div>执行概要</div>
        <div>筛选条件</div>
        <ChannelMonitorSmartScheduleExecutionAdjustments>
          渠道执行明细
        </ChannelMonitorSmartScheduleExecutionAdjustments>
      </ChannelMonitorSmartScheduleExecutionLayout>
    )

    const layout = domWindow.document.querySelector(
      '[data-schedule-execution-layout]'
    ) as HTMLElement | null
    const details = domWindow.document.querySelector(
      '[data-schedule-execution-details]'
    ) as HTMLElement | null
    const adjustments = domWindow.document.querySelector(
      '[data-schedule-execution-adjustments]'
    ) as HTMLElement | null

    assert.ok(layout)
    assert.ok(details)
    assert.ok(adjustments)
    assert.ok(layout.classList.contains('grid-rows-[13rem_minmax(0,1fr)]'))
    assert.ok(layout.classList.contains('lg:grid-rows-1'))
    assert.ok(details.classList.contains('overflow-y-auto'))
    assert.ok(details.classList.contains('lg:overflow-hidden'))
    assert.ok(adjustments.classList.contains('shrink-0'))
    assert.ok(adjustments.classList.contains('lg:min-h-0'))
    assert.ok(adjustments.classList.contains('lg:flex-1'))
    assert.ok(adjustments.classList.contains('lg:overflow-y-auto'))
  })
})

afterAll(() => {
  domWindow.close()
})
