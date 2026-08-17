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
import { afterEach, describe, test } from 'vitest'

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
const { ChannelModelDetectionRunDialog } =
  await import('../channel-model-detection-run-dialog')
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
type MockableApi = { post: ApiMethod }
type RenderedDialog = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const apiClient = api as unknown as MockableApi
const originalPost = apiClient.post
let renderedDialog: RenderedDialog | null = null

function channel() {
  return {
    id: 801,
    name: '生产渠道',
    type: 1,
    channel_status: 1,
    remark: '',
    groups: ['default'],
    cost_ratio: null,
    supported_models: ['gpt-5.6-sol'],
    health_status: 'healthy',
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
  }
}

function estimate(overrides: Record<string, unknown> = {}) {
  return {
    preset: 'medium',
    official_estimate: {
      preset: 'medium',
      available: true,
      logical_requests: 12,
      fixed_32k_requests: 2,
      config_hash: 'hash',
      unavailable_reason: '',
    },
    targets: [
      {
        target_key: 'target-existing',
        request_model: 'gpt-5.6-sol',
        claimed_model: 'gpt-5.6-sol',
        estimated_logical_requests: 12,
        estimated_http_attempts: 12,
        estimated_quota: 24_000,
        estimated_cost_nano_cny: 1_250_000_000,
        estimated_cost_cny: '1.250000000',
        cost_estimate_unknown: false,
        estimate_basis: '官方 estimate 请求量 × 渠道成本快照',
      },
    ],
    estimated_quota: 24_000,
    estimated_cost_nano_cny: 1_250_000_000,
    estimated_cost_cny: '1.250000000',
    cost_estimate_unknown_count: 0,
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

function getEstimateButton() {
  const button = document.querySelector<HTMLButtonElement>(
    'button[form="channel-model-detection-estimate-form"][type="submit"]'
  )
  assert.ok(button)
  return button
}

function getStartButton() {
  const button = [...document.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.includes('开始检测')
  ) as HTMLButtonElement | undefined
  assert.ok(button)
  return button
}

async function submitForm() {
  const form = document.querySelector<HTMLFormElement>(
    '#channel-model-detection-estimate-form'
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

async function renderRunDialog(
  options: {
    open?: boolean
    hasUnsavedConfig?: boolean
    channelRevision?: number
    onRunAccepted?: (run: unknown, channelId: number) => void
    onRefreshRequested?: () => void
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
  renderedDialog = { host, queryClient, root }
  const selectedChannel = channel()
  if (options.channelRevision != null && selectedChannel.config) {
    selectedChannel.config.revision = options.channelRevision
  }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelModelDetectionRunDialog
            channel={selectedChannel as never}
            open={options.open ?? true}
            hasUnsavedConfig={options.hasUnsavedConfig}
            onOpenChange={() => undefined}
            onRunAccepted={options.onRunAccepted as never}
            onRefreshRequested={options.onRefreshRequested}
          />
          <Toaster duration={60_000} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

async function rerenderRunDialog(open: boolean) {
  const current = renderedDialog
  assert.ok(current)
  await act(async () => {
    current.root.render(
      <QueryClientProvider client={current.queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelModelDetectionRunDialog
            channel={channel() as never}
            open={open}
            onOpenChange={() => undefined}
          />
          <Toaster duration={60_000} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

async function rerenderRunDialogWithRevision(revision: number) {
  const current = renderedDialog
  assert.ok(current)
  const selectedChannel = channel()
  assert.ok(selectedChannel.config)
  selectedChannel.config.revision = revision
  await act(async () => {
    current.root.render(
      <QueryClientProvider client={current.queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelModelDetectionRunDialog
            channel={selectedChannel as never}
            open
            onOpenChange={() => undefined}
          />
          <Toaster duration={60_000} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

afterEach(async () => {
  apiClient.post = originalPost
  const current = renderedDialog
  if (current) {
    await act(async () => current.root.unmount())
    current.queryClient.clear()
    current.host.remove()
  }
  renderedDialog = null
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  document.body.replaceChildren()
})

describe('模型检测手动估算 Dialog', () => {
  test('每次打开都要求重新选择档位，且只调用 estimate 接口', async () => {
    const calls: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return success(estimate())
    }

    await renderRunDialog()
    assert.equal(getEstimateButton().disabled, true)
    await act(async () => getControl('中档：平衡档位').click())
    assert.equal(getEstimateButton().disabled, false)
    await submitForm()
    await act(async () =>
      waitForCondition(
        () =>
          calls.length === 1 &&
          document.body.textContent?.includes('¥1.250000000') === true,
        'estimate was not shown'
      )
    )
    assert.deepEqual(calls, [
      {
        url: CHANNEL_MODEL_DETECTION_ENDPOINTS.channelEstimate(801),
        data: { preset: 'medium' },
      },
    ])
    assert.equal(
      calls.some((call) =>
        call.url.includes(CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRun(801))
      ),
      false
    )

    await rerenderRunDialog(false)
    await rerenderRunDialog(true)
    assert.equal(getEstimateButton().disabled, true)
  })

  test('高档估算要求本次独立确认', async () => {
    const requests: unknown[] = []
    apiClient.post = async (_url, data) => {
      requests.push(data)
      return success(estimate({ preset: 'high' }))
    }

    await renderRunDialog()
    await act(async () => getControl('高档：请求和成本更高').click())
    assert.equal(getEstimateButton().disabled, true)
    assert.equal(
      document.body.textContent?.includes('与统一定时高档确认互不复用'),
      true
    )
    await act(async () => getControl('确认本次高档手动检测成本风险').click())
    assert.equal(getEstimateButton().disabled, false)
    await submitForm()
    await act(async () =>
      waitForCondition(
        () =>
          requests.length === 1 &&
          document.body.textContent?.includes('估算档位高档') === true,
        'high estimate was not shown'
      )
    )
    assert.deepEqual(requests, [{ preset: 'high' }])
  })

  test('未知成本明确显示暂无法估算而不是 0', async () => {
    apiClient.post = async () =>
      success(
        estimate({
          preset: 'low',
          official_estimate: {
            ...estimate().official_estimate,
            preset: 'low',
          },
          estimated_quota: null,
          estimated_cost_nano_cny: null,
          estimated_cost_cny: null,
          cost_estimate_unknown_count: 12,
          targets: [
            {
              ...estimate().targets[0],
              estimated_quota: null,
              estimated_cost_nano_cny: null,
              estimated_cost_cny: null,
              cost_estimate_unknown: true,
            },
          ],
        })
      )

    await renderRunDialog()
    await act(async () => getControl('低档：请求较少').click())
    await submitForm()
    await act(async () =>
      waitForCondition(
        () =>
          document.body.textContent?.includes('部分请求暂无法估算成本') ===
          true,
        'unknown cost warning was not shown'
      )
    )
    const text = document.body.textContent ?? ''
    assert.equal(text.includes('暂无法估算'), true)
    assert.equal(text.includes('预计总成本¥0'), false)
  })

  test('存在未保存配置时不允许对旧目标估算', async () => {
    const calls: unknown[] = []
    apiClient.post = async (_url, data) => {
      calls.push(data)
      return success(estimate())
    }

    await renderRunDialog({ hasUnsavedConfig: true })
    assert.equal(
      document.body.textContent?.includes('存在未保存的渠道目标修改'),
      true
    )
    assert.equal(getEstimateButton().disabled, true)
    await submitForm()
    assert.deepEqual(calls, [])
  })

  test('估算后修改档位不能提交旧档位快照', async () => {
    const calls: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return success(estimate({ preset: 'low' }))
    }

    await renderRunDialog()
    await act(async () => getControl('低档：请求较少').click())
    await submitForm()
    await act(async () =>
      waitForCondition(() => !getStartButton().disabled, 'start was not ready')
    )
    await act(async () => getControl('中档：平衡档位').click())

    assert.equal(getStartButton().disabled, true)
    assert.equal(calls.length, 1)
    assert.equal(
      calls.some((call) =>
        call.url.includes(CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRun(801))
      ),
      false
    )
  })

  test('估算后渠道 revision 变化会阻止启动并要求重新估算', async () => {
    const calls: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      return success(estimate())
    }

    await renderRunDialog()
    await act(async () => getControl('中档：平衡档位').click())
    await submitForm()
    await act(async () =>
      waitForCondition(() => !getStartButton().disabled, 'start was not ready')
    )
    await rerenderRunDialogWithRevision(8)

    assert.equal(getStartButton().disabled, true)
    assert.equal(document.body.textContent?.includes('成本估算已失效'), true)
    assert.equal(calls.length, 1)
  })

  test('开始检测提交最后估算档位并回调已接受任务', async () => {
    const calls: Array<{ url: string; data: unknown }> = []
    const accepted: Array<{ run: unknown; channelId: number }> = []
    let refreshed = 0
    apiClient.post = async (url, data) => {
      calls.push({ url, data })
      if (url === CHANNEL_MODEL_DETECTION_ENDPOINTS.channelEstimate(801)) {
        return success(estimate())
      }
      return success({
        run_id: 'run-accepted',
        status: 'queued',
        preset: 'medium',
        preset_source: 'manual_selected',
      })
    }

    await renderRunDialog({
      onRunAccepted: (run, channelId) => accepted.push({ run, channelId }),
      onRefreshRequested: () => {
        refreshed += 1
      },
    })
    await act(async () => getControl('中档：平衡档位').click())
    await submitForm()
    await act(async () =>
      waitForCondition(() => !getStartButton().disabled, 'start was not ready')
    )
    await act(async () => {
      getStartButton().click()
      await waitForCondition(
        () =>
          accepted.length === 1 &&
          refreshed === 1 &&
          getStartButton().disabled === false,
        'run was not accepted'
      )
      await Promise.resolve()
    })

    assert.deepEqual(calls[1], {
      url: CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRun(801),
      data: { preset: 'medium', confirm_high_cost: false },
    })
    assert.deepEqual(accepted, [
      {
        run: {
          run_id: 'run-accepted',
          status: 'queued',
          preset: 'medium',
          preset_source: 'manual_selected',
        },
        channelId: 801,
      },
    ])
    assert.equal(refreshed, 1)
  })

  test('高档启动携带本次独立成本确认', async () => {
    const requests: Array<{ url: string; data: unknown }> = []
    apiClient.post = async (url, data) => {
      requests.push({ url, data })
      if (url === CHANNEL_MODEL_DETECTION_ENDPOINTS.channelEstimate(801)) {
        return success(estimate({ preset: 'high' }))
      }
      return success({
        run_id: 'run-high',
        status: 'queued',
        preset: 'high',
        preset_source: 'manual_selected',
      })
    }

    await renderRunDialog()
    await act(async () => getControl('高档：请求和成本更高').click())
    await act(async () => getControl('确认本次高档手动检测成本风险').click())
    await submitForm()
    await act(async () =>
      waitForCondition(() => !getStartButton().disabled, 'high start not ready')
    )
    await act(async () => {
      getStartButton().click()
      await waitForCondition(
        () => requests.length === 2 && getStartButton().disabled === false,
        'high run was not sent'
      )
      await Promise.resolve()
    })

    assert.deepEqual(requests[1], {
      url: CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRun(801),
      data: { preset: 'high', confirm_high_cost: true },
    })
  })

  test('已有活动任务 409 作为基础设施冲突处理并清除旧估算', async () => {
    let refreshed = 0
    apiClient.post = async (url) => {
      if (url === CHANNEL_MODEL_DETECTION_ENDPOINTS.channelEstimate(801)) {
        return success(estimate())
      }
      throw Object.assign(new Error('Request failed with status code 409'), {
        isAxiosError: true,
        response: {
          status: 409,
          data: {
            success: false,
            message: '该渠道已有活动模型检测任务',
          },
        },
      })
    }

    await renderRunDialog({
      onRefreshRequested: () => {
        refreshed += 1
      },
    })
    await act(async () => getControl('中档：平衡档位').click())
    await submitForm()
    await act(async () =>
      waitForCondition(() => !getStartButton().disabled, 'start was not ready')
    )
    await act(async () => {
      getStartButton().click()
      await waitForCondition(
        () =>
          refreshed === 1 &&
          document.body.textContent?.includes('模型检测任务状态发生冲突') ===
            true,
        'conflict was not shown'
      )
      await Promise.resolve()
    })

    assert.equal(getStartButton().disabled, true)
    assert.equal(document.body.textContent?.includes('检测到异常证据'), false)
  })
})
