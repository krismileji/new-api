/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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

import { ChannelMonitorFieldInfo } from '../channel-monitor-field-info'

describe('channel monitor field info', () => {
  test('uses the same help trigger treatment as group policy settings', () => {
    const markup = renderToStaticMarkup(
      <ChannelMonitorFieldInfo
        label='数据保留'
        description='按配置周期分批清理到期数据。'
      />
    )
    const trigger = markup.match(
      /<button(?=[^>]*aria-label="查看“数据保留”说明")(?=[^>]*class="[^"]*size-5[^"]*")(?=[^>]*class="[^"]*rounded-sm[^"]*")(?=[^>]*class="[^"]*cursor-help[^"]*")[^>]*>/
    )?.[0]

    assert.ok(trigger)
  })
})
