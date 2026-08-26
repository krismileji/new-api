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

import { describe, test } from 'vitest'

import type { ChannelStatusProbeChannel } from '../../types'
import {
  isChannelStatusProbeActive,
  matchesChannelStatusProbeGroup,
  matchesChannelStatusProbeSearch,
} from '../status-probe'

function createChannel(
  id: number,
  name: string,
  costRatio: number | null
): ChannelStatusProbeChannel {
  return {
    id,
    name,
    type: 1,
    channel_status: 1,
    remark: '',
    groups: ['default'],
    cost_ratio: costRatio,
    supported_models: ['model-a'],
    allows_custom_model: false,
    config: null,
    health_status: 'unconfigured',
    running: false,
    latest: null,
    avg_first_token_ms: null,
    avg_tps: null,
    model_statuses: [],
    configured_model_count: 0,
  }
}

function withProbeData(
  channel: ChannelStatusProbeChannel,
  intervalSeconds: number,
  nextRunAt: number,
  finishedAt: number,
  consecutiveFailures: number
): ChannelStatusProbeChannel {
  return {
    ...channel,
    config: {
      id: channel.id,
      channel_id: channel.id,
      enabled: true,
      models: ['model-a'],
      interval_seconds: intervalSeconds,
      display_value: 60,
      display_unit: 'minute',
      record_sample: false,
      next_run_at: nextRunAt,
      manual_request_id: '',
      manual_requested_at: 0,
      revision: 1,
      running_trigger: '',
      running_run_id: '',
      running_started_at: 0,
      created_at: 1,
      updated_at: 1,
    },
    latest: {
      id: channel.id,
      channel_id: channel.id,
      model_name: 'model-a',
      execution_id: channel.id,
      run_id: `run-${channel.id}`,
      started_at: finishedAt - 1,
      finished_at: finishedAt,
      result: consecutiveFailures > 0 ? 'upstream_failure' : 'success',
      success: consecutiveFailures === 0,
      request_dispatched: true,
      response_time_ms: 100,
      first_token_ms: 50,
      tps: 20,
      error_code: '',
      error_message: '',
      consecutive_successes: consecutiveFailures > 0 ? 0 : 1,
      consecutive_failures: consecutiveFailures,
      last_health_result:
        consecutiveFailures > 0 ? 'upstream_failure' : 'success',
      last_health_execution_id: channel.id,
      last_health_finished_at: finishedAt,
      sample_status: 'skipped',
      sample_message: '',
      trigger: 'scheduled',
      endpoint: '/v1/chat/completions',
      stream: true,
      created_at: finishedAt,
      updated_at: finishedAt,
    },
    avg_first_token_ms: 50,
    avg_tps: 20,
  }
}

describe('状态探测活动状态', () => {
  test('执行中和手动排队中的渠道需要继续刷新', () => {
    const running = createChannel(1, '执行中', 1)
    running.running = true
    const queued = withProbeData(createChannel(2, '排队中', 1), 60, 0, 0, 0)
    assert.ok(queued.config)
    queued.config.manual_request_id = 'manual-2'

    assert.equal(isChannelStatusProbeActive(running), true)
    assert.equal(isChannelStatusProbeActive(queued), true)
  })

  test('空闲渠道不需要继续刷新', () => {
    const idle = withProbeData(createChannel(1, '空闲', 1), 60, 0, 0, 0)

    assert.equal(isChannelStatusProbeActive(idle), false)
  })
})

describe('状态探测搜索', () => {
  test('搜索内容匹配渠道名称、备注和 ID', () => {
    const channel = createChannel(17, '华东渠道', 1)
    channel.remark = '低延迟主线路'

    assert.equal(matchesChannelStatusProbeSearch(channel, '华东'), true)
    assert.equal(matchesChannelStatusProbeSearch(channel, '低延迟'), true)
    assert.equal(matchesChannelStatusProbeSearch(channel, '17'), true)
    assert.equal(matchesChannelStatusProbeSearch(channel, '备用线路'), false)
  })

  test('分组使用独立筛选且不混入普通搜索', () => {
    const channel = createChannel(1, '渠道 A', 1)
    channel.groups = ['default', '华东专线']
    channel.supported_models = ['gpt-4.1']

    assert.equal(matchesChannelStatusProbeGroup(channel, '华东专线'), true)
    assert.equal(matchesChannelStatusProbeGroup(channel, '华南专线'), false)
    assert.equal(matchesChannelStatusProbeSearch(channel, '华东专线'), false)
    assert.equal(matchesChannelStatusProbeSearch(channel, 'gpt-4.1'), false)
  })
})
