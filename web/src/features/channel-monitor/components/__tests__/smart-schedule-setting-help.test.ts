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

import { test } from 'vitest'

import { runBunFixture } from '@/test-utils/run-bun-fixture'

type SettingHelpResult = {
  focusShowsExplanation: boolean
  coversRequiredMetadata: boolean
  triggerAriaLabel: string | null
  triggerType: string
}

function runSettingHelpFixture(scenario: string): SettingHelpResult {
  const execution = runBunFixture(
    'src/features/channel-monitor/components/__tests__/smart-schedule-setting-help.fixture.tsx',
    [scenario]
  )

  assert.equal(
    execution.status,
    0,
    execution.stderr || execution.stdout || 'setting help fixture failed'
  )
  const output = execution.stdout.trim().split(/\r?\n/).at(-1)
  assert.ok(output)
  return JSON.parse(output) as SettingHelpResult
}

test(
  'shows first-token warning request help when its icon receives keyboard focus',
  { timeout: 15_000 },
  () => {
    const result = runSettingHelpFixture('first-token-warning')

    assert.equal(result.triggerType, 'button')
    assert.equal(result.triggerAriaLabel, '查看“首字告警请求占比”说明')
    assert.equal(result.focusShowsExplanation, true)
    assert.equal(result.coversRequiredMetadata, true)
  }
)

test(
  'explains the exploration-owned shared sampling order on keyboard focus',
  { timeout: 15_000 },
  () => {
    const result = runSettingHelpFixture('sampling-order')

    assert.equal(result.triggerType, 'button')
    assert.equal(result.triggerAriaLabel, '查看“统一采样顺序”说明')
    assert.equal(result.focusShowsExplanation, true)
    assert.equal(result.coversRequiredMetadata, true)
  }
)

test(
  'explains K Token conversion and bounds for exploration requests',
  { timeout: 15_000 },
  () => {
    const result = runSettingHelpFixture('exploration-prompt-k-tokens')

    assert.equal(result.triggerType, 'button')
    assert.equal(result.triggerAriaLabel, '查看“探索请求上限”说明')
    assert.equal(result.focusShowsExplanation, true)
    assert.equal(result.coversRequiredMetadata, true)
  }
)
