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
import { describe, it } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { ChannelMonitorSmartScheduleSampleDetails } from '../channel-monitor-smart-schedule-sample-details'
import { createSmartScheduleCellRoute } from './smart-schedule-cell-test-data'

describe('smart schedule shared sample details', () => {
  it('shows persisted degraded-probe recovery progress', () => {
    const route = createSmartScheduleCellRoute()
    const recoverySuccessAt = 1_752_777_840
    assert.ok(route.shared_samples)
    route.shared_samples.recovery_success_count = 2
    route.shared_samples.recovery_success_at = recoverySuccessAt

    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleSampleDetails
        route={route}
        performance={undefined}
        businessPerformance={undefined}
        stability={undefined}
        samples={undefined}
      />
    )

    assert.match(markup, /连续恢复成功<\/div><div[^>]*title="2 次">2 次<\/div>/)
    const formattedSuccessAt = formatTimestampToDate(recoverySuccessAt)
    assert.ok(markup.includes(`title="${formattedSuccessAt}"`))
  })

  it('shows placeholders before a degraded probe succeeds', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorSmartScheduleSampleDetails
        route={createSmartScheduleCellRoute()}
        performance={undefined}
        businessPerformance={undefined}
        stability={undefined}
        samples={undefined}
      />
    )

    assert.match(markup, /连续恢复成功<\/div><div[^>]*title="-">-<\/div>/)
    assert.match(markup, /最近恢复探测成功<\/div><div[^>]*title="-">-<\/div>/)
  })
})
