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

import { renderToStaticMarkup } from 'react-dom/server'

import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelMonitorSmartScheduleScoreDetails,
  ChannelMonitorTaskAdjustment,
} from '../../types'
import { ChannelMonitorSmartScheduleAdjustmentRow } from '../channel-monitor-smart-schedule-execution-dialog'

function renderAdjustment(
  overrides: Partial<ChannelMonitorTaskAdjustment> = {}
) {
  const adjustment = {
    channel_id: 7,
    channel_name: '缓存主渠道',
    group: 'vip',
    model: 'cache-model',
    action: 'updated',
    old_priority: 90,
    new_priority: 100,
    old_weight: 100,
    new_weight: 900,
    score: 0.98,
    reason: '管理员固定主渠道',
    ...overrides,
  } satisfies ChannelMonitorTaskAdjustment
  return renderToStaticMarkup(
    <ol>
      <ChannelMonitorSmartScheduleAdjustmentRow adjustment={adjustment} />
    </ol>
  )
}

function createScoreDetails(): ChannelMonitorSmartScheduleScoreDetails {
  const component = {
    available: true,
    raw_value: 1,
    normalized_score: 1,
    configured_weight_percent: 100,
    effective_weight_percent: 100,
  }
  return {
    version: 2,
    strategy: 'ratio',
    minimum_samples: 5,
    sample_scope: 'channel_model',
    sample_group_count: 2,
    inputs: {
      cost_ratio: { value: 1, sample_count: 1 },
      first_token_ms: { value: null, sample_count: 0 },
      tps: { value: null, sample_count: 0 },
      stability: { value: null, sample_count: 0 },
    },
    cohort: {
      cost_ratio: { minimum: 1, maximum: 2, available_count: 2 },
      first_token_ms: { minimum: null, maximum: null, available_count: 0 },
      tps: { minimum: null, maximum: null, available_count: 0 },
    },
    components: {
      cost_ratio: component,
      first_token_ms: {
        ...component,
        available: false,
        raw_value: null,
        normalized_score: null,
        configured_weight_percent: 0,
        effective_weight_percent: 0,
      },
      tps: {
        ...component,
        available: false,
        raw_value: null,
        normalized_score: null,
        configured_weight_percent: 0,
        effective_weight_percent: 0,
      },
    },
    business_score: 1,
    stability: {
      enabled: false,
      available: false,
      applied: false,
      raw_score: null,
      configured_weight_percent: 0,
      effective_weight_percent: 0,
      business_contribution: 1,
      contribution: 0,
    },
    final_score: 1,
    decision: {
      apply_mode: 'weight',
      current_primary_channel_id: 8,
      raw_winner_channel_id: 7,
      selected_primary_channel_id: 7,
      selected_primary: true,
      manual_primary_channel_id: 0,
      switch_threshold_percent: 3,
      primary_traffic_percent: 90,
      force_reset: false,
      manual_primary: false,
      selection_reason: '评分第一且超过切换分差',
      adjustment_reason: '提升主渠道权重',
      reason: '评分第一且超过切换分差；提升主渠道权重',
    },
  }
}

describe('smart schedule execution adjustment details', () => {
  test('shows the administrator fixed state and its exact expiry time', () => {
    const fixedUntil = 1_752_800_000
    const markup = renderAdjustment({
      manual_primary: true,
      manual_primary_until: fixedUntil,
      manual_primary_allow_stability_degrade: true,
    })

    assert.ok(markup.includes('管理员固定'))
    assert.ok(markup.includes('固定到期：'))
    assert.ok(markup.includes('允许稳定性降级'))
    assert.ok(markup.includes(formatTimestampToDate(fixedUntil)))
  })

  test('does not label ordinary score adjustments as administrator fixed', () => {
    const markup = renderAdjustment()

    assert.equal(markup.includes('固定到期：'), false)
  })

  test('shows when a fixed primary channel cannot degrade during its fixed time', () => {
    const markup = renderAdjustment({
      manual_primary: true,
      manual_primary_until: 1_752_800_000,
      manual_primary_allow_stability_degrade: false,
    })

    assert.ok(markup.includes('固定期间不降级'))
  })

  test('shows the saved score inputs and adjustment reason from that execution', () => {
    const markup = renderAdjustment({ score_details: createScoreDetails() })

    assert.ok(markup.includes('评分计算'))
    assert.ok(markup.includes('本次执行快照'))
  })
})
