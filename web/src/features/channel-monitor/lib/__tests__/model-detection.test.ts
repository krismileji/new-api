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
} from '../../types-model-detection'
import {
  channelModelDetectionCostLines,
  channelModelDetectionPollInterval,
  channelModelDetectionRunPollInterval,
  filterChannelModelDetectionChannels,
  isChannelModelDetectionRunActive,
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
      },
    ],
    latest_run_cost: null,
  }
}

describe('模型检测展示工具', () => {
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

  test('页面不可见时停止轮询且活动任务使用三秒周期', () => {
    assert.equal(channelModelDetectionPollInterval(false, false), false)
    assert.equal(channelModelDetectionPollInterval(true, false), false)
    assert.equal(channelModelDetectionPollInterval(true, true), 3000)
    assert.equal(channelModelDetectionPollInterval(false, true), 20_000)
  })

  test('运行详情只在活动状态和页面可见时轮询', () => {
    for (const status of [
      'queued',
      'waiting_detector',
      'submitting',
      'submission_unknown',
      'running',
      'canceling',
    ] as const) {
      assert.equal(isChannelModelDetectionRunActive(status), true)
      assert.equal(channelModelDetectionRunPollInterval(status, true), 3000)
      assert.equal(channelModelDetectionRunPollInterval(status, false), false)
    }

    for (const status of [
      'completed',
      'partial',
      'failed',
      'external_session_conflict',
      'canceled',
    ] as const) {
      assert.equal(isChannelModelDetectionRunActive(status), false)
      assert.equal(channelModelDetectionRunPollInterval(status, true), false)
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
    })
    assert.deepEqual(noClaimedModelMatch, [])
  })
})
