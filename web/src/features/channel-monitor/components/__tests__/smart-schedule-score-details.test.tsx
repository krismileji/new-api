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

import type { ChannelMonitorSmartScheduleScoreDetails } from '../../types'
import { ChannelMonitorSmartScheduleScoreDetails as ScoreDetails } from '../channel-monitor-smart-schedule-score-details'

function createScoreDetails(): ChannelMonitorSmartScheduleScoreDetails {
  return {
    version: 6,
    strategy: 'smart',
    minimum_samples: 5,
    minimum_comparable_channels: 2,
    comparison_state: 'comparable',
    sample_scope: 'channel_model',
    sample_group_count: 3,
    inputs: {
      cost_ratio: { value: 0.84, sample_count: 1 },
      first_token_ms: { value: 500, sample_count: 30 },
      tps: { value: 40, sample_count: 30 },
      stability: { value: 0.96, sample_count: 30 },
    },
    cohort: {
      priority: 100,
      cost_ratio: { minimum: 0.8, maximum: 1.2, available_count: 3 },
      first_token_ms: { minimum: 400, maximum: 900, available_count: 3 },
      tps: { minimum: 20, maximum: 60, available_count: 3 },
    },
    components: {
      cost_ratio: {
        available: true,
        comparison_state: 'comparable' as const,
        raw_value: 0.84,
        normalized_score: 0.9,
        configured_weight_percent: 40,
        effective_weight_percent: 40,
      },
      first_token_ms: {
        available: true,
        comparison_state: 'comparable' as const,
        raw_value: 500,
        normalized_score: 0.8,
        configured_weight_percent: 40,
        effective_weight_percent: 40,
      },
      tps: {
        available: true,
        comparison_state: 'comparable' as const,
        raw_value: 40,
        normalized_score: 0.5,
        configured_weight_percent: 20,
        effective_weight_percent: 20,
      },
    },
    business_score: 0.78,
    stability: {
      enabled: true,
      available: true,
      applied: true,
      raw_score: 0.96,
      configured_weight_percent: 50,
      effective_weight_percent: 50,
      business_contribution: 0.39,
      contribution: 0.48,
    },
    health: {
      state: 'healthy',
      evidence: true,
      pressure: 0,
      error_pressure: 0,
      latency_pressure: 0,
      sample_count: 30,
      window_seconds: 600,
      error_request_percent: 0,
      risk_request_percent: 0,
      first_token_warning_request_percent: 0,
      healthy_request_percent: 100,
    },
    final_score: 0.87,
    decision: {
      apply_mode: 'priority_weight',
      current_primary_channel_id: 8,
      raw_winner_channel_id: 7,
      selected_primary_channel_id: 7,
      actual_primary_channel_id: 7,
      selected_primary: true,
      manual_primary_channel_id: 0,
      base_rank: 1,
      base_priority: 3,
      base_weight: 100,
      applied_priority: 3,
      applied_weight: 90,
      actual_highest_priority: 3,
      actual_top_layer_channel_ids: [7],
      temporary_traffic_kind: 'adaptive_sampling',
      temporary_traffic_target_percent: 2.5,
      switch_threshold_percent: 3,
      primary_traffic_percent: 90,
      force_reset: false,
      manual_primary: false,
      selection_reason: '评分领先且超过主渠道切换分差',
      adjustment_reason: '权重调整为主渠道目标流量',
      reason: '评分领先且超过主渠道切换分差',
    },
  }
}

describe('smart schedule score calculation details', () => {
  test('shows execution inputs, normalization, weights, contributions, and decision', () => {
    const markup = renderToStaticMarkup(
      <ScoreDetails
        details={createScoreDetails()}
        snapshotLabel='本次执行快照'
        defaultOpen
        channelNameById={
          new Map([
            [7, '低延迟主渠道'],
            [8, '原主渠道'],
          ])
        }
      />
    )

    assert.ok(markup.includes('评分计算'))
    assert.ok(markup.includes('本次执行快照'))
    assert.ok(markup.includes('智能综合'))
    assert.ok(markup.includes('渠道 + 模型共享样本'))
    assert.ok(markup.includes('业务样本覆盖 3 个分组'))
    assert.ok(markup.includes('最低 5 个样本'))
    assert.ok(markup.includes('归一化候选层 P100'))
    assert.ok(markup.includes('x0.8400'))
    assert.ok(markup.includes('x1.2000'))
    assert.match(
      markup,
      /class="shrink-0 whitespace-nowrap" data-score-metric-label="cost_ratio"/
    )
    assert.ok(markup.includes('500 ms'))
    assert.ok(markup.includes('30 个样本'))
    assert.ok(markup.includes('80.00 分'))
    assert.ok(markup.includes('配置 40.0%'))
    assert.ok(markup.includes('有效 40.0%'))
    assert.ok(markup.includes('32.00 分'))
    assert.ok(
      markup.includes('(x1.2000 - x0.8400) / (x1.2000 - x0.8000) = 90.00 分')
    )
    assert.ok(markup.includes('业务贡献：90.00 分 x 40.0% = 36.00 分'))
    assert.ok(markup.includes('业务得分'))
    assert.ok(markup.includes('78.00 分'))
    assert.ok(
      markup.includes(
        '成本倍率 36.00 分 + 首字 32.00 分 + TPS 10.00 分 = 78.00 分'
      )
    )
    assert.ok(markup.includes('稳定性贡献 48.00 分'))
    assert.ok(markup.includes('30 个样本 · 配置权重 50.0%'))
    assert.ok(markup.includes('窗口内错误请求'))
    assert.ok(markup.includes('窗口内首字告警请求'))
    assert.ok(markup.includes('错误率和首字告警请求占比按'))
    assert.ok(markup.includes('最终得分'))
    assert.ok(markup.includes('87.00 分'))
    assert.ok(
      markup.includes(
        '业务得分 78.00 分 x 50.0% + 稳定性 96.00 分 x 50.0% = 87.00 分'
      )
    )
    assert.ok(markup.includes('原主渠道'))
    assert.ok(markup.includes('原主渠道（ID 8）'))
    assert.ok(markup.includes('评分第一'))
    assert.ok(markup.includes('低延迟主渠道（ID 7）'))
    assert.ok(markup.includes('实际主渠道'))
    assert.ok(markup.includes('基础排名'))
    assert.ok(markup.includes('当前应用'))
    assert.ok(markup.includes('实际最高层'))
    assert.ok(markup.includes('自适应备援采样'))
    assert.ok(markup.includes('2.5%'))
    assert.ok(markup.includes('切换分差'))
    assert.ok(markup.includes('3.0%'))
    assert.ok(markup.includes('评分领先且超过主渠道切换分差'))
    assert.ok(markup.includes('权重调整为主渠道目标流量'))
  })

  test('explains when a metric is excluded because samples are insufficient', () => {
    const details = createScoreDetails()
    details.inputs.first_token_ms.sample_count = 2
    details.components.first_token_ms.available = false
    details.components.first_token_ms.normalized_score = null
    details.components.first_token_ms.effective_weight_percent = 0

    const markup = renderToStaticMarkup(
      <ScoreDetails details={details} defaultOpen />
    )

    assert.ok(markup.includes('未计入'))
    assert.ok(markup.includes('未计入：样本 2/5'))
  })

  test('shows an explicit incomparable state below the channel threshold', () => {
    const details = createScoreDetails()
    details.comparison_state = 'insufficient'
    details.business_score = null
    details.final_score = null
    details.cohort.first_token_ms.available_count = 1
    details.components.first_token_ms.available = false
    details.components.first_token_ms.comparison_state = 'insufficient'
    details.components.first_token_ms.normalized_score = null
    details.components.first_token_ms.effective_weight_percent = 0

    const markup = renderToStaticMarkup(
      <ScoreDetails details={details} defaultOpen />
    )

    assert.ok(markup.includes('评分状态：不可比较'))
    assert.ok(markup.includes('不可比较'))
    assert.ok(markup.includes('可比渠道不足（1/2），暂不比较'))
  })

  test('shows the break-even economic snapshot', () => {
    const details = createScoreDetails()
    details.economics = {
      cost_ratio: 1,
      group_ratio: 1,
      gross_margin: 0,
      role: 'break_even_fallback',
    }

    const markup = renderToStaticMarkup(
      <ScoreDetails details={details} defaultOpen />
    )

    assert.ok(markup.includes('经济身份'))
    assert.ok(markup.includes('保本兜底'))
    assert.ok(markup.includes('1.000000'))
    assert.ok(markup.includes('0.000000'))
    assert.ok(markup.includes('管理员手动固定时仍按固定结果执行'))

    const withoutEconomics = createScoreDetails()
    assert.equal(
      renderToStaticMarkup(
        <ScoreDetails details={withoutEconomics} defaultOpen />
      ).includes('经济身份'),
      false
    )
  })

  test('renders skipped decisions when the backend has no actual top layer', () => {
    const details = createScoreDetails()
    details.decision.actual_highest_priority = 0
    details.decision.actual_top_layer_channel_ids = null

    const markup = renderToStaticMarkup(
      <ScoreDetails details={details} defaultOpen />
    )

    assert.ok(markup.includes('实际最高层'))
    assert.ok(markup.includes('渠道 -'))
  })

  test('renders nothing when no saved score snapshot exists', () => {
    assert.equal(renderToStaticMarkup(<ScoreDetails details={null} />), '')
  })
})
