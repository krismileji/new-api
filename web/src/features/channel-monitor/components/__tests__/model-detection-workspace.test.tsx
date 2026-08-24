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

import type { AxiosAdapter, AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, test } from 'vitest'

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionOverview,
  ChannelModelDetectionRunSummary,
} from '../../types-model-detection'
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
const { ChannelModelDetectionWorkspace } =
  await import('../channel-model-detection-workspace')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: { zh: { translation: {} } },
})

type RenderedWorkspace = {
  host: HTMLDivElement
  queryClient: InstanceType<typeof QueryClient>
  root: ReturnType<typeof createRoot>
}

const originalAdapter = api.defaults.adapter
const originalInnerWidth = window.innerWidth
let renderedWorkspace: RenderedWorkspace | null = null

function createCost(
  overrides: Partial<ChannelModelDetectionCost> = {}
): ChannelModelDetectionCost {
  return {
    currency: 'CNY',
    estimated_quota: 20_000,
    estimated_cost_nano_cny: 40_000_000,
    estimated_cost_cny: '0.040000000',
    cost_estimate_unknown_count: 0,
    settled_quota: 12_840,
    cost_basis_quota: 12_840,
    settled_cost_nano_cny: 25_680_000,
    settled_cost_cny: '0.025680000',
    unresolved_cost_nano_cny: 0,
    unresolved_cost_cny: '0.000000000',
    unresolved_cost_unknown_count: 0,
    settled_request_count: 64,
    unresolved_request_count: 0,
    status: 'settled',
    cost_scope: 'channel_upstream_api',
    ...overrides,
  }
}

function createRunSummary(
  overrides: Partial<ChannelModelDetectionRunSummary> = {}
): ChannelModelDetectionRunSummary {
  return {
    run_id: 'run-history-1',
    channel_id: 801,
    trigger: 'manual',
    preset: 'medium',
    preset_source: 'manual_selected',
    status: 'failed',
    target_count: 1,
    completed_target_count: 0,
    progress: {
      planned: 64,
      logical_completed: 0,
      successful: 0,
      errors: 0,
      cancelled: 0,
      http_attempts: 0,
      retries: 0,
    },
    cost: createCost({
      estimated_quota: null,
      estimated_cost_nano_cny: null,
      estimated_cost_cny: null,
      cost_estimate_unknown_count: 1,
      settled_quota: 0,
      cost_basis_quota: 0,
      settled_cost_nano_cny: null,
      settled_cost_cny: null,
      unresolved_cost_nano_cny: null,
      unresolved_cost_cny: null,
      unresolved_cost_unknown_count: 0,
      settled_request_count: 0,
      unresolved_request_count: 0,
      status: 'not_started',
    }),
    queued_at: 1_775_000_000,
    started_at: 0,
    finished_at: 1_775_000_010,
    updated_at: 1_775_000_010,
    cancel_requested_at: 0,
    error_code: 'detector_unavailable',
    error_message: '独立检测器连接失败',
    created_by_user_id: 7,
    created_by_username: 'root-admin',
    created_at: 1_775_000_000,
    ...overrides,
  }
}

function createChannel(
  overrides: Partial<ChannelModelDetectionChannel> = {}
): ChannelModelDetectionChannel {
  return {
    id: 801,
    name: '生产渠道',
    type: 1,
    channel_status: 1,
    remark: '主线路',
    groups: ['default'],
    cost_ratio: null,
    supported_models: ['gpt-5.6-sol'],
    health_status: 'healthy',
    config: {
      channel_id: 801,
      schedule_enabled: true,
      revision: 7,
      created_at: 1_775_000_000,
      updated_at: 1_775_000_000,
    },
    active_run: null,
    targets: [
      {
        target_key: 'target-sol',
        request_model: 'gpt-5.6-sol',
        claimed_model: 'gpt-5.6-sol',
        enabled: true,
        position: 0,
        latest: null,
        recent_window: [],
      },
    ],
    latest_run_cost: null,
    ...overrides,
  }
}

function createOverview(
  channelOverrides: Partial<ChannelModelDetectionChannel> = {}
): ChannelModelDetectionOverview {
  const channel = createChannel(channelOverrides)
  return {
    server_now: 1_775_000_100,
    snapshot_version: 1,
    snapshot_revision: 3,
    event_watermark: 1,
    generated_at: 1_775_000_100,
    data_cutoff_at: 1_775_000_099,
    snapshot_age_seconds: 0,
    stale: false,
    settings: {
      detector_url_configured: true,
      detector_url_masked: 'http://127.0.0.1:8000',
      scheduled_preset: 'medium',
      schedule_enabled: true,
      interval_minutes: 1440,
      display_value: 30,
      display_unit: 'day',
      next_batch_at: 1_775_086_400,
      revision: 3,
    },
    detector: {
      state: 'available',
      detector_url_configured: true,
      detector_url_masked: 'http://127.0.0.1:8000',
      busy: false,
      active_session_owned: false,
      deployment_id: 'detector-a',
      last_checked_at: 1_775_000_090,
      last_error: '',
      compatibility_message: '',
      estimates: {},
    },
    summary: {
      unconfigured: 0,
      paused: 0,
      pending: 0,
      running: channel.active_run ? 1 : 0,
      healthy: channel.active_run ? 0 : 1,
      attention: 0,
      unhealthy: 0,
      detector_unavailable: 0,
      stale: 0,
    },
    groups: ['default'],
    models: ['gpt-5.6-sol'],
    models_by_group: { default: ['gpt-5.6-sol'] },
    channels: [channel],
  }
}

function success(data: unknown) {
  return {
    data: { success: true, message: '', data },
    status: 200,
    statusText: 'OK',
    headers: {},
  }
}

function settingsResponse() {
  return {
    detector_url_configured: true,
    detector_url_masked: 'http://127.0.0.1:8000',
    pending_detector_url_configured: false,
    pending_detector_url_masked: '',
    detector_url_switch_pending: false,
    relay_url_configured: false,
    relay_url: '',
    scheduled_preset: 'medium',
    schedule_enabled: true,
    interval_minutes: 1440,
    display_value: 30,
    display_unit: 'day',
    schedule_anchor_at: 1_775_000_000,
    next_batch_at: 1_775_086_400,
    revision: 3,
    connection_test_required: false,
    created_at: 1_775_000_000,
    updated_at: 1_775_000_000,
  }
}

async function renderWorkspace(
  overview = createOverview(),
  onlyEnabled = true
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
  queryClient.setQueryData(
    ['channel-monitor', 'model-detection', 'overview'],
    overview
  )
  renderedWorkspace = { host, queryClient, root }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ChannelModelDetectionWorkspace
            overview={overview}
            filters={{
              status: 'all',
              group: '',
              model: '',
              search: '',
              sort: 'ratio_asc',
              onlyEnabled,
            }}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}

async function waitForCondition(
  condition: () => boolean,
  failureMessage: string
) {
  const deadline = Date.now() + 2000
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  while (Date.now() < deadline) {
    if (condition()) return
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
  }
  throw new Error(`${failureMessage}: ${document.body.textContent}`)
}

function findButton(label: string) {
  const button = document.querySelector<HTMLButtonElement>(
    `button[aria-label="${label}"]`
  )
  assert.ok(button, `Expected button "${label}"`)
  return button
}

beforeEach(() => {
  document.body.replaceChildren()
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: 360,
  })
})

afterEach(async () => {
  api.defaults.adapter = originalAdapter
  const current = renderedWorkspace
  if (current) {
    await act(async () => current.root.unmount())
    current.queryClient.clear()
    current.host.remove()
  }
  renderedWorkspace = null
  await act(async () => {
    toast.dismiss()
    await Promise.resolve()
  })
  document.body.replaceChildren()
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: originalInnerWidth,
  })
})

describe('模型检测工作区', () => {
  test('卡片入口打开统一设置、渠道配置和手动档位弹层', async () => {
    const requests: AxiosRequestConfig[] = []
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      return { ...success(settingsResponse()), config }
    }) as AxiosAdapter
    await renderWorkspace()

    await act(async () => findButton('打开模型检测统一设置').click())
    await waitForCondition(
      () => document.body.textContent?.includes('模型检测统一设置') === true,
      '统一设置未打开'
    )
    assert.ok(
      requests.some(
        (request) =>
          request.url === '/api/channel_monitor/model_detection/settings'
      )
    )
    assert.ok(document.querySelector('[data-slot="sheet-title"]'))

    const settingsClose = document.querySelector<HTMLButtonElement>(
      '[data-slot="sheet-close"]'
    )
    assert.ok(settingsClose)
    await act(async () => settingsClose.click())
    await waitForCondition(
      () => !document.body.textContent?.includes('模型检测统一设置'),
      '统一设置未关闭'
    )

    await act(async () => findButton('配置模型检测目标').click())
    assert.match(document.body.textContent ?? '', /配置模型检测目标/)
    const configClose = document.querySelector<HTMLButtonElement>(
      '[data-slot="sheet-close"]'
    )
    assert.ok(configClose)
    await act(async () => configClose.click())

    await act(async () => findButton('选择档位并立即检测').click())
    assert.match(document.body.textContent ?? '', /手动模型检测/)
    assert.ok(findButton('低档：请求较少'))
    assert.ok(findButton('中档：平衡档位'))
    assert.ok(findButton('高档：请求和成本更高'))
  })

  test('定时开关保留目标和 revision 并刷新模型检测总览', async () => {
    const requests: AxiosRequestConfig[] = []
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      return {
        ...success({
          channel_id: 801,
          schedule_enabled: true,
          revision: 8,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_100,
          targets: [
            {
              target_key: 'target-sol',
              request_model: 'gpt-5.6-sol',
              claimed_model: 'gpt-5.6-sol',
              enabled: true,
              position: 0,
            },
          ],
        }),
        config,
      }
    }) as AxiosAdapter
    await renderWorkspace(
      createOverview({
        config: {
          channel_id: 801,
          schedule_enabled: false,
          revision: 7,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
      }),
      false
    )

    await act(async () => findButton('参加统一定时检测').click())
    await waitForCondition(() => requests.length === 1, '定时配置请求未发出')

    assert.equal(
      requests[0]?.url,
      '/api/channel_monitor/model_detection/channel/801/config'
    )
    assert.deepEqual(JSON.parse(String(requests[0]?.data)), {
      schedule_enabled: true,
      targets: [
        {
          target_key: 'target-sol',
          request_model: 'gpt-5.6-sol',
          claimed_model: 'gpt-5.6-sol',
        },
      ],
      revision: 7,
    })
    assert.equal(
      renderedWorkspace?.queryClient.getQueryState([
        'channel-monitor',
        'model-detection',
        'overview',
      ])?.isInvalidated,
      true
    )
  })

  test('暂停所有先确认，仅更新已启用渠道并在部分失败后刷新总览', async () => {
    const overview = createOverview()
    overview.channels = [
      createChannel({
        config: {
          channel_id: 801,
          schedule_enabled: true,
          revision: 7,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
      }),
      createChannel({
        id: 802,
        name: '备用渠道',
        config: {
          channel_id: 802,
          schedule_enabled: true,
          revision: 11,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
        targets: [
          {
            target_key: 'disabled-target',
            request_model: 'gpt-5.6-sol',
            claimed_model: 'gpt-5.6-sol',
            enabled: false,
            position: 0,
            latest: null,
            recent_window: [],
          },
          {
            target_key: 'enabled-target',
            request_model: 'gpt-5.6-sol',
            claimed_model: 'gpt-5.6-sol',
            enabled: true,
            position: 1,
            latest: null,
            recent_window: [],
          },
        ],
      }),
      createChannel({
        id: 803,
        name: '已暂停渠道',
        config: {
          channel_id: 803,
          schedule_enabled: false,
          revision: 5,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
      }),
    ]
    const requests: AxiosRequestConfig[] = []
    const settleRequests: Array<() => void> = []
    api.defaults.adapter = ((config) => {
      requests.push(config)
      return new Promise((resolve, reject) => {
        if (requests.length === 1) {
          settleRequests.push(() =>
            resolve({
              ...success({
                channel_id: 801,
                schedule_enabled: false,
                revision: 8,
                created_at: 1_775_000_000,
                updated_at: 1_775_000_100,
                targets: [],
              }),
              config,
            })
          )
          return
        }
        settleRequests.push(() => reject(new Error('revision conflict')))
      })
    }) as AxiosAdapter
    await renderWorkspace(overview)

    const pauseAllButton = findButton('暂停所有模型定时检测')
    assert.equal(pauseAllButton.disabled, false)
    await act(async () => pauseAllButton.click())
    assert.equal(requests.length, 0)
    assert.match(document.body.textContent ?? '', /暂停全部模型定时检测？/)

    const confirmButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('确认暂停')
    )
    assert.ok(confirmButton)
    await act(async () => confirmButton.click())
    await waitForCondition(() => requests.length === 2, '批量暂停请求未发出')
    assert.equal(pauseAllButton.disabled, true)
    for (const button of document.querySelectorAll<HTMLButtonElement>(
      '[aria-label="退出统一定时检测"], [aria-label="参加统一定时检测"]'
    )) {
      assert.equal(button.disabled, true)
    }
    assert.deepEqual(
      requests.map((request) => ({
        url: request.url,
        body: JSON.parse(String(request.data)),
      })),
      [
        {
          url: '/api/channel_monitor/model_detection/channel/801/config',
          body: {
            schedule_enabled: false,
            targets: [
              {
                target_key: 'target-sol',
                request_model: 'gpt-5.6-sol',
                claimed_model: 'gpt-5.6-sol',
              },
            ],
            revision: 7,
          },
        },
        {
          url: '/api/channel_monitor/model_detection/channel/802/config',
          body: {
            schedule_enabled: false,
            targets: [
              {
                target_key: 'enabled-target',
                request_model: 'gpt-5.6-sol',
                claimed_model: 'gpt-5.6-sol',
              },
            ],
            revision: 11,
          },
        },
      ]
    )

    await act(async () => {
      for (const settleRequest of settleRequests) settleRequest()
      await Promise.resolve()
    })
    await waitForCondition(
      () => !document.body.textContent?.includes('暂停全部模型定时检测？'),
      '批量暂停确认未关闭'
    )
    assert.equal(
      renderedWorkspace?.queryClient.getQueryState([
        'channel-monitor',
        'model-detection',
        'overview',
      ])?.isInvalidated,
      true
    )
  })

  test('启用所有先确认，仅更新已配置但暂停的渠道', async () => {
    const overview = createOverview()
    overview.channels = [
      createChannel({
        config: {
          channel_id: 801,
          schedule_enabled: false,
          revision: 7,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
      }),
      createChannel({
        id: 802,
        name: '无效目标渠道',
        config: {
          channel_id: 802,
          schedule_enabled: false,
          revision: 11,
          created_at: 1_775_000_000,
          updated_at: 1_775_000_000,
        },
        targets: [
          {
            target_key: 'disabled-target',
            request_model: 'gpt-5.6-sol',
            claimed_model: 'gpt-5.6-sol',
            enabled: false,
            position: 0,
            latest: null,
            recent_window: [],
          },
        ],
      }),
    ]
    const requests: AxiosRequestConfig[] = []
    const settleRequests: Array<() => void> = []
    api.defaults.adapter = ((config) => {
      requests.push(config)
      return new Promise((resolve) => {
        settleRequests.push(() =>
          resolve({
            ...success({
              channel_id: Number(config.url?.match(/channel\/(\d+)/)?.[1]),
              schedule_enabled: true,
              revision: 8,
              created_at: 1_775_000_000,
              updated_at: 1_775_000_100,
              targets: [],
            }),
            config,
          })
        )
      })
    }) as AxiosAdapter

    await renderWorkspace(overview)

    const enableAllButton = findButton('启用所有模型定时检测')
    assert.equal(enableAllButton.disabled, false)
    await act(async () => enableAllButton.click())
    assert.equal(requests.length, 0)
    assert.match(document.body.textContent ?? '', /启用全部模型定时检测？/)

    const confirmButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('确认启用')
    )
    assert.ok(confirmButton)
    await act(async () => confirmButton.click())
    await waitForCondition(() => requests.length === 1, '批量启用请求未发出')
    assert.deepEqual(
      requests.map((request) => ({
        url: request.url,
        body: JSON.parse(String(request.data)),
      })),
      [
        {
          url: '/api/channel_monitor/model_detection/channel/801/config',
          body: {
            schedule_enabled: true,
            targets: [
              {
                target_key: 'target-sol',
                request_model: 'gpt-5.6-sol',
                claimed_model: 'gpt-5.6-sol',
              },
            ],
            revision: 7,
          },
        },
      ]
    )

    await act(async () => {
      for (const settleRequest of settleRequests) settleRequest()
      await Promise.resolve()
    })
    await waitForCondition(
      () => !document.body.textContent?.includes('启用全部模型定时检测？'),
      '批量启用确认未关闭'
    )
    assert.equal(
      renderedWorkspace?.queryClient.getQueryState([
        'channel-monitor',
        'model-detection',
        'overview',
      ])?.isInvalidated,
      true
    )
  })

  test('历史筛选进入真实请求并从轮次打开基础设施详情', async () => {
    const requests: AxiosRequestConfig[] = []
    const run = createRunSummary()
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      if (config.url?.endsWith('/runs/run-history-1')) {
        return { ...success({ run, executions: [] }), config }
      }
      return {
        ...success({ page: 1, page_size: 20, total: 1, items: [run] }),
        config,
      }
    }) as AxiosAdapter
    await renderWorkspace()

    await act(async () => findButton('查看 生产渠道 的模型检测记录').click())
    await waitForCondition(
      () => document.body.textContent?.includes('run-history-1') === true,
      '历史轮次未加载'
    )
    const trigger = document.querySelector<HTMLElement>(
      '[aria-label="按触发方式筛选检测轮次"]'
    )
    assert.ok(trigger)
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
    const manualOption = [
      ...document.querySelectorAll<HTMLElement>(
        '[data-slot="select-content"][data-open] [role="option"]'
      ),
    ].find((option) => option.textContent?.trim() === '手动')
    assert.ok(manualOption)
    await act(async () => manualOption.click())
    await waitForCondition(
      () =>
        requests.some(
          (request) =>
            request.url?.endsWith('/channel/801/runs') &&
            request.params?.trigger === 'manual'
        ),
      '筛选参数未进入历史请求'
    )

    await act(async () => findButton('查看轮次 run-history-1 详情').click())
    await waitForCondition(
      () => document.body.textContent?.includes('任务基础设施状态') === true,
      '轮次详情未加载'
    )
    const detailSheet = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="sheet-content"]'),
    ].find((sheet) => sheet.textContent?.includes('模型检测轮次详情'))
    assert.ok(detailSheet)
    assert.match(detailSheet.className, /w-full/)
    assert.match(detailSheet.className, /max-w-full/)
    assert.match(detailSheet.className, /overflow-x-hidden/)
    assert.match(detailSheet.className, /sm:max-w-4xl/)
    assert.match(detailSheet.textContent ?? '', /独立检测器连接失败/)
    assert.doesNotMatch(detailSheet.textContent ?? '', /检测到异常证据/)
  })

  test('重新打开轮次详情时刷新缓存的排队状态', async () => {
    const requests: AxiosRequestConfig[] = []
    const queuedRun = createRunSummary({
      status: 'queued',
      started_at: 0,
      finished_at: 0,
      updated_at: 1_775_000_000,
      error_code: '',
      error_message: '',
    })
    const completedRun = createRunSummary({
      status: 'completed',
      completed_target_count: 1,
      progress: {
        planned: 64,
        logical_completed: 64,
        successful: 64,
        errors: 0,
        cancelled: 0,
        http_attempts: 64,
        retries: 0,
      },
      started_at: 1_775_000_005,
      finished_at: 1_775_000_020,
      updated_at: 1_775_000_020,
      error_code: '',
      error_message: '',
    })
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      if (config.url?.endsWith('/runs/run-history-1')) {
        return {
          ...success({ run: completedRun, executions: [] }),
          config,
        }
      }
      return {
        ...success({
          page: 1,
          page_size: 20,
          total: 1,
          items: [completedRun],
        }),
        config,
      }
    }) as AxiosAdapter
    await renderWorkspace()

    renderedWorkspace?.queryClient.setQueryData(
      ['channel-monitor', 'model-detection', 'run', queuedRun.run_id],
      { run: queuedRun, executions: [] }
    )
    await act(async () => findButton('查看 生产渠道 的模型检测记录').click())
    await waitForCondition(
      () => document.body.textContent?.includes('run-history-1') === true,
      '历史轮次未加载'
    )

    await act(async () => findButton('查看轮次 run-history-1 详情').click())
    await waitForCondition(
      () =>
        document
          .querySelector('[data-slot="model-detection-run-detail"]')
          ?.getAttribute('data-run-status') === 'completed',
      '缓存的排队状态未刷新'
    )

    assert.equal(
      requests.filter((request) => request.url?.endsWith('/runs/run-history-1'))
        .length,
      1
    )
    assert.match(document.body.textContent ?? '', /已完成/)
  })

  test('取消先确认且请求期间锁定操作，成功后刷新总览', async () => {
    const requests: AxiosRequestConfig[] = []
    let finishCancel: (() => void) | null = null
    api.defaults.adapter = (async (config) => {
      requests.push(config)
      await new Promise<void>((resolve) => {
        finishCancel = resolve
      })
      return {
        ...success({ run_id: 'run-active-1', status: 'canceling' }),
        config,
      }
    }) as AxiosAdapter
    await renderWorkspace(
      createOverview({
        health_status: 'running',
        active_run: {
          run_id: 'run-active-1',
          status: 'running',
          trigger: 'manual',
          preset: 'medium',
          preset_source: 'manual_selected',
          progress: {
            planned: 64,
            logical_completed: 12,
            successful: 12,
            errors: 0,
            cancelled: 0,
            http_attempts: 12,
            retries: 0,
          },
          queued_at: 1_775_000_000,
          started_at: 1_775_000_010,
          updated_at: 1_775_000_020,
        },
      })
    )

    await act(async () => findButton('取消当前模型检测').click())
    assert.equal(requests.length, 0)
    assert.match(document.body.textContent ?? '', /取消当前模型检测？/)
    const confirmButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('确认取消')
    )
    assert.ok(confirmButton)
    await act(async () => confirmButton.click())
    await waitForCondition(() => requests.length === 1, '取消请求未发出')
    await waitForCondition(() => confirmButton.disabled, '取消按钮未锁定')
    assert.equal(confirmButton.disabled, true)
    assert.equal(
      requests[0]?.url,
      '/api/channel_monitor/model_detection/runs/run-active-1/cancel'
    )

    await act(async () => {
      assert.ok(finishCancel)
      finishCancel()
      await Promise.resolve()
    })
    await waitForCondition(
      () => !document.body.textContent?.includes('取消当前模型检测？'),
      '取消确认未关闭'
    )
    assert.equal(
      renderedWorkspace?.queryClient.getQueryState([
        'channel-monitor',
        'model-detection',
        'overview',
      ])?.isInvalidated,
      true
    )
  })
})
