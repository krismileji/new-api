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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Window } from 'happy-dom'
import { createInstance } from 'i18next'
import type { ReactElement } from 'react'
import { I18nextProvider } from 'react-i18next'

const domWindow = new Window()
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
    value: domWindow[key],
  })
}
Object.defineProperty(domWindow.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const testI18n = createInstance()
await testI18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { ChannelBatchTestDialog } = await import('../channel-batch-test-dialog')
const { ChannelTestDialogForChannel } = await import('../channel-test-dialog')

async function renderDialog(
  element: ReactElement,
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
    },
  })
) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nextProvider i18n={testI18n}>
        <QueryClientProvider client={queryClient}>
          {element}
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
  const dialog = document.body.querySelector<HTMLElement>(
    '[data-slot="dialog-content"]'
  )
  assert.ok(dialog)

  return {
    dialog,
    async cleanup() {
      await act(async () => root.unmount())
      container.remove()
      queryClient.clear()
    },
  }
}

function getVerticalScrollContainers(dialog: HTMLElement) {
  return [...dialog.querySelectorAll<HTMLElement>('*')].filter((element) =>
    element.classList.contains('overflow-y-auto')
  )
}

const batchQueryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
  },
})
batchQueryClient.setQueryData(['channel-batch-test', 'priced-models'], [])
const batchRendered = await renderDialog(
  <ChannelBatchTestDialog open onOpenChange={() => {}} channels={[]} />,
  batchQueryClient
)
assert.equal(
  batchRendered.dialog.style.getPropertyValue('--dialog-content-height'),
  'auto'
)
assert.equal(getVerticalScrollContainers(batchRendered.dialog).length, 1)
await batchRendered.cleanup()

const singleRendered = await renderDialog(
  <ChannelTestDialogForChannel
    channel={{
      id: 7,
      name: '测试渠道',
      models: 'gpt-5',
      test_model: 'gpt-5',
    }}
    open
    onOpenChange={() => {}}
  />
)
assert.ok(singleRendered.dialog.classList.contains('max-h-[calc(100vh-2rem)]'))
assert.equal(
  singleRendered.dialog.style.getPropertyValue('--dialog-content-height'),
  'auto'
)
assert.equal(getVerticalScrollContainers(singleRendered.dialog).length, 1)
await singleRendered.cleanup()

domWindow.close()
process.stdout.write('ok\n')
