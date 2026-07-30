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
import { describe, test } from 'node:test'

import { renderToStaticMarkup } from 'react-dom/server'

import { ChannelDefaultRoutingLabel } from '../channel-default-routing-label'

describe('channel default routing label', () => {
  test('shows the default-value label with keyboard-accessible help', () => {
    const markup = renderToStaticMarkup(
      <ChannelDefaultRoutingLabel field='priority' />
    )

    assert.match(markup, /默认优先级/)
    assert.match(markup, /aria-label="查看“默认优先级”说明"/)
    assert.match(markup, /type="button"/)
  })
})
