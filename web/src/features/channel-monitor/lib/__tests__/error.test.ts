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

import { shouldReloadChannelMonitorSettings } from '../error'

function channelMonitorRequestError(status: number, message: string) {
  return Object.assign(new Error(message), {
    isAxiosError: true,
    response: { status, data: { message } },
  })
}

test('reloads settings after a concurrent configuration conflict', () => {
  assert.equal(
    shouldReloadChannelMonitorSettings(
      channelMonitorRequestError(409, '智能调度配置已被其他管理员更新')
    ),
    true
  )
})

test('reloads settings when the server reports a committed partial failure', () => {
  assert.equal(
    shouldReloadChannelMonitorSettings(
      channelMonitorRequestError(
        500,
        '设置已保存，但 429 冷却状态同步失败，请重试当前设置'
      )
    ),
    true
  )
})

test('keeps the current form for a normal request failure', () => {
  assert.equal(
    shouldReloadChannelMonitorSettings(
      channelMonitorRequestError(500, '保存智能调度设置失败')
    ),
    false
  )
})
