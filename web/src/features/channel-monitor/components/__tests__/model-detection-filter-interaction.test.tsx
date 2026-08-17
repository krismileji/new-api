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

test(
  '选择分组后列出该分组支持的模型并正确筛选渠道',
  { timeout: 15_000 },
  () => {
    const execution = runBunFixture(
      'src/features/channel-monitor/components/__tests__/model-detection-filter-interaction.fixture.tsx'
    )

    assert.equal(
      execution.status,
      0,
      execution.stderr || execution.stdout || '模型检测筛选交互校验失败'
    )
  }
)
