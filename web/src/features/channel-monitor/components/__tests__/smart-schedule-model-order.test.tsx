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

import { useState } from 'react'
import { describe, test } from 'vitest'

import { ChannelMonitorSmartScheduleModelOrder } from '../channel-monitor-smart-schedule-model-order'
import './test-dom'

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function getRenderedModels(container: HTMLElement): string[] {
  return [...container.querySelectorAll<HTMLElement>('[data-model]')].map(
    (element) => element.dataset.model ?? ''
  )
}

describe('smart schedule model card order', () => {
  test('moves models with accessible controls and disables unavailable moves', async () => {
    const changes: string[][] = []
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    function Fixture() {
      const [value, setValue] = useState<string[]>([])
      return (
        <ChannelMonitorSmartScheduleModelOrder
          models={['model-gamma', 'model-alpha', 'model-beta']}
          value={value}
          onChange={(nextValue) => {
            changes.push(nextValue)
            setValue(nextValue)
          }}
        />
      )
    }

    await act(async () => root.render(<Fixture />))

    assert.deepEqual(getRenderedModels(container), [
      'model-alpha',
      'model-beta',
      'model-gamma',
    ])
    const firstUpButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="上移模型 model-alpha"]'
    )
    const lastDownButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="下移模型 model-gamma"]'
    )
    assert.ok(firstUpButton?.disabled)
    assert.ok(lastDownButton?.disabled)

    const betaUpButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="上移模型 model-beta"]'
    )
    assert.ok(betaUpButton)
    await act(async () => betaUpButton.click())

    assert.deepEqual(changes.at(-1), [
      'model-beta',
      'model-alpha',
      'model-gamma',
    ])
    assert.deepEqual(getRenderedModels(container), [
      'model-beta',
      'model-alpha',
      'model-gamma',
    ])

    await act(async () => root.unmount())
    container.remove()
  })

  test('keeps configured models first and appends new models by name', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () =>
      root.render(
        <ChannelMonitorSmartScheduleModelOrder
          models={['model-beta', 'model-alpha', 'model-zeta', 'model-gamma']}
          value={['model-zeta', 'model-beta']}
          onChange={() => {}}
        />
      )
    )

    assert.deepEqual(getRenderedModels(container), [
      'model-zeta',
      'model-beta',
      'model-alpha',
      'model-gamma',
    ])

    await act(async () => root.unmount())
    container.remove()
  })
})
