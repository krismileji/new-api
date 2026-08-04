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

test('keeps channel monitor dialogs content-sized until data overflows', () => {
  const fixturePath = fileURLToPath(
    new URL('./dialog-layout.fixture.tsx', import.meta.url)
  )
  const execution = spawnSync(process.execPath, [fixturePath], {
    cwd: process.cwd(),
    encoding: 'utf8',
  })

  assert.equal(
    execution.status,
    0,
    execution.stderr || execution.stdout || 'dialog layout fixture failed'
  )
})
