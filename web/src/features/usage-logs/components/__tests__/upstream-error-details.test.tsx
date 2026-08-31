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

import { Window } from 'happy-dom'
import { afterAll, describe, test } from 'vitest'

import type { UsageLog } from '../../data/schema'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'Document',
  'DocumentFragment',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'NodeFilter',
  'Element',
  'ShadowRoot',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'MutationObserver',
  'ResizeObserver',
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

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { DetailsDialog } = await import('../dialogs/details-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: { zh: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

function upstreamErrorLog(): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1_752_777_845,
    type: 5,
    content: 'status_code=500, upstream error: do request failed',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 1,
    is_stream: false,
    channel: 9,
    channel_name: 'test-channel',
    token_id: 7,
    group: 'default',
    ip: '',
    request_id: 'request-1',
    upstream_request_id: '',
    other: JSON.stringify({
      admin_info: {
        upstream_error: {
          category: 'dns_error',
          summary: '上游域名解析失败',
          host: 'api.example.com',
          detail: 'lookup ***.***.com: no such host',
        },
      },
    }),
  }
}

async function renderDetails(isAdmin: boolean) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <DetailsDialog
            log={upstreamErrorLog()}
            isAdmin={isAdmin}
            isRoot={false}
            open
            onOpenChange={() => {}}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
  return { container, root, queryClient }
}

describe('upstream error log details', () => {
  afterAll(() => {
    domWindow.close()
  })

  test('shows the classified transport diagnosis to administrators', async () => {
    const rendered = await renderDetails(true)
    const dialog = document.body.querySelector<HTMLElement>(
      '[data-slot="dialog-content"]'
    )

    assert.ok(dialog)
    assert.ok(dialog.textContent?.includes('上游连接诊断'))
    assert.ok(dialog.textContent?.includes('上游域名解析失败'))
    assert.ok(dialog.textContent?.includes('dns_error'))
    assert.ok(dialog.textContent?.includes('api.example.com'))
    assert.ok(dialog.textContent?.includes('no such host'))

    await act(async () => rendered.root.unmount())
    rendered.queryClient.clear()
    rendered.container.remove()
  })

  test('does not render the admin diagnosis for a regular user', async () => {
    const rendered = await renderDetails(false)
    const dialog = document.body.querySelector<HTMLElement>(
      '[data-slot="dialog-content"]'
    )

    assert.ok(dialog)
    assert.equal(dialog.textContent?.includes('上游连接诊断'), false)
    assert.equal(dialog.textContent?.includes('api.example.com'), false)

    await act(async () => rendered.root.unmount())
    rendered.queryClient.clear()
    rendered.container.remove()
  })
})
