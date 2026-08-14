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
import { afterEach, beforeEach, describe, test } from 'node:test'

import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
} from '../../types-model-detection'
import './test-dom'

const originalInnerWidth = window.innerWidth

const { act, useState } = await import('react')
const { createRoot } = await import('react-dom/client')
const {
  ChannelModelDetectionHistorySheet,
}: typeof import('../channel-model-detection-history-sheet') =
  await import('../channel-model-detection-history-sheet')
type ChannelModelDetectionHistoryQuery =
  import('../channel-model-detection-history-sheet').ChannelModelDetectionHistoryQuery
type ChannelModelDetectionRunHistoryPage =
  import('../channel-model-detection-history-sheet').ChannelModelDetectionRunHistoryPage
type ChannelModelDetectionRunSummary =
  import('../channel-model-detection-history-sheet').ChannelModelDetectionRunSummary

type RenderedHistory = {
  host: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

type HarnessProps = {
  channel: ChannelModelDetectionChannel
  initialQuery: ChannelModelDetectionHistoryQuery
  data?: ChannelModelDetectionRunHistoryPage
  loading?: boolean
  refreshing?: boolean
  error?: string | null
  onQueryChange?: (query: ChannelModelDetectionHistoryQuery) => void
  onRefresh?: () => void
  onOpenRun?: (run: ChannelModelDetectionRunSummary) => void
}

let renderedHistory: RenderedHistory | null = null

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
    cost_basis_quota: 13_200,
    settled_cost_nano_cny: 25_680_000,
    settled_cost_cny: '0.025680000',
    unresolved_cost_nano_cny: 8_000_000,
    unresolved_cost_cny: '0.008000000',
    unresolved_cost_unknown_count: 1,
    settled_request_count: 63,
    unresolved_request_count: 1,
    status: 'partial',
    cost_scope: 'channel_upstream_api',
    ...overrides,
  }
}

function createChannel(): ChannelModelDetectionChannel {
  return {
    id: 12,
    name: '模型检测渠道',
    type: 1,
    channel_status: 1,
    remark: '主线路',
    groups: ['default'],
    cost_ratio: null,
    supported_models: ['gpt-5.6'],
    health_status: 'healthy',
    config: {
      channel_id: 12,
      schedule_enabled: true,
      revision: 1,
      created_at: 1_775_000_000,
      updated_at: 1_775_000_000,
    },
    active_run: null,
    targets: [],
    latest_run_cost: null,
  }
}

function createRun(
  overrides: Partial<ChannelModelDetectionRunSummary> = {}
): ChannelModelDetectionRunSummary {
  return {
    run_id: 'run-manual-high',
    channel_id: 12,
    trigger: 'manual',
    preset: 'high',
    preset_source: 'manual_selected',
    status: 'partial',
    target_count: 3,
    completed_target_count: 2,
    progress: {
      planned: 202,
      logical_completed: 138,
      successful: 136,
      errors: 2,
      cancelled: 0,
      http_attempts: 141,
      retries: 3,
    },
    cost: createCost(),
    queued_at: 1_775_000_000,
    started_at: 1_775_000_010,
    finished_at: 1_775_000_080,
    updated_at: 1_775_000_080,
    cancel_requested_at: 0,
    error_code: '',
    error_message: '',
    created_by_user_id: 7,
    created_by_username: 'root-admin',
    created_at: 1_775_000_000,
    ...overrides,
  }
}

function createQuery(
  overrides: Partial<ChannelModelDetectionHistoryQuery> = {}
): ChannelModelDetectionHistoryQuery {
  return {
    page: 1,
    page_size: 20,
    trigger: '',
    status: '',
    model: '',
    outcome: '',
    ...overrides,
  }
}

function createPage(
  items: ChannelModelDetectionRunSummary[],
  overrides: Partial<ChannelModelDetectionRunHistoryPage> = {}
): ChannelModelDetectionRunHistoryPage {
  return {
    page: 1,
    page_size: 20,
    total: items.length,
    items,
    ...overrides,
  }
}

function HistoryHarness(props: HarnessProps) {
  const [query, setQuery] = useState(props.initialQuery)
  return (
    <ChannelModelDetectionHistorySheet
      channel={props.channel}
      open
      query={query}
      data={props.data}
      loading={props.loading}
      refreshing={props.refreshing}
      error={props.error}
      onOpenChange={() => undefined}
      onQueryChange={(nextQuery) => {
        props.onQueryChange?.(nextQuery)
        setQuery(nextQuery)
      }}
      onRefresh={props.onRefresh}
      onOpenRun={props.onOpenRun}
    />
  )
}

async function renderHistory(props: HarnessProps) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  renderedHistory = { host, root }
  await act(async () => root.render(<HistoryHarness {...props} />))
}

async function cleanupRenderedHistory() {
  const current = renderedHistory
  if (!current) return
  const closeButton = document.querySelector<HTMLButtonElement>(
    '[data-slot="sheet-close"]'
  )
  if (closeButton) {
    await act(async () => {
      closeButton.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
  await act(async () => {
    current.root.unmount()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  current.host.remove()
  renderedHistory = null
}

function findButton(label: string) {
  const button = document.querySelector<HTMLButtonElement>(
    `button[aria-label="${label}"]`
  )
  assert.ok(button, `Expected button "${label}"`)
  return button
}

function findControl(label: string) {
  const control = document.querySelector<HTMLElement>(`[aria-label="${label}"]`)
  assert.ok(control, `Expected control "${label}"`)
  return control
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
  await act(async () => {
    option.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function changeInput(input: HTMLInputElement, value: string) {
  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(valueSetter)
    valueSetter.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

beforeEach(() => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: 360,
  })
})

afterEach(async () => {
  await cleanupRenderedHistory()
  document.body.replaceChildren()
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: originalInnerWidth,
  })
})

describe('模型检测历史 Sheet', () => {
  test('展示触发方式、实际档位、状态、进度、时间和创建管理员', async () => {
    const run = createRun()
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      data: createPage([run]),
    })

    const text = document.body.textContent ?? ''
    assert.match(text, /手动 · 高档/)
    assert.match(text, /部分完成/)
    assert.match(text, /逻辑完成 138 \/ 202/)
    assert.match(text, /目标 2 \/ 3/)
    assert.match(text, new RegExp(formatTimestampToDate(run.queued_at)))
    assert.match(text, new RegExp(formatTimestampToDate(run.started_at)))
    assert.match(text, new RegExp(formatTimestampToDate(run.finished_at)))
    assert.match(text, /root-admin/)
    assert.equal(
      document.querySelector('[data-slot="sheet-title"]')?.textContent,
      '模型检测历史'
    )
  })

  test('已结算和待核实成本分开显示并保留等价额度与计价基数', async () => {
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      data: createPage([createRun()]),
    })

    const text = document.body.textContent ?? ''
    assert.match(text, /预计成本 ¥0\.040000000/)
    assert.match(text, /预计额度 20,000/)
    assert.match(text, /等价已结算额度 12,840/)
    assert.match(text, /计价基数 13,200/)
    assert.match(text, /已结算渠道成本 ¥0\.025680000/)
    assert.match(text, /待核实预计成本 ¥0\.008000000/)
    assert.match(text, /无法估算请求数 1/)
  })

  test('未知金额显示暂无法估算且不会把空值格式化为零', async () => {
    const cost = createCost({
      estimated_quota: null,
      estimated_cost_nano_cny: null,
      estimated_cost_cny: null,
      cost_estimate_unknown_count: 2,
      settled_quota: 0,
      cost_basis_quota: 0,
      settled_cost_nano_cny: 0,
      settled_cost_cny: '0.000000000',
      settled_request_count: 0,
      unresolved_cost_nano_cny: null,
      unresolved_cost_cny: null,
      unresolved_cost_unknown_count: 2,
      unresolved_request_count: 2,
      status: 'unresolved',
    })
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      data: createPage([createRun({ cost })]),
    })

    const text = document.body.textContent ?? ''
    assert.match(text, /预计成本暂无法估算/)
    assert.match(text, /预计额度暂无法估算/)
    assert.match(text, /待核实预计成本暂无法估算/)
    assert.doesNotMatch(text, /¥0\.000000000/)
  })

  test('触发方式、状态、模型和结论使用 API 字段并将筛选重置到第一页', async () => {
    const changes: ChannelModelDetectionHistoryQuery[] = []
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery({
        page: 4,
        page_size: 50,
        model: 'gpt-5.6-old',
      }),
      data: createPage([], { page: 4, page_size: 50, total: 0 }),
      onQueryChange: (query) => changes.push(query),
    })

    const trigger = findControl('按触发方式筛选检测轮次')
    const status = findControl('按状态筛选检测轮次')
    const model = findControl('按请求模型筛选检测轮次') as HTMLInputElement
    const outcome = findControl('按结论筛选检测轮次')
    assert.equal(trigger.dataset.apiField, 'trigger')
    assert.equal(status.dataset.apiField, 'status')
    assert.equal(model.dataset.apiField, 'model')
    assert.equal(outcome.dataset.apiField, 'outcome')

    await chooseOption(trigger, '手动')
    assert.deepEqual(changes.at(-1), {
      page: 1,
      page_size: 50,
      trigger: 'manual',
      status: '',
      model: 'gpt-5.6-old',
      outcome: '',
    })
    await chooseOption(status, '失败')
    assert.equal(changes.at(-1)?.status, 'failed')
    assert.equal(changes.at(-1)?.page, 1)
    await changeInput(model, 'gpt-5.6')
    assert.equal(changes.at(-1)?.model, 'gpt-5.6')
    assert.equal(changes.at(-1)?.page, 1)
    await chooseOption(outcome, '可能不是 GPT')
    assert.equal(changes.at(-1)?.outcome, 'possible_non_gpt')
    assert.equal(changes.at(-1)?.page, 1)
  })

  test('上一页和下一页保留筛选条件并只更新页码', async () => {
    const changes: ChannelModelDetectionHistoryQuery[] = []
    const filteredQuery = createQuery({
      page: 2,
      page_size: 10,
      trigger: 'scheduled',
      status: 'completed',
      model: 'gpt-5.6',
      outcome: 'juice_pass_fingerprint_strong',
    })
    await renderHistory({
      channel: createChannel(),
      initialQuery: filteredQuery,
      data: createPage([createRun()], {
        page: 2,
        page_size: 10,
        total: 40,
      }),
      onQueryChange: (query) => changes.push(query),
    })

    await act(async () => findButton('上一页').click())
    assert.deepEqual(changes.at(-1), { ...filteredQuery, page: 1 })
    await act(async () => findButton('下一页').click())
    assert.deepEqual(changes.at(-1), { ...filteredQuery, page: 2 })
  })

  test('加载、错误和空状态彼此独立并提供显式重试', async () => {
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      loading: true,
    })
    assert.ok(document.querySelector('[aria-label="正在加载检测历史"]'))

    await cleanupRenderedHistory()
    let refreshCount = 0
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      error: '请求超时',
      onRefresh: () => {
        refreshCount += 1
      },
    })
    assert.match(document.body.textContent ?? '', /检测历史加载失败/)
    assert.match(document.body.textContent ?? '', /请求超时/)
    const retryButton = [...document.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('重试')
    )
    assert.ok(retryButton)
    await act(async () => retryButton.click())
    assert.equal(refreshCount, 1)

    await cleanupRenderedHistory()
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      data: createPage([]),
    })
    assert.match(document.body.textContent ?? '', /暂无检测记录/)
  })

  test('宽 Sheet 在 360px 下约束横向溢出且历史列表不包含完整报告 JSON', async () => {
    const run = createRun({
      error_message: 'authorization: secret-value 上游连接失败',
    })
    await renderHistory({
      channel: createChannel(),
      initialQuery: createQuery(),
      data: createPage([run]),
    })

    const sheet = document.querySelector<HTMLElement>(
      '[data-slot="sheet-content"]'
    )
    const filters = document.querySelector<HTMLElement>(
      '[data-slot="model-detection-history-filters"]'
    )
    assert.ok(sheet)
    assert.ok(filters)
    assert.match(sheet.className, /w-full/)
    assert.match(sheet.className, /max-w-full/)
    assert.match(sheet.className, /min-w-0/)
    assert.match(sheet.className, /overflow-x-hidden/)
    assert.match(sheet.className, /sm:max-w-3xl/)
    assert.match(filters.className, /grid-cols-1/)
    assert.match(filters.className, /sm:grid-cols-2/)
    assert.match(document.body.textContent ?? '', /authorization=\*\*\*/)
    assert.doesNotMatch(document.body.textContent ?? '', /secret-value/)
    assert.doesNotMatch(document.body.textContent ?? '', /"report"\s*:/)
  })
})
