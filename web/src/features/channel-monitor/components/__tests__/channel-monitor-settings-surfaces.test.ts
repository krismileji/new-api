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
  allNotificationTypesSelected: boolean
  conflictExecutionsQueryInvalidated: boolean
  conflictFormClosed: boolean
  conflictHistoryQueryInvalidated: boolean
  conflictMonitorQueryInvalidated: boolean
  generalHasSchedule: boolean
  generalTitle: string
  generalUsesContentSizedViewport: boolean
  legacyRetentionDefaultsApplied: boolean
  notificationTypeCanBeUnchecked: boolean
  policyDialogBlocksHorizontalOverflow: boolean
  policyDialogCentered: boolean
  policyDialogUsesContentSizedViewport: boolean
  policyDialogExplainsExplicitScope: boolean
  policyDialogHasGroupSettingHelp: boolean
  policyDialogHasCompletePolicyControls: boolean
  policyDialogHidesLegacyDegradeScore: boolean
  policyDialogHasNoLegacyWeightControls: boolean
  policyDialogUsesInsetRing: boolean
  policyDialogStabilityInputsAligned: boolean
  policyListAvoidsHorizontalOverflow: boolean
  previewEmailButtonEnabled: boolean
  newPolicyVisible: boolean
  scheduleHasExplicitPolicyScope: boolean
  scheduleHasNoImplicitPolicyControls: boolean
  scheduleSide: string | null
  scheduleTitle: string
  scheduleUsesChannelDrawerLayout: boolean
  scheduleUsesUnifiedTransition: boolean
}

function runSettingsSurfaceFixture() {
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
  return JSON.parse(output) as SettingsSurfaceResult
}

test(
  'uses separate settings surfaces with a centered group policy editor',
  { timeout: 15_000 },
  () => {
    const result = runSettingsSurfaceFixture()

    assert.match(result.generalTitle, /渠道监控设置/)
    assert.match(result.generalTitle, /上游请求超时/)
    assert.match(result.generalTitle, /错误信息映射/)
    assert.equal(result.generalHasSchedule, false)
    assert.equal(result.generalUsesContentSizedViewport, true)
    assert.equal(result.legacyRetentionDefaultsApplied, true)
    assert.equal(result.allNotificationTypesSelected, true)
    assert.equal(result.notificationTypeCanBeUnchecked, true)
    assert.equal(result.previewEmailButtonEnabled, true)
    assert.equal(result.scheduleSide, 'right')
    assert.match(result.scheduleTitle, /智能调度设置/)
    assert.equal(result.scheduleUsesChannelDrawerLayout, true)
    assert.equal(result.scheduleUsesUnifiedTransition, true)
    assert.equal(result.scheduleHasExplicitPolicyScope, true)
    assert.equal(result.scheduleHasNoImplicitPolicyControls, true)
    assert.equal(result.policyDialogCentered, true)
    assert.equal(result.policyDialogUsesContentSizedViewport, true)
    assert.equal(result.policyDialogBlocksHorizontalOverflow, true)
    assert.equal(result.policyDialogHasCompletePolicyControls, true)
    assert.equal(result.policyDialogHidesLegacyDegradeScore, true)
    assert.equal(result.policyDialogHasNoLegacyWeightControls, true)
    assert.equal(result.policyDialogUsesInsetRing, true)
    assert.equal(result.policyDialogStabilityInputsAligned, true)
    assert.equal(result.policyDialogExplainsExplicitScope, true)
    assert.equal(result.policyDialogHasGroupSettingHelp, true)
    assert.equal(result.policyListAvoidsHorizontalOverflow, true)
    assert.equal(result.newPolicyVisible, true)
  }
)

test(
  'closes stale settings and invalidates related data after a revision conflict',
  { timeout: 15_000 },
  () => {
    const result = runSettingsSurfaceFixture()

    assert.equal(result.conflictFormClosed, true)
    assert.equal(result.conflictMonitorQueryInvalidated, true)
    assert.equal(result.conflictExecutionsQueryInvalidated, true)
    assert.equal(result.conflictHistoryQueryInvalidated, true)
  }
)
