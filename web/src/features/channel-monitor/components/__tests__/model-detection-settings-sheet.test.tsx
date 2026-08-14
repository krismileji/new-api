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
const { ChannelModelDetectionSettingsSheet } =
  await import('../channel-model-detection-settings-sheet')
const {
  channelModelDetectionSettingsSchema,
  createChannelModelDetectionSettingsUpdateRequest,
} = await import('../../lib/model-detection-settings-schema')
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
type MockableApi = {
  get: ApiMethod
  put: ApiMethod
  post: ApiMethod
}
type RenderedSheet = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPut = apiClient.put
const originalPost = apiClient.post
let renderedSheet: RenderedSheet | null = null

function settings(overrides: Record<string, unknown> = {}) {
  return {
    detector_url_configured: true,
    detector_url_masked: 'http://10.0.0.8:8000/private',
    pending_detector_url_configured: false,
    pending_detector_url_masked: '',
    detector_url_switch_pending: false,
    scheduled_preset: 'medium',
    schedule_enabled: true,
    interval_minutes: 1440,
    schedule_anchor_at: 1_775_000_000,
    next_batch_at: 1_775_086_400,
    revision: 7,
    connection_test_required: false,
    created_at: 1_774_000_000,
    updated_at: 1_775_000_000,
    ...overrides,
  }
}

function detectorService() {
  return {
    state: 'available',
    detector_url_configured: true,
    detector_url_masked: 'http://10.0.0.8:8000/private',
    busy: false,
    active_session_owned: false,
    deployment_id: null,
    last_checked_at: 1_775_000_100,
    last_error: '',
    compatibility_message: '官方检测器接口兼容',
    estimates: {},
  }
}

function success<T>(data: T) {
  return { data: { success: true, message: '', data } }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, reject, resolve }
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

function findButton(text: string, required: true): HTMLButtonElement
function findButton(text: string, required: false): HTMLButtonElement | null
function findButton(text: string, required = true) {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.includes(text))
  if (required) assert.ok(button, `Expected button containing "${text}"`)
  return button ?? null
}

function getSaveButton() {
  const button = document.querySelector<HTMLButtonElement>(
    'button[form="channel-model-detection-settings-form"][type="submit"]'
  )
  assert.ok(button)
  return button
}

function getNewDetectorURLInput() {
  const input = document.querySelector<HTMLInputElement>(
    'input[aria-label="新检测器地址"]'
  )
  assert.ok(input)
  return input
}

function getControl(ariaLabel: string) {
  const control = document.querySelector<HTMLElement>(
    `[aria-label="${ariaLabel}"]`
  )
  assert.ok(control, `Expected control "${ariaLabel}"`)
  return control
}

async function changeInput(input: HTMLInputElement, value: string) {
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

async function submitForm() {
  const form = document.querySelector<HTMLFormElement>(
    '#channel-model-detection-settings-form'
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

async function renderSettingsSheet() {
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
          <ChannelModelDetectionSettingsSheet
            open
            onOpenChange={() => undefined}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

async function waitForLoadedSettings() {
  await act(async () =>
    waitForCondition(
      () => document.querySelector('input[aria-label="新检测器地址"]') !== null,
      'settings sheet did not finish loading'
    )
  )
}

afterEach(async () => {
  apiClient.get = originalGet
  apiClient.put = originalPut
  apiClient.post = originalPost
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  if (renderedSheet) {
    await act(async () => renderedSheet?.root.unmount())
    renderedSheet.queryClient.clear()
    renderedSheet.host.remove()
    renderedSheet = null
  }
  document.body.replaceChildren()
})

describe('模型检测统一设置规则', () => {
  test('限制整分钟周期、地址互斥和高档确认', () => {
    for (const interval of [
      { intervalValue: 15, intervalUnit: 'minute' },
      { intervalValue: 24, intervalUnit: 'hour' },
    ] as const) {
      assert.equal(
        channelModelDetectionSettingsSchema.safeParse({
          detectorURL: '',
          clearDetectorURL: false,
          scheduledPreset: 'medium',
          scheduleEnabled: true,
          ...interval,
          confirmHighCost: false,
          revision: 1,
        }).success,
        true
      )
    }

    const invalidValues = [
      { intervalValue: 0 },
      { intervalValue: 1.5 },
      { intervalValue: 8761, intervalUnit: 'hour' },
      { detectorURL: 'ftp://10.0.0.8:8000' },
      {
        detectorURL: 'http://10.0.0.8:8000',
        clearDetectorURL: true,
      },
      {
        scheduledPreset: 'high',
        scheduleEnabled: true,
        confirmHighCost: false,
      },
    ]

    for (const invalid of invalidValues) {
      assert.equal(
        channelModelDetectionSettingsSchema.safeParse({
          detectorURL: '',
          clearDetectorURL: false,
          scheduledPreset: 'medium',
          scheduleEnabled: true,
          intervalValue: 24,
          intervalUnit: 'hour',
          confirmHighCost: false,
          revision: 1,
          ...invalid,
        }).success,
        false
      )
    }

    assert.equal(
      channelModelDetectionSettingsSchema.safeParse({
        detectorURL: '',
        clearDetectorURL: false,
        scheduledPreset: 'high',
        scheduleEnabled: false,
        intervalValue: 24,
        intervalUnit: 'hour',
        confirmHighCost: false,
        revision: 1,
      }).success,
      true
    )
  })

  test('空地址不会进入更新请求，高档确认只随本次有效命令提交', () => {
    const unchanged = createChannelModelDetectionSettingsUpdateRequest({
      detectorURL: '   ',
      clearDetectorURL: false,
      scheduledPreset: 'medium',
      scheduleEnabled: true,
      intervalValue: 90,
      intervalUnit: 'minute',
      confirmHighCost: true,
      revision: 7,
    })
    assert.equal('detector_url' in unchanged, false)
    assert.equal(unchanged.confirm_high_cost, false)

    const high = createChannelModelDetectionSettingsUpdateRequest({
      detectorURL: ' http://10.0.0.9:8000 ',
      clearDetectorURL: false,
      scheduledPreset: 'high',
      scheduleEnabled: true,
      intervalValue: 48,
      intervalUnit: 'hour',
      confirmHighCost: true,
      revision: 8,
    })
    assert.equal(high.detector_url, 'http://10.0.0.9:8000')
    assert.equal(high.interval_minutes, 2880)
    assert.equal(high.confirm_high_cost, true)
  })
})

describe('模型检测统一设置 Sheet', () => {
  test('配置地址原样展示且不会自动回填输入框，保存期间锁定编辑', async () => {
    const updates: Array<Record<string, unknown>> = []
    const updateRequest = deferred<{ data: unknown }>()
    apiClient.get = async (url) => {
      assert.equal(url, CHANNEL_MODEL_DETECTION_ENDPOINTS.settings)
      return success(settings())
    }
    apiClient.put = (url, data) => {
      assert.equal(url, CHANNEL_MODEL_DETECTION_ENDPOINTS.settings)
      assert.ok(data && typeof data === 'object')
      updates.push(data as Record<string, unknown>)
      return updateRequest.promise
    }

    await renderSettingsSheet()
    await waitForLoadedSettings()

    const addressInput = getNewDetectorURLInput()
    assert.equal(addressInput.value, '')
    assert.equal(
      document.body.textContent?.includes('http://10.0.0.8:8000/private'),
      true
    )

    await submitForm()
    await act(async () =>
      waitForCondition(
        () => updates.length === 1,
        'settings were not submitted'
      )
    )
    assert.equal('detector_url' in (updates[0] ?? {}), false)
    assert.equal(updates[0]?.revision, 7)
    await act(async () =>
      waitForCondition(
        () => getSaveButton().disabled && addressInput.disabled,
        'settings form did not lock during save'
      )
    )

    await act(async () =>
      updateRequest.resolve(success(settings({ revision: 8 })))
    )
    await act(async () =>
      waitForCondition(
        () => !getSaveButton().disabled,
        'settings form did not unlock after save'
      )
    )
    assert.equal(getNewDetectorURLInput().value, '')
  })

  test('连接测试只会在显式点击且没有未保存地址时发起', async () => {
    const tested: string[] = []
    apiClient.get = async () => success(settings())
    apiClient.post = async (url) => {
      tested.push(url)
      return success(detectorService())
    }

    await renderSettingsSheet()
    await waitForLoadedSettings()
    assert.deepEqual(tested, [])

    const addressInput = getNewDetectorURLInput()
    await changeInput(addressInput, 'http://10.0.0.9:8000')
    const testButton = findButton('测试连接', true)
    assert.equal(testButton.disabled, true)
    assert.deepEqual(tested, [])

    await changeInput(addressInput, '')
    assert.equal(testButton.disabled, false)
    await act(async () => testButton.click())
    await act(async () =>
      waitForCondition(
        () => document.body.textContent?.includes('连接正常') === true,
        'connection result was not shown'
      )
    )
    assert.deepEqual(tested, [CHANNEL_MODEL_DETECTION_ENDPOINTS.serviceTest])
  })

  test('待切换地址仍可测试当前已保存服务，连接失败会展示检测器状态', async () => {
    const tested: string[] = []
    apiClient.get = async () =>
      success(
        settings({
          pending_detector_url_configured: true,
          pending_detector_url_masked: 'http://10.0.0.9:8000/private',
          detector_url_switch_pending: true,
        })
      )
    apiClient.post = async (url) => {
      tested.push(url)
      return success(detectorService())
    }

    await renderSettingsSheet()
    await waitForLoadedSettings()
    assert.equal(findButton('测试连接', true).disabled, false)
    await act(async () => findButton('测试连接', true).click())
    await act(async () =>
      waitForCondition(
        () => document.body.textContent?.includes('连接正常') === true,
        'connection result was not shown'
      )
    )
    assert.deepEqual(tested, [CHANNEL_MODEL_DETECTION_ENDPOINTS.serviceTest])

    const firstRenderedSheet = renderedSheet
    assert.ok(firstRenderedSheet)
    await act(async () => firstRenderedSheet.root.unmount())
    firstRenderedSheet.queryClient.clear()
    firstRenderedSheet.host.remove()
    renderedSheet = null
    await act(async () => {
      toast.dismiss()
      await Promise.resolve()
    })
    document.body.replaceChildren()

    apiClient.get = async () => success(settings())
    apiClient.post = async () => {
      const error = new Error('连接被拒绝') as Error & {
        isAxiosError: boolean
        response: { data: unknown }
      }
      error.isAxiosError = true
      error.response = {
        data: {
          success: false,
          message: '连接被拒绝',
          data: {
            ...detectorService(),
            state: 'offline',
            last_error: '连接被拒绝',
            compatibility_message: '官方检测器检查失败',
          },
        },
      }
      throw error
    }
    await renderSettingsSheet()
    await waitForLoadedSettings()
    await act(async () => findButton('测试连接', true).click())
    await act(async () =>
      waitForCondition(
        () => document.body.textContent?.includes('连接失败') === true,
        'failed detector state was not shown'
      )
    )
    assert.equal(
      document.body.textContent?.includes('官方检测器检查失败'),
      true
    )
  })

  test('高档定时设置每次保存都重新要求确认', async () => {
    const updates: Array<Record<string, unknown>> = []
    let revision = 7
    apiClient.get = async () => success(settings({ revision }))
    apiClient.put = async (_url, data) => {
      assert.ok(data && typeof data === 'object')
      updates.push(data as Record<string, unknown>)
      revision += 1
      return success(
        settings({
          scheduled_preset: 'high',
          revision,
          schedule_enabled: true,
        })
      )
    }

    await renderSettingsSheet()
    await waitForLoadedSettings()
    await act(async () => getControl('高档：请求更多，成本更高').click())

    assert.equal(
      document.body.textContent?.includes('高档定时检测会产生更多请求和成本'),
      true
    )
    assert.equal(getSaveButton().disabled, true)
    const confirmation = getControl('确认高档定时检测成本风险')
    await act(async () => confirmation.click())
    assert.equal(getSaveButton().disabled, false)

    await submitForm()
    await act(async () =>
      waitForCondition(
        () => updates.length === 1,
        'first high save did not run'
      )
    )
    assert.equal(updates[0]?.confirm_high_cost, true)
    await act(async () =>
      waitForCondition(
        () => getSaveButton().disabled,
        'high confirmation was not cleared after save'
      )
    )

    await act(async () => getControl('确认高档定时检测成本风险').click())
    await submitForm()
    await act(async () =>
      waitForCondition(
        () => updates.length === 2,
        'second high save did not run'
      )
    )
    assert.equal(updates[1]?.confirm_high_cost, true)
    assert.equal(updates[1]?.revision, 8)
  })

  test('revision 冲突后必须刷新设置再重试', async () => {
    const revisions: unknown[] = []
    let getCount = 0
    let putCount = 0
    apiClient.get = async () => {
      getCount += 1
      return success(settings({ revision: getCount === 1 ? 7 : 11 }))
    }
    apiClient.put = async (_url, data) => {
      assert.ok(data && typeof data === 'object')
      revisions.push((data as Record<string, unknown>).revision)
      putCount += 1
      if (putCount === 1) {
        return {
          data: {
            success: false,
            message: '设置已被其他管理员更新，请刷新后重试',
            code: 'revision_conflict',
          },
        }
      }
      return success(settings({ revision: 12 }))
    }

    await renderSettingsSheet()
    await waitForLoadedSettings()
    await submitForm()
    await act(async () =>
      waitForCondition(
        () => document.body.textContent?.includes('设置已发生冲突') === true,
        'revision conflict was not shown'
      )
    )
    assert.equal(getSaveButton().disabled, true)

    await act(async () => findButton('刷新设置', true).click())
    await act(async () =>
      waitForCondition(
        () =>
          document.body.textContent?.includes('设置已发生冲突') === false &&
          !getSaveButton().disabled,
        'refreshed settings were not applied'
      )
    )
    await submitForm()
    await act(async () =>
      waitForCondition(() => revisions.length === 2, 'retry save did not run')
    )
    assert.deepEqual(revisions, [7, 11])
  })

  test('加载失败前不允许保存并提供重试状态', async () => {
    const loadRequest = deferred<{ data: unknown }>()
    apiClient.get = () => loadRequest.promise

    await renderSettingsSheet()
    assert.equal(document.body.textContent?.includes('正在加载统一设置'), true)
    assert.equal(getSaveButton().disabled, true)

    await act(async () => loadRequest.reject(new Error('network down')))
    await act(async () =>
      waitForCondition(
        () => document.body.textContent?.includes('统一设置加载失败') === true,
        'load error state was not shown'
      )
    )
    assert.equal(document.body.textContent?.includes('network down'), true)
    assert.equal(findButton('重试', false) !== null, true)
    assert.equal(getSaveButton().disabled, true)
  })
})
