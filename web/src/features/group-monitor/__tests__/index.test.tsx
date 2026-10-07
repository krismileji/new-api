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

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test, vi } from 'vitest'

import * as groupMonitorApi from '../api'
import { useGroupMonitor } from '../hooks/use-group-monitor'
import { GroupMonitorBucketDetails, GroupMonitorContent } from '../index'
import type { PricingGroupMonitor, PricingGroupMonitorItem } from '../types'

test('快照刷新失败后隐藏已缓存的正常状态', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['pricing', 'group-monitor'], {
    success: true,
    data: categoryMonitorResult([{ group: 'vip' }]),
  })
  const request = vi
    .spyOn(groupMonitorApi, 'getPricingGroupMonitor')
    .mockRejectedValue(new Error('分组监控汇总暂不可用'))
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  try {
    const { result, unmount } = renderHook(() => useGroupMonitor(), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.data).toBeUndefined()
    unmount()
  } finally {
    request.mockRestore()
    client.clear()
  }
})

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

test('在分组名称下展示完整说明，保留换行并允许长文本折行', () => {
  const result = categoryMonitorResult([{ group: 'vip' }])
  const description =
    '高可用线路，适合长上下文请求\nhttps://example.com/groups/long-description-without-spaces'
  result.items[0].description = description
  render(<GroupMonitorContent result={result} />)

  const group = within(screen.getByRole('article', { name: 'vip' }))
  const text = group.getByText(description.replace('\n', ' '))
  expect(text).toBeVisible()
  expect(text.textContent).toBe(description)
  expect(text).toHaveClass('whitespace-pre-wrap', 'wrap-anywhere')
})

test.each([undefined, '', ' \n '])(
  '说明缺省或空白时不显示说明段落（%s）',
  (description) => {
    const result = categoryMonitorResult([{ group: 'vip' }])
    result.items[0].description = '已有说明'
    const view = render(<GroupMonitorContent result={result} />)
    expect(screen.getByText('已有说明')).toBeVisible()

    result.items[0].description = description
    view.rerender(<GroupMonitorContent result={result} />)
    expect(screen.queryByText('已有说明')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'vip' })).toBeVisible()
  }
)

test.each([false, undefined])(
  '关闭或缺省显示开关时隐藏缓存率，即使响应仍含缓存数据（%s）',
  (showCacheRate) => {
    const result = categoryMonitorResult([{ group: 'vip' }])
    result.show_cache_rate = showCacheRate
    result.items[0].cache_rate = 75
    result.items[0].cache_rate_max = 90
    result.items[0].cache_rate_average = 60
    render(<GroupMonitorContent result={result} />)

    expect(screen.queryByText('缓存率')).not.toBeInTheDocument()
    expect(screen.queryByText('75.0%')).not.toBeInTheDocument()
    expect(screen.queryByText('最高')).not.toBeInTheDocument()
    expect(screen.queryByText('平均')).not.toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'vip' })).toHaveTextContent(
      '成功率'
    )
  }
)

test('开启缓存率后仅以 API Key 最高值展示缓存率，区分零命中与无有效样本', () => {
  const result = categoryMonitorResult([
    { group: 'hit' },
    { group: 'miss' },
    { group: 'empty' },
  ])
  result.show_cache_rate = true
  result.items[0].cache_rate = 72.5
  result.items[0].cache_rate_max = 90
  result.items[0].cache_rate_average = 60
  result.items[1].cache_rate_max = 0
  result.items[1].cache_rate_average = 0
  result.items[2].cache_rate = 80
  result.items[2].cache_rate_average = 60 // Other aggregates must not stand in for the maximum.
  render(<GroupMonitorContent result={result} />)

  const hit = within(screen.getByRole('article', { name: 'hit' }))
  expect(hit.getByText('缓存率')).toBeInTheDocument()
  expect(hit.getByText('缓存率').parentElement).toHaveTextContent('90.0%')
  expect(hit.getByText('90.0%')).toBeVisible()
  expect(hit.queryByText('最高')).not.toBeInTheDocument()
  expect(hit.queryByText('平均')).not.toBeInTheDocument()
  expect(hit.queryByText('60.0%')).not.toBeInTheDocument()
  expect(hit.queryByText('72.5%')).not.toBeInTheDocument()
  expect(
    within(screen.getByRole('article', { name: 'miss' })).getAllByText('0.0%')
  ).toHaveLength(1)
  expect(
    within(screen.getByRole('article', { name: 'empty' })).getAllByText(
      '暂无数据'
    )
  ).toHaveLength(1)
  expect(hit.getByText('缓存率').closest('dl')).toHaveClass(
    'grid-cols-2',
    'sm:grid-cols-4',
    'lg:col-span-4'
  )
})

test('修改状态展示范围后缓存率说明同步更新', () => {
  const result = categoryMonitorResult([{ group: 'vip' }])
  result.show_cache_rate = true
  const view = render(<GroupMonitorContent result={result} />)

  expect(
    screen.getByTitle(/近 60 分钟内，先按用户 API Key.*最高值作为缓存率/)
  ).toBeVisible()

  view.rerender(
    <GroupMonitorContent
      result={{ ...result, display_value: 3, display_unit: 'hour' }}
    />
  )
  expect(
    screen.getByTitle(/近 3 小时内，先按用户 API Key.*最高值作为缓存率/)
  ).toBeVisible()

  view.rerender(
    <GroupMonitorContent
      result={{ ...result, display_value: 7, display_unit: 'day' }}
    />
  )
  expect(
    screen.getByTitle(/近 7 天内，先按用户 API Key.*最高值作为缓存率/)
  ).toBeVisible()
})

test('设置上下文下限后缓存率说明显示包含边界的流式筛选口径', () => {
  const result = categoryMonitorResult([{ group: 'vip' }])
  result.show_cache_rate = true
  result.cache_min_context_k = 32
  render(<GroupMonitorContent result={result} />)
  expect(
    screen.getByTitle(/仅统计输入上下文 ≥ 32 K tokens 的流式请求/)
  ).toBeVisible()
  expect(screen.getByTitle(/修改监控配置后清空统计并重新累计/)).toBeVisible()
})

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
  const codingList = within(
    coding.getByRole('list', { name: '编程模型分组列表' })
  )
  expect(
    codingList
      .getAllByRole('listitem')
      .map((row) => within(row).getByRole('heading', { level: 3 }).textContent)
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
  expect(
    within(uncategorized.getByRole('list')).getAllByRole('listitem')
  ).toHaveLength(3)
})

test('updated categories move rows immediately and long names can wrap', () => {
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

  test('long group and model names truncate with full titles and missing probe data stays explicit in a list row', () => {
    const groupName = 'precision-group-with-a-long-unbroken-name'
    const probeModel = 'provider/model-with-a-long-unbroken-version-name'
    render(
      <GroupMonitorContent
        result={{
          enabled: true,
          server_now: 1_752_777_900,
          data_cutoff_at: 1_752_777_840,
          display_value: 60,
          display_unit: 'minute',
          items: [
            {
              group: groupName,
              initial: 'P',
              status: 'pending',
              probe_model: probeModel,
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

    const list = within(screen.getByRole('list', { name: '未分类分组列表' }))
    expect(list.getAllByRole('listitem')).toHaveLength(1)
    const row = within(list.getByRole('article', { name: groupName }))
    expect(row.getByRole('heading', { name: groupName })).toHaveAttribute(
      'title',
      groupName
    )
    expect(row.getByTitle(groupName)).toHaveClass('truncate')
    expect(row.getByTitle(probeModel)).toHaveClass('truncate')
    expect(row.getByText('0.00123456789x')).toHaveAttribute(
      'title',
      '0.00123456789x'
    )
    expect(row.getByText('待检测')).toBeVisible()
    expect(row.getAllByText('--')).toHaveLength(2)
    expect(row.getByText('暂无探测记录')).toBeVisible()
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
