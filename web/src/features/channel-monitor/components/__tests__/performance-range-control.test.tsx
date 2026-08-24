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

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, test } from 'vitest'

import { ChannelMonitorPerformanceRangeControl } from '../channel-monitor-performance-range-control'

const noop = () => {}

describe('channel monitor performance range control', () => {
  test('shows the authoritative smart schedule range without a numeric input', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorPerformanceRangeControl
        source='smart_schedule'
        rangeMinutes={60}
        inputValue='15'
        inputValid
        minMinutes={1}
        maxMinutes={1440}
        onInputChange={noop}
        onApply={noop}
      />
    )

    assert.ok(markup.includes('近 60 分钟'))
    assert.ok(markup.includes('由智能调度性能窗口决定'))
    assert.equal(markup.includes('type="number"'), false)
  })

  test('keeps the manual range editable while smart scheduling is inactive', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorPerformanceRangeControl
        source='manual'
        rangeMinutes={15}
        inputValue='30'
        inputValid
        minMinutes={1}
        maxMinutes={1440}
        onInputChange={noop}
        onApply={noop}
      />
    )

    assert.match(markup, /type="number"[^>]*min="1"[^>]*max="1440"/)
    assert.ok(markup.includes('value="30"'))
    assert.ok(markup.includes('性能与成功率统计范围（分钟）'))
  })
})
