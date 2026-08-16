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
import { describe, test } from 'node:test'

import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionExecutionStatus,
  ChannelModelDetectionOutcomeCode,
} from '../../types-model-detection'
import {
  channelModelDetectionCostLines,
  channelModelDetectionResultLabel,
  channelModelDetectionResultTone,
  filterChannelModelDetectionChannels,
  isChannelModelDetectionRunActive,
  sortChannelModelDetectionChannels,
} from '../model-detection'

function createCost(overrides: Partial<ChannelModelDetectionCost> = {}) {
  return {
    currency: 'CNY',
    estimated_quota: null,
    estimated_cost_nano_cny: null,
    estimated_cost_cny: null,
    cost_estimate_unknown_count: 0,
    settled_quota: 0,
    cost_basis_quota: 0,
    settled_cost_nano_cny: 0,
    settled_cost_cny: '0.000000000',
    unresolved_cost_nano_cny: 0,
    unresolved_cost_cny: '0.000000000',
    unresolved_cost_unknown_count: 0,
    settled_request_count: 0,
    unresolved_request_count: 0,
    status: 'not_started',
    cost_scope: 'channel_upstream_api',
    ...overrides,
  } satisfies ChannelModelDetectionCost
}

function createChannel(id: number): ChannelModelDetectionChannel {
  return {
    id,
    name: id === 1 ? '主渠道' : '备用渠道',
    type: 1,
    channel_status: 1,
    remark: id === 1 ? '华东主线路' : '华北备用',
    groups: id === 1 ? ['default'] : ['vip'],
    cost_ratio: null,
    supported_models: ['gpt-5.6'],
    health_status: id === 1 ? 'healthy' : 'attention',
    config: null,
    active_run: null,
    targets: [
      {
        target_key: `target-${id}`,
        request_model: id === 1 ? 'gpt-5.6' : 'gpt-5.6-terra',
        claimed_model: id === 1 ? 'gpt-5.6-sol' : 'gpt-5.6-terra',
        enabled: true,
        position: 0,
        latest: null,
        recent_window: [],
      },
    ],
    latest_run_cost: null,
  }
}

describe('模型检测展示工具', () => {
  test('完整区分检测结论、执行状态和强指纹冲突', () => {
    const outcomes: Array<
      [
        ChannelModelDetectionOutcomeCode | '',
        'success' | 'attention' | 'unhealthy',
      ]
    > = [
      ['juice_pass_fingerprint_strong', 'success'],
      ['juice_pass_fingerprint_unclear', 'success'],
      ['juice_mismatch_fingerprint_strong', 'unhealthy'],
      ['juice_mismatch_fingerprint_unclear', 'unhealthy'],
      ['juice_insufficient_fingerprint_strong', 'attention'],
      ['juice_insufficient_fingerprint_unclear', 'attention'],
      ['possible_non_gpt', 'unhealthy'],
      ['future_detector_outcome', 'attention'],
      ['', 'attention'],
    ]
    for (const [outcomeCode, expected] of outcomes) {
      assert.equal(
        channelModelDetectionResultTone({
          claimedModel: 'gpt-5.6-sol',
          status: 'completed',
          outcomeCode,
        }),
        expected
      )
    }

    const statuses: Array<
      [
        ChannelModelDetectionExecutionStatus,
        'running' | 'failed' | 'inactive',
        string,
      ]
    > = [
      ['pending', 'running', '待执行'],
      ['submitting', 'running', '提交中'],
      ['running', 'running', '检测中'],
      ['failed', 'failed', '执行失败'],
      ['canceled', 'inactive', '已取消'],
      ['skipped', 'inactive', '已跳过'],
    ]
    for (const [status, expectedTone, expectedLabel] of statuses) {
      assert.equal(
        channelModelDetectionResultTone({
          claimedModel: 'gpt-5.6-sol',
          status,
          outcomeCode: '',
        }),
        expectedTone
      )
      assert.equal(
        channelModelDetectionResultLabel({ status, outcomeCode: '' }),
        expectedLabel
      )
    }

    assert.equal(
      channelModelDetectionResultTone({
        claimedModel: 'gpt-5.6-sol',
        status: 'completed',
        outcomeCode: 'juice_pass_fingerprint_strong',
        fingerprintModel: 'gpt-5.6-luna',
      }),
      'unhealthy'
    )
  })

  test('未知成本保持未知语义而不是格式化为免费', () => {
    const lines = channelModelDetectionCostLines(
      createCost({
        unresolved_cost_nano_cny: null,
        unresolved_cost_cny: null,
        unresolved_cost_unknown_count: 1,
        unresolved_request_count: 1,
        status: 'unresolved',
      })
    )

    assert.deepEqual(lines, ['暂无法估算 · 1 次请求'])
    assert.equal(
      lines.some((line) => line.includes('0.000000000')),
      false
    )
  })

  test('准确区分活动状态和终态', () => {
    for (const status of [
      'queued',
      'waiting_detector',
      'submitting',
      'submission_unknown',
      'running',
      'canceling',
    ] as const) {
      assert.equal(isChannelModelDetectionRunActive(status), true)
    }

    for (const status of [
      'completed',
      'partial',
      'failed',
      'external_session_conflict',
      'canceled',
    ] as const) {
      assert.equal(isChannelModelDetectionRunActive(status), false)
    }
  })

  test('搜索、分组和请求模型筛选可组合且不使用申报型号替代请求模型', () => {
    const channels = [createChannel(1), createChannel(2)]
    const filtered = filterChannelModelDetectionChannels(channels, {
      status: 'attention',
      group: 'vip',
      model: 'gpt-5.6-terra',
      search: '备用',
      sort: 'latest_desc',
      onlyConfigured: false,
    })

    assert.deepEqual(
      filtered.map((channel) => channel.id),
      [2]
    )
    const noClaimedModelMatch = filterChannelModelDetectionChannels(channels, {
      status: 'all',
      group: '',
      model: 'gpt-5.6-sol',
      search: '',
      sort: 'latest_desc',
      onlyConfigured: false,
    })
    assert.deepEqual(noClaimedModelMatch, [])
  })

  test('仅查看已配置时要求渠道存在启用的检测目标', () => {
    const config = {
      channel_id: 1,
      schedule_enabled: false,
      revision: 1,
      created_at: 1,
      updated_at: 1,
    }
    const channels = [
      { ...createChannel(1), config },
      {
        ...createChannel(2),
        config: { ...config, channel_id: 2 },
        targets: [],
      },
      { ...createChannel(3), config: null },
    ]
    channels[0].targets[0].enabled = true

    const filters = {
      status: 'all' as const,
      group: '',
      model: '',
      search: '',
      sort: 'ratio_asc' as const,
      onlyConfigured: true,
    }
    assert.deepEqual(
      filterChannelModelDetectionChannels(channels, filters).map(
        (channel) => channel.id
      ),
      [1]
    )
    assert.deepEqual(
      filterChannelModelDetectionChannels(channels, {
        ...filters,
        onlyConfigured: false,
      }).map((channel) => channel.id),
      [1, 2, 3]
    )
  })

  test('成本倍率升降序都将未知倍率置底，并按渠道名称和 ID 稳定排序', () => {
    const channels = [
      { ...createChannel(5), name: '上海渠道', cost_ratio: null },
      { ...createChannel(4), name: '北京渠道', cost_ratio: 2 },
      { ...createChannel(3), name: '上海渠道', cost_ratio: 1 },
      { ...createChannel(2), name: '北京渠道', cost_ratio: 1 },
      { ...createChannel(1), name: '北京渠道', cost_ratio: 1 },
      { ...createChannel(6), name: '未知倍率', cost_ratio: Number.NaN },
    ] as ChannelModelDetectionChannel[]

    assert.deepEqual(
      sortChannelModelDetectionChannels(channels, 'ratio_asc').map(
        (channel) => channel.id
      ),
      [1, 2, 3, 4, 5, 6]
    )
    assert.deepEqual(
      sortChannelModelDetectionChannels(channels, 'ratio_desc').map(
        (channel) => channel.id
      ),
      [4, 1, 2, 3, 5, 6]
    )
  })
})
