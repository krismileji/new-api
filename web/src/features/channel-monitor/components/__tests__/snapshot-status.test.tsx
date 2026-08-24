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
import { test } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { ChannelMonitorSnapshotStatus } from '../channel-monitor-snapshot-status'

test('shows task snapshot freshness independently from polling frequency', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorSnapshotStatus
      generatedAt={1_752_777_845}
      dataCutoffAt={1_752_777_840}
      eventWatermark={42}
      snapshotAgeSeconds={15}
      stale
    />
  )

  assert.ok(markup.includes('任务快照已过期'))
  assert.ok(markup.includes(formatTimestampToDate(1_752_777_845)))
  assert.ok(markup.includes(formatTimestampToDate(1_752_777_840)))
  assert.ok(markup.includes('已处理事件序号 42'))
  assert.ok(markup.includes('快照年龄 15 秒'))
})
