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

import type { ChannelModelDetectionCost } from '../../types-model-detection'
import './test-dom'

const originalInnerWidth = window.innerWidth

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const {
  ChannelModelDetectionReport,
}: typeof import('../channel-model-detection-report') =
  await import('../channel-model-detection-report')
type ChannelModelDetectionExecutionDetail =
  import('../channel-model-detection-report').ChannelModelDetectionExecutionDetail

type RenderedReport = {
  host: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

let renderedReport: RenderedReport | null = null

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

function createExecution(
  overrides: Partial<ChannelModelDetectionExecutionDetail> = {}
): ChannelModelDetectionExecutionDetail {
  return {
    run_id: 'run-report-1',
    target_key: 'target-sol',
    status: 'completed',
    request_model: 'gpt-5.6',
    claimed_model: 'gpt-5.6-sol',
    outcome_code: 'juice_pass_fingerprint_strong',
    title_cn: '',
    subtitle_cn: '',
    preset: 'medium',
    preset_source: 'manual_selected',
    trigger: 'manual',
    progress: {
      planned: 64,
      logical_completed: 64,
      successful: 63,
      errors: 1,
      cancelled: 0,
      http_attempts: 66,
      retries: 2,
    },
    cost: createCost(),
    started_at: 1_775_000_010,
    finished_at: 1_775_000_080,
    updated_at: 1_775_000_080,
    official_session_id: 'official-session-123',
    official: true,
    config_hash: 'config-sha-123',
    schema_version: 3,
    scoring_version: 'trusted-fingerprint-v3',
    baseline_id: 'gpt56-fingerprint-baseline',
    baseline_sha256: 'baseline-sha-456',
    build_hash: 'build-sha-789',
    juice_verdict_state: 'pass',
    fingerprint_verdict_state: 'strong_match',
    fingerprint_model: 'gpt-5.6-sol',
    fingerprint_claim_mismatch: false,
    usage_available: true,
    input_tokens: 120_000,
    output_tokens: 8_000,
    total_tokens: 128_000,
    report_sha256: 'report-sha-abc',
    final_error_code: '',
    error_code: '',
    error_message: '',
    report: {
      overall_verdict: 'pass',
      official_grade: 'medium',
      trust_scope: 'official-baseline',
      possible_models: [
        {
          model: 'gpt-5.6-sol',
          label_cn: 'Sol',
          match: true,
          score: 0.92,
          threshold: 0.8,
        },
        {
          model: 'gpt-5.6-terra',
          label_cn: 'Terra',
          match: false,
          score: 0.42,
          threshold: 0.8,
        },
        {
          model: 'gpt-5.6-luna',
          label_cn: 'Luna',
          match: false,
          score: 0.18,
          threshold: 0.8,
        },
      ],
      failed_items: [],
    },
    ...overrides,
  }
}

async function renderReport(
  executions: ChannelModelDetectionExecutionDetail[]
) {
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  renderedReport = { host, root }
  await act(async () =>
    root.render(<ChannelModelDetectionReport executions={executions} />)
  )
}

async function cleanupRenderedReport() {
  const current = renderedReport
  if (!current) return
  await act(async () => {
    current.root.unmount()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  current.host.remove()
  renderedReport = null
}

function executionNode(targetKey: string) {
  const node = document.querySelector<HTMLElement>(
    `[data-target-key="${targetKey}"]`
  )
  assert.ok(node, `Expected execution ${targetKey}`)
  return node
}

beforeEach(() => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: 360,
  })
})

afterEach(async () => {
  await cleanupRenderedReport()
  document.body.replaceChildren()
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: originalInnerWidth,
  })
})

describe('模型检测目标报告', () => {
  test('七类已知结论使用稳定归纳级别和中文说明', async () => {
    const fixtures = [
      {
        code: 'juice_pass_fingerprint_strong',
        level: 'normal',
        text: 'Juice 通过，指纹明确',
      },
      {
        code: 'juice_pass_fingerprint_unclear',
        level: 'normal',
        text: 'Juice 通过，指纹不明确',
      },
      {
        code: 'juice_mismatch_fingerprint_strong',
        level: 'anomaly',
        text: 'Juice 与申报型号不一致，指纹明确',
      },
      {
        code: 'juice_mismatch_fingerprint_unclear',
        level: 'anomaly',
        text: 'Juice 与申报型号不一致，指纹不明确',
      },
      {
        code: 'juice_insufficient_fingerprint_strong',
        level: 'attention',
        text: 'Juice 证据不足，指纹明确',
      },
      {
        code: 'juice_insufficient_fingerprint_unclear',
        level: 'attention',
        text: 'Juice 与指纹证据均不足',
      },
      {
        code: 'possible_non_gpt',
        level: 'anomaly',
        text: '可能不是 GPT',
      },
    ] as const

    await renderReport(
      fixtures.map((fixture) =>
        createExecution({
          target_key: fixture.code,
          outcome_code: fixture.code,
          report: null,
        })
      )
    )

    for (const fixture of fixtures) {
      const node = executionNode(fixture.code)
      assert.equal(node.dataset.outcomeLevel, fixture.level)
      assert.match(node.textContent ?? '', new RegExp(fixture.text))
    }
  })

  test('低档 Juice 通过但指纹不明确不会误报模型异常', async () => {
    await renderReport([
      createExecution({
        target_key: 'low-unclear',
        preset: 'low',
        outcome_code: 'juice_pass_fingerprint_unclear',
        report: null,
      }),
    ])

    const node = executionNode('low-unclear')
    assert.equal(node.dataset.outcomeLevel, 'normal')
    assert.match(node.textContent ?? '', /低档检测出现此结果属于预期/)
    assert.doesNotMatch(node.textContent ?? '', /模型异常/)
  })

  test('Juice 通过但强指向其他型号时覆盖为模型异常', async () => {
    await renderReport([
      createExecution({
        target_key: 'pass-fingerprint-conflict',
        title_cn: 'Juice 通过；指纹强烈指向 Luna',
        fingerprint_verdict_state: 'strong_match',
        fingerprint_model: 'gpt-5.6-luna',
        report: {
          fingerprint_verdict_state: 'strong_match',
          fingerprint_model: 'gpt-5.6-luna',
          fingerprint_claim_mismatch: true,
        },
      }),
    ])

    const node = executionNode('pass-fingerprint-conflict')
    assert.equal(node.dataset.outcomeLevel, 'anomaly')
    assert.match(node.textContent ?? '', /模型异常/)
    assert.match(node.textContent ?? '', /强烈指向 Luna/)
    assert.match(node.textContent ?? '', /行为指纹与申报型号冲突/)
  })

  test('未知 outcome 原样显示并要求升级主系统适配', async () => {
    await renderReport([
      createExecution({
        target_key: 'unknown-outcome',
        outcome_code: 'future_detector_outcome_v5',
      }),
    ])

    const node = executionNode('unknown-outcome')
    assert.equal(node.dataset.outcomeLevel, 'unknown')
    assert.match(node.textContent ?? '', /future_detector_outcome_v5/)
    assert.match(node.textContent ?? '', /需要升级主系统适配/)
    assert.match(node.textContent ?? '', /不会被自动解释为正常或异常/)
  })

  test('展示型号匹配度、正式阈值、失败项目和未完成探针格', async () => {
    await renderReport([
      createExecution({
        report: {
          possible_models: [
            {
              model: 'gpt-5.6-sol',
              label_cn: 'Sol',
              match: true,
              score: 0.92,
              threshold: 0.8,
            },
            {
              model: 'gpt-5.6-terra',
              label_cn: 'Terra',
              match: false,
              score: 0.42,
              threshold: 0.75,
            },
          ],
          failed_items: [
            {
              layer: 'fingerprint',
              reason_code: 'insufficient_cells',
              reason_cn: '有效探针格不足',
              evidence: '第 4 轮未返回有效样本',
              incomplete_cells: ['effort_4/cell_2', 'effort_4/cell_3'],
              missing_current_success_efforts: [4],
              insufficient_valid_efforts: 1,
            },
          ],
        },
      }),
    ])

    const text = document.body.textContent ?? ''
    assert.match(text, /Sol匹配度 92\.0%正式阈值 80\.0%命中 是/)
    assert.match(text, /Terra匹配度 42\.0%正式阈值 75\.0%命中 否/)
    assert.match(text, /Luna匹配度 未提供正式阈值 未提供命中 未提供/)
    assert.match(text, /有效探针格不足/)
    assert.match(text, /insufficient_cells/)
    assert.match(text, /effort_4\/cell_2/)
    assert.match(text, /缺少当前成功 effort/)
    assert.match(text, /有效 effort 不足/)
  })

  test('展示 session、官方标记和所有报告版本身份', async () => {
    await renderReport([createExecution()])

    const text = document.body.textContent ?? ''
    assert.match(text, /official-session-123/)
    assert.match(text, /是（官方报告）/)
    assert.match(text, /config-sha-123/)
    assert.match(text, /Schema 版本3/)
    assert.match(text, /trusted-fingerprint-v3/)
    assert.match(text, /gpt56-fingerprint-baseline/)
    assert.match(text, /baseline-sha-456/)
    assert.match(text, /build-sha-789/)
    assert.match(text, /report-sha-abc/)
  })

  test('Usage 可用时显示 Token，不可用时明确提示而不显示零值', async () => {
    await renderReport([
      createExecution({ target_key: 'usage-available' }),
      createExecution({
        target_key: 'usage-unavailable',
        usage_available: false,
        input_tokens: 0,
        output_tokens: 0,
        total_tokens: 0,
      }),
    ])

    const available = executionNode('usage-available').textContent ?? ''
    assert.match(available, /输入 Token120,000/)
    assert.match(available, /输出 Token8,000/)
    assert.match(available, /总 Token128,000/)

    const unavailable = executionNode('usage-unavailable').textContent ?? ''
    assert.match(unavailable, /Usage 暂不可用/)
    assert.doesNotMatch(unavailable, /输入 Token0/)
    assert.doesNotMatch(unavailable, /输出 Token0/)
  })

  test('settled、unresolved、unknown 和 not_started 成本语义严格分开', async () => {
    await renderReport([
      createExecution({
        target_key: 'settled-cost',
        cost: createCost({
          unresolved_cost_nano_cny: 0,
          unresolved_cost_cny: '0.000000000',
          unresolved_cost_unknown_count: 0,
          unresolved_request_count: 0,
          status: 'settled',
        }),
      }),
      createExecution({
        target_key: 'unresolved-cost',
        cost: createCost({
          settled_quota: 0,
          cost_basis_quota: 0,
          settled_cost_nano_cny: 0,
          settled_cost_cny: '0.000000000',
          settled_request_count: 0,
          unresolved_cost_nano_cny: 8_000_000,
          unresolved_cost_cny: '0.008000000',
          unresolved_cost_unknown_count: 0,
          unresolved_request_count: 2,
          status: 'unresolved',
        }),
      }),
      createExecution({
        target_key: 'unknown-cost',
        cost: createCost({
          settled_request_count: 0,
          settled_cost_nano_cny: 0,
          settled_cost_cny: '0.000000000',
          unresolved_cost_nano_cny: null,
          unresolved_cost_cny: null,
          unresolved_cost_unknown_count: 3,
          unresolved_request_count: 3,
          status: 'unresolved',
        }),
      }),
      createExecution({
        target_key: 'not-started-cost',
        cost: createCost({
          settled_quota: 0,
          cost_basis_quota: 0,
          settled_cost_nano_cny: 0,
          settled_cost_cny: '0.000000000',
          settled_request_count: 0,
          unresolved_cost_nano_cny: 0,
          unresolved_cost_cny: '0.000000000',
          unresolved_cost_unknown_count: 0,
          unresolved_request_count: 0,
          status: 'not_started',
        }),
      }),
    ])

    const settled = executionNode('settled-cost').textContent ?? ''
    assert.match(settled, /等价已结算额度12,840/)
    assert.match(settled, /计价基数13,200/)
    assert.match(settled, /已结算渠道成本¥0\.025680000/)
    assert.match(settled, /真实上游请求数63/)

    const unresolved = executionNode('unresolved-cost').textContent ?? ''
    assert.match(unresolved, /待核实预计成本¥0\.008000000/)
    assert.match(unresolved, /待核实请求数2/)

    const unknown = executionNode('unknown-cost').textContent ?? ''
    assert.match(unknown, /待核实预计成本暂无法估算/)
    assert.match(unknown, /无法估算请求数3/)

    const notStarted = executionNode('not-started-cost').textContent ?? ''
    assert.match(notStarted, /尚未发出上游请求/)
    assert.match(notStarted, /真实上游请求数0/)
  })

  test('空金额不会被格式化为零成本', async () => {
    await renderReport([
      createExecution({
        cost: createCost({
          estimated_quota: null,
          estimated_cost_nano_cny: null,
          estimated_cost_cny: null,
          settled_request_count: 1,
          settled_cost_nano_cny: null,
          settled_cost_cny: null,
          unresolved_request_count: 1,
          unresolved_cost_nano_cny: null,
          unresolved_cost_cny: null,
          unresolved_cost_unknown_count: 1,
          status: 'partial',
        }),
      }),
    ])

    const text = document.body.textContent ?? ''
    assert.match(text, /运行前预计渠道成本暂无法估算/)
    assert.match(text, /运行前预计等价额度暂无法估算/)
    assert.match(text, /已结算渠道成本暂无法估算/)
    assert.match(text, /待核实预计成本暂无法估算/)
    assert.doesNotMatch(text, /¥0\.000000000/)
  })

  test('技术 JSON 默认收起并再次隐藏敏感键和值但保留 session_id', async () => {
    await renderReport([
      createExecution({
        report: {
          session_id: 'official-session-visible',
          authorization: 'Bearer top-secret-token',
          api_key: 'sk-super-secret-key',
          nested: {
            session_token: 'session-secret',
            detector_url: 'https://detector.internal.example/run',
            note: 'authorization=Bearer another-secret',
          },
        },
        error_message:
          'request failed authorization=Bearer runtime-secret detector_url=https://detector.internal.example/run',
      }),
    ])

    const trigger = document.querySelector<HTMLButtonElement>(
      '[data-slot="accordion-trigger"]'
    )
    assert.ok(trigger)
    assert.equal(trigger.getAttribute('aria-expanded'), 'false')

    await act(async () => {
      trigger.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const technicalJson = document.querySelector<HTMLElement>(
      '[data-slot="model-detection-technical-json"]'
    )
    assert.ok(technicalJson)
    const text = technicalJson.textContent ?? ''
    assert.match(text, /official-session-visible/)
    assert.match(text, /\[已隐藏\]/)
    assert.doesNotMatch(text, /top-secret-token/)
    assert.doesNotMatch(text, /super-secret-key/)
    assert.doesNotMatch(text, /session-secret/)
    assert.doesNotMatch(text, /detector\.internal\.example/)
    assert.doesNotMatch(document.body.textContent ?? '', /runtime-secret/)
  })

  test('360px 布局限制横向溢出并允许长技术字段换行', async () => {
    await renderReport([
      createExecution({
        request_model: `gpt-${'very-long-model-name-'.repeat(12)}`,
        official_session_id: 'session'.repeat(60),
        report: { long_value: 'value'.repeat(100) },
      }),
    ])

    const root = document.querySelector<HTMLElement>(
      '[data-slot="model-detection-report"]'
    )
    const execution = document.querySelector<HTMLElement>(
      '[data-slot="model-detection-execution-report"]'
    )
    assert.ok(root)
    assert.ok(execution)
    assert.match(root.className, /min-w-0/)
    assert.match(root.className, /overflow-x-hidden/)
    assert.match(execution.className, /min-w-0/)
    assert.match(execution.className, /overflow-x-hidden/)
    assert.ok(document.querySelector('.break-all'))
  })
})
