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

import { afterEach, beforeEach, describe, test } from 'vitest'

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
    schema_version: 4,
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
      schema_version: 4,
      scoring_version: 'trusted-fingerprint-v3',
      overall_verdict: 'pass',
      official_grade: true,
      trust_scope: 'official_preset',
      possible_models: [
        {
          model: 'gpt-5.6-sol',
          label_cn: 'Sol',
          match: 0.920_549_805_582_057_7,
          score: -2.291_608_932_175_692_3,
          threshold: 0.8,
        },
        {
          model: 'gpt-5.6-terra',
          label_cn: 'Terra',
          match: 0.069_898_587_567_904_5,
          score: -4.869_534_595_913_43,
          threshold: 0.8,
        },
        {
          model: 'gpt-5.6-luna',
          label_cn: 'Luna',
          match: 0.009_551_606_850_037_782,
          score: -6.859_870_641_593_742_5,
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

  test('优先显示检测器报告中的原始中文标题和副标题', async () => {
    await renderReport([
      createExecution({
        title_cn: '数据库旧标题',
        subtitle_cn: '数据库旧说明',
        report: {
          schema_version: 4,
          scoring_version: 'trusted-fingerprint-v3',
          title_cn: 'Juice通过；指纹强烈指向 Sol',
          subtitle_cn:
            'Juice 未发现型号冲突；本批行为分布与 Sol 的可信指纹最接近。',
        },
      }),
    ])

    const text = document.body.textContent ?? ''
    assert.match(text, /Juice通过；指纹强烈指向 Sol/)
    assert.match(text, /本批行为分布与 Sol 的可信指纹最接近/)
    assert.doesNotMatch(text, /数据库旧标题/)
    assert.doesNotMatch(text, /数据库旧说明/)
  })

  test('缺少报告 Schema 时不提示不兼容并展示检测器结论', async () => {
    await renderReport([
      createExecution({
        target_key: 'missing-schema',
        schema_version: 0,
        report: {
          scoring_version: 'trusted-fingerprint-v3',
          outcome_code: 'juice_pass_fingerprint_strong',
          title_cn: '无 Schema 的检测器结论',
        },
      }),
    ])

    const node = executionNode('missing-schema')
    assert.equal(node.dataset.outcomeLevel, 'normal')
    assert.match(node.textContent ?? '', /无 Schema 的检测器结论/)
    assert.doesNotMatch(node.textContent ?? '', /报告未声明 Schema/)
    assert.doesNotMatch(node.textContent ?? '', /报告版本不兼容/)
  })

  test('新版报告 Schema 不阻断检测器结论', async () => {
    await renderReport([
      createExecution({
        target_key: 'unsupported-schema',
        schema_version: 5,
        scoring_version: 'trusted-fingerprint-v4',
        report: {
          schema_version: 5,
          scoring_version: 'trusted-fingerprint-v4',
          outcome_code: 'juice_pass_fingerprint_strong',
          title_cn: '未来版本声称检测正常',
          subtitle_cn: '此字段语义未经当前主系统验证。',
        },
      }),
    ])

    const node = executionNode('unsupported-schema')
    assert.equal(node.dataset.outcomeLevel, 'normal')
    assert.match(node.textContent ?? '', /未来版本声称检测正常/)
    assert.match(node.textContent ?? '', /此字段语义未经当前主系统验证/)
    assert.doesNotMatch(node.textContent ?? '', /Schema 5 不受支持/)
    assert.doesNotMatch(node.textContent ?? '', /报告版本提示/)
  })

  test('报告请求模型与执行快照不一致时停止解释检测结论', async () => {
    await renderReport([
      createExecution({
        target_key: 'request-model-mismatch',
        request_model: 'upstream-model-alias',
        schema_version: 3,
        report: {
          schema_version: 3,
          scoring_version: 'trusted-fingerprint-v3',
          claimed_model: 'gpt-5.6-sol',
          candidate_configuration_without_key: {
            model: 'gpt-5.6-sol',
          },
          outcome_code: 'juice_pass_fingerprint_strong',
          title_cn: '旧检测器声称检测正常',
        },
      }),
    ])

    const node = executionNode('request-model-mismatch')
    assert.equal(node.dataset.outcomeLevel, 'unknown')
    assert.match(node.textContent ?? '', /报告版本不兼容/)
    assert.match(node.textContent ?? '', /报告请求模型 gpt-5\.6-sol/)
    assert.match(node.textContent ?? '', /执行快照 upstream-model-alias/)
    assert.match(node.textContent ?? '', /可能不支持独立请求模型/)
    assert.doesNotMatch(node.textContent ?? '', /旧检测器声称检测正常/)
  })

  test('展示型号匹配度、正式阈值、失败项目和未完成探针格', async () => {
    await renderReport([
      createExecution({
        report: {
          possible_models: [
            {
              model: 'gpt-5.6-sol',
              label_cn: 'Sol',
              match: 0.920_549_805_582_057_7,
              score: -2.291_608_932_175_692_3,
              threshold: 0.8,
            },
            {
              model: 'gpt-5.6-terra',
              label_cn: 'Terra',
              match: 0.069_898_587_567_904_5,
              score: -4.869_534_595_913_43,
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
    const sol = document.querySelector<HTMLElement>('[data-model-match="Sol"]')
    const terra = document.querySelector<HTMLElement>(
      '[data-model-match="Terra"]'
    )
    const luna = document.querySelector<HTMLElement>(
      '[data-model-match="Luna"]'
    )
    assert.ok(sol)
    assert.ok(terra)
    assert.ok(luna)
    assert.match(sol.textContent ?? '', /92\.055%强指向线 >80%/)
    assert.match(terra.textContent ?? '', /6\.990%强指向线 >75%/)
    assert.match(luna.textContent ?? '', /未提供当前模式仅参考/)
    assert.doesNotMatch(text, /-2\.2916/)
    assert.doesNotMatch(text, /-4\.8695/)
    assert.match(text, /有效探针格不足/)
    assert.match(text, /insufficient_cells/)
    assert.match(text, /effort_4\/cell_2/)
    assert.match(text, /缺少当前成功 effort/)
    assert.match(text, /有效 effort 不足/)
  })

  test('指纹匹配区展示检测器结论、验证参考、不明确原因和自定义参考探针', async () => {
    await renderReport([
      createExecution({
        fingerprint_verdict_state: 'unclear',
        fingerprint_model: '',
        report: {
          custom_preset: true,
          custom_changes: ['增加自定义天气探针'],
          fingerprint_verdict_state: 'unclear',
          fingerprint_details: [
            {
              model: 'gpt-5.6-sol',
              label_cn: 'Sol',
              match: 0.02968,
              threshold: 0.82,
            },
            {
              model: 'gpt-5.6-terra',
              label_cn: 'Terra',
              match: 0.00513,
              threshold: 0.84,
            },
            {
              model: 'gpt-5.6-luna',
              label_cn: 'Luna',
              match: 0.96519,
              threshold: 0.97,
            },
          ],
          fingerprint_summary: {
            fingerprint_unclear_reasons_cn: [
              '三个模型都没有越过当前档位的强指向线。',
            ],
          },
          reference_fingerprint_results: [
            {
              probe_id: 'custom_weather_probe',
              fingerprint_match: {
                'gpt-5.6-sol': 0.7,
                'gpt-5.6-terra': 0.2,
                'gpt-5.6-luna': 0.1,
              },
            },
          ],
        },
      }),
    ])

    const referenceTrigger = document.querySelector<HTMLButtonElement>(
      '[data-report-section="fingerprint-reference"]'
    )
    assert.ok(referenceTrigger)
    assert.equal(referenceTrigger.getAttribute('aria-expanded'), 'false')
    await act(async () => {
      referenceTrigger.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    const text = document.body.textContent ?? ''
    assert.match(text, /指纹匹配度/)
    assert.match(text, /证据不明确/)
    assert.match(text, /自定义档位测试结果仅供参考/)
    assert.match(text, /修改项目：增加自定义天气探针/)
    assert.match(text, /现有验证参考与适用边界/)
    assert.match(text, /历史模拟平均/)
    assert.match(text, /低Sol91\.297%54\.645%>54%/)
    assert.match(text, /三个模型都没有越过当前档位的强指向线/)
    assert.match(text, /custom weather probe（自定义参考）/)
    assert.match(text, /Sol 70\.000%/)
  })

  test('粘性确定性异常和未完成探针格按检测器报告语义展开', async () => {
    await renderReport([
      createExecution({
        report: {
          output_integrity_summary: {
            requests: 2,
            exact: 1,
            invalid: 0,
            hard_anomaly: false,
            sticky_hard_anomaly: true,
          },
          coverage_summary: {
            requests: 2,
            hard_anomaly: false,
            sticky_hard_anomaly: true,
          },
          failed_items: [
            {
              layer: '指纹匹配',
              reason_code: 'candidate_samples_incomplete',
              reason_cn: '至少一个探针格未完成计划请求数的90%。',
              incomplete_cells: [
                {
                  cell: 'rand_country|normal+no_history',
                  planned: 10,
                  completed: 7,
                  minimum: 9,
                },
              ],
            },
          ],
        },
      }),
    ])

    const text = document.body.textContent ?? ''
    assert.match(text, /来自本会话历史粘性事件/)
    assert.match(text, /固定随机国家 · 普通请求 · 无历史/)
    assert.match(text, /计划 10，完成 7，至少需要 9，缺少 2/)
  })

  test('展示 session、官方标记和所有报告版本身份', async () => {
    await renderReport([createExecution()])

    const text = document.body.textContent ?? ''
    assert.match(text, /official-session-123/)
    assert.match(text, /是（官方报告）/)
    assert.match(text, /config-sha-123/)
    assert.match(text, /Schema 版本4/)
    assert.match(text, /trusted-fingerprint-v3/)
    assert.match(text, /gpt56-fingerprint-baseline/)
    assert.match(text, /baseline-sha-456/)
    assert.match(text, /build-sha-789/)
    assert.match(text, /report-sha-abc/)
  })

  test('按检测器报告展示 Juice、完整性、线路、探针和请求格式数据', async () => {
    await renderReport([
      createExecution({
        report: {
          schema_version: 4,
          scoring_version: 'trusted-fingerprint-v3',
          juice_summary: {
            per_effort: {
              high: {
                attempted: 6,
                valid_completed: 6,
                current_success: 6,
                mixed: 0,
                unsuccessful: 0,
                network_error: 0,
                shared_current_success: 0,
              },
            },
          },
          output_integrity_summary: {
            requests: 2,
            exact: 2,
            invalid: 0,
            hard_anomaly: false,
            sticky_hard_anomaly: false,
          },
          coverage_summary: {
            requests: 2,
            hard_anomaly: false,
            sticky_hard_anomaly: false,
          },
          network_summary: {
            logical_tasks: 49,
            logical_completed: 49,
            successful: 49,
            final_errors: 0,
            cancelled: 0,
            http_attempts: 49,
            retries: 0,
          },
          fingerprint_summary: {
            cell_details: {
              'rand_country|normal+no_history': {
                probe_id: 'rand_country',
                profile: 'normal+no_history',
                sample_count: 10,
                planned_samples: 10,
                counts: { uruguay: 5, portugal: 3, madagascar: 1 },
                average_log_likelihood: {
                  'gpt-5.6-sol': -1.61,
                  'gpt-5.6-terra': -3.99,
                  'gpt-5.6-luna': -4.2,
                },
                between_model_jsd: 0.513,
                within_model_jsd: 0.045,
                weight: 0.913,
                complete: true,
              },
            },
          },
          profile_summary: {
            'normal+no_history': {
              logical_tasks: 49,
              successful: 49,
              final_errors: 0,
              cancelled: 0,
            },
          },
          failed_items: [],
          network_error_details: [
            {
              probe_id: 'rand_bird',
              category_cn: '上游限流',
              http_status: 429,
              attempt: 2,
              safe_message: '请求频率过高',
            },
          ],
        },
      }),
    ])

    const text = document.body.textContent ?? ''
    assert.match(text, /Juice 结果/)
    assert.match(text, /高6660000/)
    assert.match(text, /32\/48 输出完整性/)
    assert.match(text, /成功响应 2 条，精确返回 2 条，格式无效 0 条/)
    assert.match(text, /线路质量/)
    assert.match(text, /逻辑任务49/)
    assert.match(text, /逻辑完成49/)
    assert.match(text, /行为指纹探针/)
    assert.match(text, /固定随机国家/)
    assert.match(text, /uruguay 5；portugal 3；madagascar 1/)
    assert.match(text, /更支持 Sol/)
    assert.match(text, /请求格式对比/)
    assert.match(text, /普通请求 · 无历史494900/)
    assert.match(text, /固定随机鸟 · 上游限流/)
    assert.match(text, /HTTP 429，第 2 次尝试/)
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

  test('详情不展示主系统渠道成本字段', async () => {
    await renderReport([
      createExecution({
        target_key: 'hidden-cost',
        cost: createCost({
          unresolved_cost_nano_cny: 8_000_000,
          unresolved_cost_cny: '0.008000000',
          unresolved_cost_unknown_count: 3,
          unresolved_request_count: 2,
          status: 'partial',
        }),
      }),
    ])

    const text = executionNode('hidden-cost').textContent ?? ''
    assert.doesNotMatch(text, /渠道成本详情/)
    assert.doesNotMatch(text, /等价已结算额度/)
    assert.doesNotMatch(text, /已结算渠道成本/)
    assert.doesNotMatch(text, /待核实成本/)
    assert.doesNotMatch(text, /0\.008000000/)
  })

  test('技术 JSON 默认收起并再次隐藏敏感键和值但保留 session_id', async () => {
    await renderReport([
      createExecution({
        report: {
          session_id: 'official-session-visible',
          limitations: ['指纹匹配度不是真实路由概率'],
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
      '[data-report-section="model-detection-technical"]'
    )
    assert.ok(trigger)
    assert.equal(trigger.getAttribute('aria-expanded'), 'false')
    assert.match(trigger.textContent ?? '', /方法、限制和 JSON 摘要/)

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
    assert.match(document.body.textContent ?? '', /指纹匹配度不是真实路由概率/)
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
