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
import { describe, test } from 'node:test'

import { Analytics01Icon } from '@hugeicons/core-free-icons'
import type { KeyboardEvent, ReactElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

import { MonitorStatCard } from '../../index'

const noop = () => {}

function createCard(onClick = noop) {
  return MonitorStatCard({
    label: '今日累计成本',
    value: '¥1.20',
    description: '按北京时间记录结算成本',
    icon: Analytics01Icon,
    ariaLabel: '查看每日成本',
    onClick,
  })
}

describe('channel monitor stat card', () => {
  test('makes the whole card keyboard-focusable when it has an action', () => {
    const markup = renderToStaticMarkup(createCard())

    assert.match(markup, /^<div\b[^>]*role="button"/)
    assert.ok(markup.includes('tabindex="0"'))
    assert.ok(markup.includes('aria-label="查看每日成本"'))
  })

  test('activates the whole card from Enter and Space', () => {
    const activated: string[] = []
    const element = createCard(() => activated.push('opened')) as ReactElement<{
      onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void
    }>

    for (const key of ['Enter', ' ']) {
      let defaultPrevented = false
      element.props.onKeyDown({
        key,
        preventDefault: () => {
          defaultPrevented = true
        },
      } as KeyboardEvent<HTMLDivElement>)
      assert.equal(defaultPrevented, true)
    }

    assert.deepEqual(activated, ['opened', 'opened'])
  })
})
