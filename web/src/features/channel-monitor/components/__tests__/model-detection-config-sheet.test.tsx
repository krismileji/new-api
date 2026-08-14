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

import type { ChannelModelDetectionChannel } from '../../types-model-detection'
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
const { Toaster, toast } = await import('sonner')
const { api } = await import('@/lib/api')
const { ChannelModelDetectionConfigSheet } =
  await import('../channel-model-detection-config-sheet')
const {
  channelModelDetectionChannelToConfigFormValues,
  createChannelModelDetectionConfigSchema,
  createChannelModelDetectionConfigUpdateRequest,
} = await import('../../lib/model-detection-channel-schema')
const { CHANNEL_MODEL_DETECTION_ENDPOINTS } =
  await import('../../lib/model-detection')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: { zh: { translation: {} } },
})

type ApiMethod = (
  url: string,
  data?: unknown,
  config?: unknown
) => Promise<{ data: unknown }>
type MockableApi = { put: ApiMethod }
type RenderedSheet = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const apiClient = api as unknown as MockableApi
const originalPut = apiClient.put
let renderedSheet: RenderedSheet | null = null

function channel(
  overrides: Partial<ChannelModelDetectionChannel> = {}
): ChannelModelDetectionChannel {
  return {
    id: 801,
    name: '生产渠道',
    type: 1,
    channel_status: 1,
    remark: '',
    groups: ['default'],
    cost_ratio: null,
    supported_models: ['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-*'],
    health_status: 'healthy' as const,
    config: {
      channel_id: 801,
      schedule_enabled: false,
      revision: 7,
      created_at: 1,
      updated_at: 2,
    },
    active_run: null,
    targets: [
      {
        target_key: 'target-existing',
        request_model: 'gpt-5.6-sol',
        claimed_model: 'gpt-5.6-sol',
        enabled: true,
        position: 0,
        latest: null,
      },
    ],
    latest_run_cost: null,
    ...overrides,
  }
}

function savedConfig(overrides: Record<string, unknown> = {}) {
  return {
    channel_id: 801,
    schedule_enabled: false,
    revision: 8,
    created_at: 1,
    updated_at: 3,
    targets: [
      {
        target_key: 'target-existing',
        request_model: 'gpt-5.6-sol',
        claimed_model: 'gpt-5.6-sol',
        enabled: true,
        position: 0,
      },
    ],
    ...overrides,
  }
}

function success<T>(data: T) {
  return { data: { success: true, message: '', data } }
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

function getControl(label: string) {
  const control = document.querySelector<HTMLElement>(`[aria-label="${label}"]`)
  assert.ok(control, `Expected control "${label}"`)
  return control
}

function getSaveButton() {
  const button = document.querySelector<HTMLButtonElement>(
    'button[form="channel-model-detection-config-form"][type="submit"]'
  )
  assert.ok(button)
  return button
}

async function chooseOption(trigger: HTMLElement, label: string) {
  await act(async () => {
    trigger.focus()
    trigger.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'ArrowDown',
        code: 'ArrowDown',
        bubbles: true,
      })
    )
  })
  const option = [
    ...document.querySelectorAll<HTMLElement>(
      '[data-slot="select-content"][data-open] [role="option"]'
    ),
  ].find((candidate) => candidate.textContent?.trim() === label)
  assert.ok(option, `Expected option "${label}"`)
  await act(async () => option.click())
}

async function submitForm() {
  const form = document.querySelector<HTMLFormElement>(
    '#channel-model-detection-config-form'
  )
  assert.ok(form)
  await act(async () => {
    form.dispatchEvent(
      new domWindow.Event('submit', {
        bubbles: true,
        cancelable: true,
      }) as unknown as Event
    )
  })
}

async function renderConfigSheet(
  options: {
    detectorURLConfigured?: boolean
    channelOverrides?: Record<string, unknown>
    onOpenChange?: (open: boolean) => void
    onRefreshChannel?: (channelId: number) => void | Promise<void>
  } = {}
) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  renderedSheet = { host, queryClient, root }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelModelDetectionConfigSheet
            channel={channel(
              options.channelOverrides as Partial<ChannelModelDetectionChannel>
            )}
            detectorURLConfigured={options.detectorURLConfigured ?? true}
            open
            onOpenChange={options.onOpenChange ?? (() => undefined)}
            onRefreshChannel={options.onRefreshChannel}
          />
          <Toaster duration={60_000} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

afterEach(async () => {
  apiClient.put = originalPut
  const current = renderedSheet
  if (current) {
    await act(async () => current.root.unmount())
    current.queryClient.clear()
    current.host.remove()
  }
  renderedSheet = null
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  document.body.replaceChildren()
})

describe('模型检测渠道配置 schema', () => {
  test('过滤通配模型并拒绝重复组合', () => {
    const values = channelModelDetectionChannelToConfigFormValues(
      channel() as never
    )
    assert.deepEqual(values.targets, [
      {
        targetKey: 'target-existing',
        requestModel: 'gpt-5.6-sol',
        claimedModel: 'gpt-5.6-sol',
      },
    ])

    const schema = createChannelModelDetectionConfigSchema(
      ['gpt-5.6-sol', 'gpt-5.6-*'],
      true
    )
    const result = schema.safeParse({
      scheduleEnabled: false,
      revision: 7,
      targets: [
        {
          targetKey: 'one',
          requestModel: 'gpt-5.6-sol',
          claimedModel: 'gpt-5.6-sol',
        },
        {
          targetKey: 'two',
          requestModel: 'gpt-5.6-sol',
          claimedModel: 'gpt-5.6-sol',
        },
      ],
    })
    assert.equal(result.success, false)
    assert.equal(
      result.error?.issues.some((issue) =>
        issue.message.includes('组合不能重复')
      ),
      true
    )
  })

  test('构建请求时保留已有目标标识和 revision', () => {
    const request = createChannelModelDetectionConfigUpdateRequest({
      scheduleEnabled: true,
      revision: 9,
      targets: [
        {
          targetKey: ' existing-key ',
          requestModel: ' gpt-5.6-sol ',
          claimedModel: 'gpt-5.6-terra',
        },
        {
          targetKey: '',
          requestModel: 'gpt-5.6-terra',
          claimedModel: 'gpt-5.6-terra',
        },
      ],
    })
    assert.deepEqual(request, {
      schedule_enabled: true,
      revision: 9,
      targets: [
        {
          target_key: 'existing-key',
          request_model: 'gpt-5.6-sol',
          claimed_model: 'gpt-5.6-terra',
        },
        {
          target_key: '',
          request_model: 'gpt-5.6-terra',
          claimed_model: 'gpt-5.6-terra',
        },
      ],
    })
  })
})

describe('模型检测渠道配置 Sheet', () => {
  test('新增目标后保存精确模型、申报型号和空的新目标标识', async () => {
    const requests: Array<Record<string, unknown>> = []
    apiClient.put = async (url, data) => {
      assert.equal(url, CHANNEL_MODEL_DETECTION_ENDPOINTS.channelConfig(801))
      assert.ok(data && typeof data === 'object')
      requests.push(data as Record<string, unknown>)
      return success(
        savedConfig({
          targets: [
            ...savedConfig().targets,
            {
              target_key: 'target-new',
              request_model: 'gpt-5.6-terra',
              claimed_model: 'gpt-5.6-luna',
              enabled: true,
              position: 1,
            },
          ],
        })
      )
    }

    await renderConfigSheet()
    const addButton = [
      ...document.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('添加目标'))
    assert.ok(addButton)
    await act(async () => addButton.click())
    await chooseOption(getControl('目标 2 请求模型'), 'gpt-5.6-terra')
    await chooseOption(getControl('目标 2 申报型号'), 'Luna')
    await submitForm()
    await act(async () =>
      waitForCondition(() => requests.length === 1, 'config was not saved')
    )

    assert.deepEqual(requests[0], {
      schedule_enabled: false,
      revision: 7,
      targets: [
        {
          target_key: 'target-existing',
          request_model: 'gpt-5.6-sol',
          claimed_model: 'gpt-5.6-sol',
        },
        {
          target_key: '',
          request_model: 'gpt-5.6-terra',
          claimed_model: 'gpt-5.6-luna',
        },
      ],
    })
  })

  test('未配置检测器地址时禁用统一定时参与', async () => {
    await renderConfigSheet({ detectorURLConfigured: false })
    const schedule = getControl('参加模型检测统一定时')
    assert.equal(schedule.getAttribute('data-disabled') !== null, true)
    assert.equal(
      document.body.textContent?.includes('尚未配置检测器地址'),
      true
    )
  })

  test('revision 冲突后锁定保存并要求刷新渠道', async () => {
    let closeValue: boolean | null = null
    const refreshed: number[] = []
    apiClient.put = async () => ({
      data: {
        success: false,
        message: '渠道配置已被其他管理员更新',
        code: 'revision_conflict',
      },
    })

    await renderConfigSheet({
      onOpenChange: (open) => {
        closeValue = open
      },
      onRefreshChannel: (channelId) => {
        refreshed.push(channelId)
      },
    })
    await submitForm()
    await act(async () =>
      waitForCondition(
        () =>
          document.body.textContent?.includes('渠道配置已发生冲突') === true,
        'conflict was not shown'
      )
    )
    assert.equal(getSaveButton().disabled, true)

    const refreshButton = [
      ...document.querySelectorAll<HTMLButtonElement>('button'),
    ].find((button) => button.textContent?.includes('刷新渠道后重新打开'))
    assert.ok(refreshButton)
    await act(async () => refreshButton.click())
    await act(async () =>
      waitForCondition(
        () => refreshed.length === 1,
        'channel was not refreshed'
      )
    )
    assert.deepEqual(refreshed, [801])
    assert.equal(closeValue, false)
  })
})
