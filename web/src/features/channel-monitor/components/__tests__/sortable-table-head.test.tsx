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
import { test } from 'node:test'

import { renderToStaticMarkup } from 'react-dom/server'

import { ChannelMonitorSortButton } from '../channel-monitor-sortable-table-head'

test('keeps sortable table labels horizontal when a metric column is narrow', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorSortButton
      label='缓存利用率'
      align='right'
      onSort={() => undefined}
    />
  )
  const button = markup.match(/^<button\b[^>]*>/)?.[0] ?? ''
  const label = markup.match(/<span\b[^>]*>/)?.[0] ?? ''

  assert.ok(button.includes('whitespace-nowrap'))
  assert.equal(button.includes('whitespace-normal'), false)
  assert.ok(label.includes('whitespace-nowrap'))
  assert.equal(label.includes('break-words'), false)
})
