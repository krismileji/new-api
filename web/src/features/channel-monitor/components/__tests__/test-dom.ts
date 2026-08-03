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
import { Window } from 'happy-dom'

const TEST_DOM_KEY = '__channel_monitor_test_dom__'

type ChannelMonitorTestDom = {
  window: Window
}

const testGlobals = globalThis as typeof globalThis & {
  [TEST_DOM_KEY]?: ChannelMonitorTestDom
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

const domWindow =
  testGlobals[TEST_DOM_KEY]?.window ??
  (() => {
    const window = new Window()
    const domGlobals = [
      'window',
      'document',
      'navigator',
      'Document',
      'DocumentFragment',
      'HTMLElement',
      'HTMLButtonElement',
      'HTMLFormElement',
      'HTMLInputElement',
      'HTMLSelectElement',
      'SVGElement',
      'Node',
      'NodeFilter',
      'Element',
      'ShadowRoot',
      'Event',
      'InputEvent',
      'FocusEvent',
      'KeyboardEvent',
      'MouseEvent',
      'CustomEvent',
      'MutationObserver',
      'ResizeObserver',
      'FormData',
      'requestAnimationFrame',
      'cancelAnimationFrame',
      'getComputedStyle',
    ] as const

    for (const key of domGlobals) {
      Object.defineProperty(globalThis, key, {
        configurable: true,
        value: window[key],
      })
    }
    Object.defineProperty(window.Element.prototype, 'getAnimations', {
      configurable: true,
      value: () => [],
    })
    testGlobals[TEST_DOM_KEY] = { window }
    return window
  })()

testGlobals.IS_REACT_ACT_ENVIRONMENT = true

export { domWindow }
