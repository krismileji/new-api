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

import { render, screen, within } from '@testing-library/react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'

import { GroupMonitorBucketDetails, GroupMonitorContent } from '../index'
import type { PricingGroupMonitor, PricingGroupMonitorItem } from '../types'

function categoryMonitorResult(
  groups: Array<Pick<PricingGroupMonitorItem, 'group' | 'category'>>
): PricingGroupMonitor {
  return {
    enabled: true,
    server_now: 1_752_777_900,
    data_cutoff_at: 1_752_777_840,
    display_value: 60,
    display_unit: 'minute',
    items: groups.map((group) => ({
      ...group,
      initial: 'G',
      status: 'healthy',
      latest_first_token_ms: 215,
      success_rate: 100,
      last_finished_at: 1_752_777_840,
      recent_window: [],
    })),
  }
}

test('groups interleaved categories in first appearance order and keeps each category’s group order', () => {
  render(
    <GroupMonitorContent
      result={categoryMonitorResult([
        { group: 'coding-vip', category: '编程模型' },
        { group: 'general', category: '通用模型' },
        { group: 'coding-basic', category: '编程模型' },
      ])}
    />
  )

  expect(
    screen
      .getAllByRole('region')
      .map((region) => region.getAttribute('aria-label'))
  ).toEqual(['编程模型', '通用模型'])
  const coding = within(screen.getByRole('region', { name: '编程模型' }))
  expect(
    coding
      .getAllByRole('heading', { level: 3 })
      .map((heading) => heading.textContent)
  ).toEqual(['coding-vip', 'coding-basic'])
  expect(coding.getByText('2 个分组')).toBeVisible()
  expect(
    within(screen.getByRole('region', { name: '通用模型' })).getByRole(
      'heading',
      { name: 'general' }
    )
  ).toBeVisible()
})

test('legacy, blank and explicitly uncategorized groups share one uncategorized section', () => {
  render(
    <GroupMonitorContent
      result={categoryMonitorResult([
        { group: 'legacy' },
        { group: 'blank', category: '  ' },
        { group: 'explicit', category: '未分类' },
      ])}
    />
  )

  expect(screen.getAllByRole('region')).toHaveLength(1)
  const uncategorized = within(screen.getByRole('region', { name: '未分类' }))
  expect(uncategorized.getByText('3 个分组')).toBeVisible()
  expect(uncategorized.getAllByRole('article')).toHaveLength(3)
})

test('updated categories move cards immediately and long names can wrap', () => {
  const longCategory = 'a'.repeat(64)
  const view = render(
    <GroupMonitorContent
      result={categoryMonitorResult([{ group: 'default', category: '旧分类' }])}
    />
  )
  view.rerender(
    <GroupMonitorContent
      result={categoryMonitorResult([
        { group: 'default', category: longCategory },
      ])}
    />
  )

  expect(
    screen.queryByRole('region', { name: '旧分类' })
  ).not.toBeInTheDocument()
  const category = within(screen.getByRole('region', { name: longCategory }))
  expect(category.getByRole('heading', { name: longCategory })).toHaveClass(
    'break-words'
  )
  expect(category.getByRole('heading', { name: 'default' })).toBeVisible()
  expect(category.getByText('100.0%')).toBeVisible()
})

test('an empty monitor result shows the empty state without category sections', () => {
  render(<GroupMonitorContent result={categoryMonitorResult([])} />)

  expect(screen.getByText('暂无分组监控')).toBeVisible()
  expect(screen.queryByRole('region')).not.toBeInTheDocument()
})

test('按保存的分类顺序展示分组并保留空分类', () => {
  const result = {
    ...categoryMonitorResult([
      { group: 'default', category: '88' },
      { group: 'cache-demo', category: '77' },
    ]),
    categories: ['77', '空分类', '88'],
  }
  render(<GroupMonitorContent result={result} />)

  expect(
    screen
      .getAllByRole('region')
      .map((region) => region.getAttribute('aria-label'))
  ).toEqual(['77', '空分类', '88'])
  const emptyCategory = within(screen.getByRole('region', { name: '空分类' }))
  expect(emptyCategory.getByText('0 个分组')).toBeVisible()
  expect(emptyCategory.getByText('此分类暂无监控分组')).toBeVisible()
})

test('只保存分类还未添加分组时仍展示分类', () => {
  const result = { ...categoryMonitorResult([]), categories: ['空分类'] }
  render(<GroupMonitorContent result={result} />)

  expect(screen.getByRole('region', { name: '空分类' })).toBeVisible()
  expect(screen.getByText('此分类暂无监控分组')).toBeVisible()
  expect(screen.queryByText('暂无分组监控')).not.toBeInTheDocument()
})

describe('group monitor content', () => {
  test('hover details show only first token, TPS, and response time', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 1,
          upstream_failure: 1,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 0,
          first_token_total_ms: 220,
          first_token_sample_count: 1,
          tps_total: 38.5,
          tps_sample_count: 1,
          response_time_total_ms: 1_480,
          response_time_sample_count: 1,
          result: 'success',
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('首字'))
    assert.ok(markup.includes('0.22 秒'))
    assert.ok(markup.includes('TPS'))
    assert.ok(markup.includes('38.5'))
    assert.ok(markup.includes('耗时'))
    assert.ok(markup.includes('1.48 秒'))
    assert.ok(!markup.includes('毫秒'))
    assert.ok(!markup.includes('上游失败'))
    assert.ok(!markup.includes('成功率'))
  })

  test('keeps visible configured groups when monitoring is paused', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorContent
        result={{
          enabled: false,
          server_now: 1_752_777_900,
          data_cutoff_at: 1_752_777_840,
          display_value: 60,
          display_unit: 'minute',
          items: [
            {
              group: 'default',
              initial: 'D',
              status: 'paused',
              probe_model: 'gpt-4.1-mini',
              latest_first_token_ms: 215,
              success_rate: 100,
              last_finished_at: 1_752_777_840,
              recent_window: [
                {
                  started_at: 1_752_777_840,
                  success: 1,
                  upstream_failure: 0,
                  rate_limited: 0,
                  local_failure: 0,
                  unavailable: 0,
                  skipped: 0,
                  timeout: 0,
                  result: 'success',
                },
              ],
            },
          ],
        }}
      />
    )

    assert.ok(markup.includes('default'))
    assert.ok(markup.includes('gpt-4.1-mini'))
    assert.ok(markup.includes('已停用'))
    assert.ok(!markup.includes('分组监控暂未启用'))
    assert.match(markup, /data-slot="group-monitor-bucket"/)
    assert.match(markup, /data-group-monitor-window-value="60"/)
  })

  test('keeps precise group ratios visible in the monitor card', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorContent
        result={{
          enabled: true,
          server_now: 1_752_777_900,
          data_cutoff_at: 1_752_777_840,
          display_value: 60,
          display_unit: 'minute',
          items: [
            {
              group: 'precision',
              initial: 'P',
              status: 'healthy',
              group_ratio: 0.00123456789,
              latest_first_token_ms: null,
              success_rate: null,
              last_finished_at: 0,
              recent_window: [],
            },
          ],
        }}
      />
    )

    assert.ok(markup.includes('0.00123456789x'))
  })

  test('renders a timed out probe as a yellow warning', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 0,
          upstream_failure: 0,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 1,
          result: 'timeout',
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('超时'))
    assert.match(markup, /bg-warning/)
  })

  test('uses the latest execution instead of the bucket aggregate for hover details', () => {
    const markup = renderToStaticMarkup(
      <GroupMonitorBucketDetails
        bucket={{
          started_at: 1_752_777_840,
          success: 1,
          upstream_failure: 0,
          rate_limited: 0,
          local_failure: 0,
          unavailable: 0,
          skipped: 0,
          timeout: 1,
          first_token_total_ms: 220,
          first_token_sample_count: 1,
          tps_total: 38.5,
          tps_sample_count: 1,
          response_time_total_ms: 31_040,
          response_time_sample_count: 1,
          result: 'timeout',
          latest_result: 'success',
          latest_first_token_ms: 220,
          latest_tps: 38.5,
          latest_response_time_ms: 1_480,
        }}
        displayUnit='minute'
        enabled
      />
    )

    assert.ok(markup.includes('成功'))
    assert.ok(markup.includes('0.22 秒'))
    assert.ok(markup.includes('1.48 秒'))
    assert.ok(!markup.includes('超时'))
  })
})
