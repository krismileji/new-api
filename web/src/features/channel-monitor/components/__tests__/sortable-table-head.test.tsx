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
import { test } from 'vitest'

import {
  ChannelMonitorSortButton,
  ChannelMonitorSortableTableHead,
} from '../channel-monitor-sortable-table-head'

test('keeps sortable table labels and indicators inside a narrow metric column', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorSortButton
      label='缓存利用率'
      align='right'
      onSort={() => undefined}
    />
  )
  const button = markup.match(/^<button\b[^>]*>/)?.[0] ?? ''
  const label = markup.match(/<span\b[^>]*>/)?.[0] ?? ''

  assert.ok(button.includes('min-w-0'))
  assert.ok(button.includes('overflow-hidden'))
  assert.ok(button.includes('grid-cols-[minmax(0,1fr)_auto]'))
  assert.ok(label.includes('truncate'))
  assert.match(markup, /<svg[^>]*class="[^"]*shrink-0/)
})

test('can keep inactive sort indicators quiet until the header is focused', () => {
  const markup = renderToStaticMarkup(
    <ChannelMonitorSortButton
      label='解析率'
      align='right'
      subtleUnsortedIcon
      onSort={() => undefined}
    />
  )
  const icon = markup.match(/<svg\b[^>]*>/)?.[0] ?? ''

  assert.ok(icon.includes('opacity-0'))
  assert.ok(icon.includes('group-hover/sort:opacity-60'))
  assert.ok(icon.includes('group-focus-visible/sort:opacity-60'))
})

test('keeps column sizing classes on the table head instead of shrinking the sort button', () => {
  const markup = renderToStaticMarkup(
    <table>
      <thead>
        <tr>
          <ChannelMonitorSortableTableHead
            label='缓存利用率'
            align='right'
            className='w-[12%] px-1 text-xs'
            onSort={() => undefined}
          />
        </tr>
      </thead>
    </table>
  )
  const head = markup.match(/<th\b[^>]*>/)?.[0] ?? ''
  const button = markup.match(/<button\b[^>]*>/)?.[0] ?? ''

  assert.ok(head.includes('w-[12%]'))
  assert.equal(button.includes('w-[12%]'), false)
})
