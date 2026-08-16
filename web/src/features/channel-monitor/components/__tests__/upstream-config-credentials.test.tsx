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
import { afterEach, describe, test } from 'node:test'

import type {
  ChannelMonitorItem,
  ChannelMonitorUpstreamRequest,
} from '../../types'
import { domWindow } from './test-dom'

for (const key of ['HTMLLabelElement', 'PointerEvent'] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { toast } = await import('sonner')
const { api } = await import('@/lib/api')
const { UpstreamConfigDialog } = await import('../upstream-config-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: { zh: { translation: {} } },
})

type ApiPost = (
  url: string,
  data?: unknown,
  config?: unknown
) => Promise<{ data: unknown }>
const apiClient = api as unknown as { post: ApiPost }
const originalPost = apiClient.post
let rendered:
  | {
      host: HTMLDivElement
      queryClient: InstanceType<typeof QueryClient>
      root: ReturnType<typeof createRoot>
    }
  | undefined

const channel: ChannelMonitorItem = {
  id: 7,
  name: '测试渠道',
  type: 1,
  status: 1,
  status_reason: '',
  priority: 0,
  weight: 0,
  base_url: 'https://upstream.example',
  models: 'test-model',
  test_model: 'test-model',
  groups: ['default'],
  ratio: 1,
  previous_ratio: 1,
  cost_ratio: 1,
  previous_cost_ratio: 1,
  conversion_factor: 1,
  remark: '',
  channel_remark: '',
  updated_time: 0,
  updated_by: 0,
  updated_by_username: '',
  last_fetch_status: '',
  last_fetch_error: '',
  last_fetch_time: 0,
  consecutive_failures: 0,
  upstream_balance: null,
  last_balance_time: 0,
  last_balance_error: '',
  today_cost_cny: 0,
  today_cost_configured: false,
  today_cost_complete: true,
  today_cost_unresolved_count: 0,
  concurrency_limit: 0,
  concurrency_active: 0,
  upstream: {
    type: 'sub2api',
    base_url: 'https://upstream.example',
    group: 'vip',
    auth_type: 'token',
    user_id: 0,
    has_access_token: false,
    has_refresh_token: false,
    account: '',
    has_password: false,
    single_channel_action: 'none',
    multiple_channels_action: 'none',
    balance_warning_threshold: null,
    balance_auto_disable_threshold: null,
    ratio_sync_enabled: true,
    balance_sync_enabled: true,
    cost_conversion: { mode: 'none' },
  },
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
) {
  const deadline = Date.now() + 2000
  while (Date.now() < deadline) {
    if (condition()) return
    await new Promise((resolve) => setTimeout(resolve, 10))
  }
  throw new Error(`${failureMessage}: ${document.body.textContent}`)
}

function findButton(text: string) {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.trim() === text)
  assert.ok(button, `Expected button "${text}"`)
  return button
}

async function renderDialog(channelItem: ChannelMonitorItem) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  rendered = { host, queryClient, root }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <UpstreamConfigDialog
            channel={channelItem}
            open
            onOpenChange={() => undefined}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

async function changeInput(placeholder: string, value: string) {
  const input = document.querySelector<HTMLInputElement>(
    `input[placeholder="${placeholder}"]`
  )
  assert.ok(input, `Expected input "${placeholder}"`)
  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      domWindow.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(valueSetter)
    valueSetter.call(input, value)
    input.dispatchEvent(
      new domWindow.Event('input', { bubbles: true }) as unknown as Event
    )
  })
}

afterEach(async () => {
  apiClient.post = originalPost
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  if (rendered) {
    await act(async () => rendered?.root.unmount())
    rendered.queryClient.clear()
    rendered.host.remove()
    rendered = undefined
  }
  document.body.replaceChildren()
})

describe('Sub2API credential controls', () => {
  test('keeps both credentials visible and tests each credential independently', async () => {
    const requests: ChannelMonitorUpstreamRequest[] = []
    apiClient.post = async (url, data) => {
      assert.equal(url, '/api/channel_monitor/channel/7/upstream/test')
      requests.push(data as ChannelMonitorUpstreamRequest)
      return {
        data: {
          success: true,
          message: '',
          data: {
            ratio: 1,
            cost_ratio: 1,
            conversion_factor: 1,
            endpoint: '/api/v1/groups',
            balance: { amount: 10 },
          },
        },
      }
    }

    await renderDialog(channel)

    findButton('API Key（新版）')
    findButton('账号密码')
    findButton('Token 认证')
    assert.equal(
      [...document.querySelectorAll<HTMLButtonElement>('button')].some(
        (button) => button.textContent?.trim() === 'Refresh Token'
      ),
      false
    )

    await changeInput('输入登录后的 JWT Token', 'access-token')
    await changeInput('输入 Sub2API Refresh Token', 'refresh-token')

    await act(async () => findButton('测试手动 Token').click())
    await act(async () =>
      waitForCondition(
        () => requests.length === 1,
        'manual Token was not tested'
      )
    )
    await act(async () => findButton('测试 Refresh Token').click())
    await act(async () =>
      waitForCondition(
        () => requests.length === 2,
        'Refresh Token was not tested'
      )
    )

    assert.deepEqual(
      requests.map(({ auth_type, access_token, refresh_token }) => ({
        auth_type,
        access_token,
        refresh_token,
      })),
      [
        { auth_type: 'token', access_token: 'access-token', refresh_token: '' },
        {
          auth_type: 'refresh_token',
          access_token: 'refresh-token',
          refresh_token: undefined,
        },
      ]
    )
  })

  test('shows safety defaults for an unconfigured upstream', async () => {
    await renderDialog({ ...channel, upstream: null })
    const warningThreshold = document.querySelector<HTMLInputElement>(
      'input[name="balanceWarningThreshold"]'
    )
    const autoDisableThreshold = document.querySelector<HTMLInputElement>(
      'input[name="balanceAutoDisableThreshold"]'
    )
    const dialogText = document.body.textContent ?? ''

    assert.equal(warningThreshold?.value, '2')
    assert.equal(autoDisableThreshold?.value, '1')
    assert.match(dialogText, /仅剩此渠道时禁用此渠道/)
    assert.match(dialogText, /存在多个渠道时移除当前渠道/)
  })
})
