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

type SettingsSurfaceResult = {
  generalHasSchedule: boolean
  generalTitle: string
  policyDialogBlocksHorizontalOverflow: boolean
  policyDialogCentered: boolean
  policyTableScrollable: boolean
  matchingDefaultPolicyVisible: boolean
  scheduleControlsAligned: boolean
  scheduleGroupsCompact: boolean
  scheduleSide: string | null
  scheduleTitle: string
  scheduleUsesUnifiedTransition: boolean
}

test('uses separate settings surfaces with a centered group policy editor', () => {
  const fixturePath = fileURLToPath(
    new URL('./channel-monitor-settings-surfaces.fixture.tsx', import.meta.url)
  )
  const execution = spawnSync(process.execPath, [fixturePath], {
    cwd: process.cwd(),
    encoding: 'utf8',
  })

  assert.equal(
    execution.status,
    0,
    execution.stderr || execution.stdout || 'settings surface fixture failed'
  )
  const output = execution.stdout.trim().split(/\r?\n/).at(-1)
  assert.ok(output)
  const result = JSON.parse(output) as SettingsSurfaceResult

  assert.match(result.generalTitle, /渠道监控设置/)
  assert.equal(result.generalHasSchedule, false)
  assert.equal(result.scheduleSide, 'right')
  assert.match(result.scheduleTitle, /智能调度设置/)
  assert.equal(result.scheduleUsesUnifiedTransition, true)
  assert.equal(result.scheduleControlsAligned, true)
  assert.equal(result.scheduleGroupsCompact, true)
  assert.equal(result.policyDialogCentered, true)
  assert.equal(result.policyDialogBlocksHorizontalOverflow, true)
  assert.equal(result.policyTableScrollable, true)
  assert.equal(result.matchingDefaultPolicyVisible, true)
})
