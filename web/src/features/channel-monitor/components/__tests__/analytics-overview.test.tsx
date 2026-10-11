import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { expect, test } from 'vitest'

import type { ChannelMonitorAnalyticsResponse } from '../../types-analytics'
import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
  type AnalyticsParams,
} from './analytics-query.fixture'

function overviewResponse(params: AnalyticsParams) {
  const summary =
    params.metric === 'profit'
      ? {
          ...analyticsMetrics,
          income_nano_cny: 12e9,
          cost_nano_cny: 3e9,
          profit_nano_cny: 9e9,
          profit_rate: 0.75,
          profit_confirmed: true,
          unresolved_count: 0,
        }
      : analyticsMetrics
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

test('the overview displays financial and request metrics together in a wide responsive dialog', async () => {
  renderAnalyticsQuery('profit', overviewResponse, undefined, {
    showRequestOverview: true,
  })

  const dialog = screen.getByRole('dialog', { name: '成本、利润与请求概览' })
  const profit = within(screen.getByRole('region', { name: '渠道利润分析' }))
  const successRegion = screen.getByRole('region', { name: '成功率与缓存分析' })
  const success = within(successRegion)
  await profit.findByRole('button', { name: '查看渠道 A明细' })
  await success.findByRole('button', { name: '查看渠道 A明细' })

  expect(dialog).toHaveClass('sm:max-w-[96rem]', 'overflow-y-auto')
  expect(successRegion.parentElement).toHaveClass(
    'grid-cols-1',
    'xl:grid-cols-2'
  )
  expect(profit.getAllByText('¥3.0000').length).toBeGreaterThan(0)
  expect(profit.getAllByText('¥9.0000').length).toBeGreaterThan(0)
  expect(success.getAllByText('90.0%').length).toBeGreaterThan(0)
  expect(success.getAllByText('40.0%').length).toBeGreaterThan(0)
  expect(
    success.getByRole('columnheader', { name: /缓存写入次数/ })
  ).toBeVisible()
  expect(screen.getAllByLabelText('搜索分析明细')).toHaveLength(1)
  expect(screen.getAllByRole('button', { name: '统计日期范围' })).toHaveLength(
    1
  )
  expect(screen.getAllByRole('dialog')).toHaveLength(1)
})

test('changing the shared date, dimension and search applies to both panels and their drilldowns', async () => {
  const { requests } = renderAnalyticsQuery(
    'profit',
    overviewResponse,
    undefined,
    { showRequestOverview: true }
  )
  const profit = within(screen.getByRole('region', { name: '渠道利润分析' }))
  const success = within(
    screen.getByRole('region', { name: '成功率与缓存分析' })
  )
  await success.findByRole('button', { name: '查看渠道 A明细' })

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

  await waitFor(() => {
    for (const metric of ['profit', 'success']) {
      expect(requests).toContainEqual(
        expect.objectContaining({
          metric,
          group_by: 'user',
          from: '2026-09-01',
          to: '2026-09-04',
          search: 'alice',
        })
      )
    }
  })
  fireEvent.click(await profit.findByRole('button', { name: '查看alice明细' }))
  fireEvent.click(await success.findByRole('button', { name: '查看alice明细' }))
  await profit.findByRole('button', { name: '查看生产 Key明细' })
  await success.findByRole('button', { name: '查看生产 Key明细' })
  for (const metric of ['profit', 'success']) {
    expect(requests).toContainEqual(
      expect.objectContaining({
        metric,
        group_by: 'api_key',
        user_id: 31,
        from: '2026-09-01',
        to: '2026-09-04',
        search: 'alice',
      })
    )
  }
})

test('a failed request query can retry while the financial results remain visible', async () => {
  let unavailable = true
  renderAnalyticsQuery(
    'profit',
    (params) => {
      if (params.metric === 'success' && unavailable) {
        throw new Error('请求统计暂不可用')
      }
      return overviewResponse(params)
    },
    undefined,
    { showRequestOverview: true }
  )
  const profit = within(screen.getByRole('region', { name: '渠道利润分析' }))
  const success = within(
    screen.getByRole('region', { name: '成功率与缓存分析' })
  )

  await success.findByText('统计加载失败')
  expect(
    await profit.findByRole('button', { name: '查看渠道 A明细' })
  ).toBeVisible()
  expect(profit.getAllByText('¥3.0000').length).toBeGreaterThan(0)
  unavailable = false
  fireEvent.click(success.getByRole('button', { name: '重试' }))
  expect(
    await success.findByRole('button', { name: '查看渠道 A明细' })
  ).toBeVisible()
  expect(success.queryByText('统计加载失败')).not.toBeInTheDocument()
})

test('financial results are available while request statistics are still loading', async () => {
  let release: (result: ChannelMonitorAnalyticsResponse) => void = () =>
    undefined
  const pending = new Promise<ChannelMonitorAnalyticsResponse>((resolve) => {
    release = resolve
  })
  renderAnalyticsQuery(
    'profit',
    (params) =>
      params.metric === 'success' ? pending : overviewResponse(params),
    undefined,
    { showRequestOverview: true }
  )
  const profit = within(screen.getByRole('region', { name: '渠道利润分析' }))
  const success = within(
    screen.getByRole('region', { name: '成功率与缓存分析' })
  )

  expect(
    await profit.findByRole('button', { name: '查看渠道 A明细' })
  ).toBeVisible()
  expect(success.queryByRole('table')).not.toBeInTheDocument()
  await act(async () => {
    release(overviewResponse({ metric: 'success', group_by: 'channel' }))
  })
  expect(
    await success.findByRole('button', { name: '查看渠道 A明细' })
  ).toBeVisible()
})
