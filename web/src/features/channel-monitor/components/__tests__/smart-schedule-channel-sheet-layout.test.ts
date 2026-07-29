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
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

type ChannelSheetLayoutResult = {
  side: string | null
  title: string
  jitterDetailCount: number
  usesChannelDrawerLayout: boolean
}

test(
  'uses the same drawer shell as the channel editor',
  { timeout: 15_000 },
  () => {
    const fixturePath = fileURLToPath(
      new URL(
        './smart-schedule-channel-sheet-layout.fixture.tsx',
        import.meta.url
      )
    )
    const execution = spawnSync(process.execPath, [fixturePath], {
      cwd: process.cwd(),
      encoding: 'utf8',
    })

    assert.equal(
      execution.status,
      0,
      execution.stderr || execution.stdout || 'channel sheet fixture failed'
    )
    const output = execution.stdout.trim().split(/\r?\n/).at(-1)
    assert.ok(output)
    const result = JSON.parse(output) as ChannelSheetLayoutResult

    assert.equal(result.side, 'right')
    assert.match(result.title, /智能调度详情/)
    assert.match(result.title, /延迟抖动/)
    assert.match(result.title, /基线 300 ms/)
    assert.match(result.title, /P50 320 ms/)
    assert.match(result.title, /P95 950 ms/)
    assert.match(result.title, /阈值 1000 ms/)
    assert.match(result.title, /慢成功 7\/100 · 容忍 5 · 超额惩罚 2/)
    assert.equal(result.jitterDetailCount, 1)
    assert.equal(result.usesChannelDrawerLayout, true)
  }
)
