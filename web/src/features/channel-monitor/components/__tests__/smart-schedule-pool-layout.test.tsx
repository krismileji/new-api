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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import { ChannelMonitorSmartSchedulePoolLayout } from '../channel-monitor-smart-schedule-pool-layout'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

let wideViewport = false
Object.defineProperty(domWindow, 'matchMedia', {
  configurable: true,
  value: (query: string) => ({
    matches: query === '(min-width: 1536px)' && wideViewport,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => true,
  }),
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function createPoolItems() {
  return ['one', 'two', 'three', 'four', 'five'].map((pool) => (
    <article key={pool} data-pool={pool}>
      {pool}
    </article>
  ))
}

describe('smart schedule pool layout', () => {
  test('uses independent desktop columns so a tall card cannot push down the other column', async () => {
    wideViewport = true
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () =>
      root.render(
        <ChannelMonitorSmartSchedulePoolLayout>
          {createPoolItems()}
        </ChannelMonitorSmartSchedulePoolLayout>
      )
    )

    const columns = container.querySelectorAll<HTMLElement>(
      '[data-schedule-pool-column]'
    )
    assert.equal(columns.length, 2)
    assert.deepEqual(
      [...columns[0].querySelectorAll<HTMLElement>('[data-pool]')].map(
        (pool) => pool.dataset.pool
      ),
      ['one', 'three', 'five']
    )
    assert.deepEqual(
      [...columns[1].querySelectorAll<HTMLElement>('[data-pool]')].map(
        (pool) => pool.dataset.pool
      ),
      ['two', 'four']
    )

    await act(async () => root.unmount())
    container.remove()
  })

  test('keeps the configured card order in one column below the desktop breakpoint', async () => {
    wideViewport = false
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () =>
      root.render(
        <ChannelMonitorSmartSchedulePoolLayout>
          {createPoolItems()}
        </ChannelMonitorSmartSchedulePoolLayout>
      )
    )

    assert.equal(
      container.querySelectorAll('[data-schedule-pool-column]').length,
      0
    )
    assert.deepEqual(
      [...container.querySelectorAll<HTMLElement>('[data-pool]')].map(
        (pool) => pool.dataset.pool
      ),
      ['one', 'two', 'three', 'four', 'five']
    )

    await act(async () => root.unmount())
    container.remove()
  })
})

after(() => {
  domWindow.close()
})
