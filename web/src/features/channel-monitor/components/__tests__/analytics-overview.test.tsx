import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
  type AnalyticsParams,
} from './analytics-query.fixture'

function overviewResponse(params: AnalyticsParams) {
  const summary = {
    ...analyticsMetrics,
    income_nano_cny: 12e9,
    cost_nano_cny: 3e9,
    profit_nano_cny: 9e9,
    profit_rate: 0.75,
    profit_confirmed: true,
    unresolved_count: 0,
  }
  let items = [analyticsItem('7', { ...summary, channel_id: 7 })]
  if (params.group_by === 'day') items = []
  if (params.group_by === 'user') {
    items = [
      analyticsItem('31', { ...summary, user_id: 31, user_name: 'alice' }),
    ]
  }
  if (params.group_by === 'api_key') {
    items = [
      analyticsItem('201', {
        ...summary,
        api_key_id: 201,
        api_key_name: '生产 Key',
      }),
    ]
  }
  return analyticsResponse(params, items, { summary, scope_summary: summary })
}

test('the overview shows cost, profit, success and cache metrics in the same row of one table', async () => {
  renderAnalyticsQuery('profit', overviewResponse, undefined, {
    showRequestOverview: true,
  })

  const dialog = screen.getByRole('dialog', { name: '成本、利润与请求概览' })
  const channel = await screen.findAllByRole('button', {
    name: '查看渠道 A明细',
  })
  expect(channel).toHaveLength(1)
  expect(screen.getAllByRole('table')).toHaveLength(1)
  const row = within(screen.getByRole('row', { name: /渠道 A/ }))
  expect(row.getByText('¥3.0000')).toBeVisible()
  expect(row.getByText('¥9.0000')).toBeVisible()
  expect(row.getByText('90.0%')).toBeVisible()
  expect(row.getByText('40.0%')).toBeVisible()
  expect(row.getAllByRole('cell')).toHaveLength(9)
  expect(dialog).toHaveClass('sm:max-w-[96rem]', 'overflow-y-auto')
  expect(screen.getByRole('table').parentElement).toHaveClass('overflow-x-auto')
  expect(screen.getAllByRole('button', { name: '下一页' })).toHaveLength(1)
  expect(screen.getAllByLabelText('搜索分析明细')).toHaveLength(1)
})

test('the unified table preserves date and search when expanding a user to API keys', async () => {
  const { requests } = renderAnalyticsQuery(
    'profit',
    overviewResponse,
    undefined,
    { showRequestOverview: true }
  )
  await screen.findByRole('table')
  fireEvent.click(screen.getByRole('button', { name: '统计日期范围' }))
  fireEvent.change(screen.getByLabelText('开始日期'), {
    target: { value: '2026-09-01' },
  })
  fireEvent.change(screen.getByLabelText('结束日期'), {
    target: { value: '2026-09-03' },
  })
  fireEvent.click(screen.getByRole('button', { name: '应用' }))
  fireEvent.click(screen.getByRole('button', { name: 'API Key 明细' }))
  fireEvent.change(screen.getByLabelText('搜索分析明细'), {
    target: { value: 'alice' },
  })
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        metric: 'overview',
        group_by: 'user',
        from: '2026-09-01',
        to: '2026-09-04',
        search: 'alice',
      })
    )
  )
  fireEvent.click(await screen.findByRole('button', { name: '查看alice明细' }))
  await screen.findByRole('button', { name: '查看生产 Key明细' })
  const row = within(screen.getByRole('row', { name: /生产 Key/ }))
  expect(row.getByText('¥3.0000')).toBeVisible()
  expect(row.getByText('90.0%')).toBeVisible()
  expect(row.getByText('40.0%')).toBeVisible()
  expect(screen.getAllByRole('table')).toHaveLength(1)
  expect(requests).toContainEqual(
    expect.objectContaining({
      metric: 'overview',
      group_by: 'api_key',
      user_id: 31,
      from: '2026-09-01',
      to: '2026-09-04',
      search: 'alice',
    })
  )
})

test('sorting request metrics and changing pages applies to the whole overview', async () => {
  const { requests } = renderAnalyticsQuery(
    'profit',
    (params) => ({ ...overviewResponse(params), total: 21 }),
    undefined,
    { showRequestOverview: true }
  )
  fireEvent.click(
    await screen.findByRole('button', { name: /按上游成功率排序/ })
  )
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        metric: 'overview',
        sort: 'success_rate',
        page: 1,
      })
    )
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: '下一页' })).toBeEnabled()
  )
  fireEvent.click(screen.getByRole('button', { name: '下一页' }))
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        metric: 'overview',
        sort: 'success_rate',
        page: 2,
      })
    )
  )
  expect(screen.getAllByRole('table')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: '仅看已确认亏损' }))
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        metric: 'overview',
        sort: 'success_rate',
        page: 1,
        only_loss: true,
      })
    )
  )
})

test('a failed overview retries as one table with all metrics', async () => {
  let unavailable = true
  renderAnalyticsQuery(
    'profit',
    (params) => {
      if (params.group_by !== 'day' && unavailable) {
        throw new Error('统计暂不可用')
      }
      return overviewResponse(params)
    },
    undefined,
    { showRequestOverview: true }
  )
  await screen.findByText('统计加载失败')
  unavailable = false
  fireEvent.click(screen.getByRole('button', { name: '重试' }))
  await screen.findByRole('button', { name: '查看渠道 A明细' })
  expect(
    within(screen.getByRole('row', { name: /渠道 A/ })).getByText('40.0%')
  ).toBeVisible()
  expect(screen.getAllByRole('table')).toHaveLength(1)
})

test('missing request coverage leaves request cells unknown while keeping financial values in the same row', async () => {
  renderAnalyticsQuery(
    'profit',
    (params) => {
      const response = overviewResponse(params)
      return {
        ...response,
        request_coverage: {
          ...response.coverage,
          status: 'partial',
          reasons: ['daily_checkpoint_missing'],
        },
        items: response.items.map((item) => ({
          ...item,
          request_metrics_available: false,
        })),
      }
    },
    undefined,
    { showRequestOverview: true }
  )
  await screen.findByRole('button', { name: '查看渠道 A明细' })
  const row = within(screen.getByRole('row', { name: /渠道 A/ }))
  expect(row.getByText('¥3.0000')).toBeVisible()
  expect(row.queryByText('90.0%')).not.toBeInTheDocument()
  expect(row.getAllByTitle('请求统计尚未完整，不能确认此行数据')).toHaveLength(
    4
  )
  expect(screen.getByText('请求统计覆盖不完整')).toBeVisible()
})
