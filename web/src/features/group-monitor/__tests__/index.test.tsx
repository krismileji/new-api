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

import { GroupMonitorContent } from '../index'

describe('group monitor content', () => {
  test('keeps visible configured groups when monitoring is paused', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorContent
        result={{
          enabled: false,
          server_now: 1_752_777_900,
          data_cutoff_at: 1_752_777_840,
          display_value: 60,
          display_unit: 'minute',
          items: [
            {
              group: 'default',
              initial: 'D',
              status: 'paused',
              latest_first_token_ms: 215,
              success_rate: 100,
              last_finished_at: 1_752_777_840,
            },
          ],
        }}
      />
    )

    assert.ok(markup.includes('default'))
    assert.ok(markup.includes('已停用'))
    assert.ok(!markup.includes('分组监控暂未启用'))
  })
})
