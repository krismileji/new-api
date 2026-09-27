import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'

import { ChannelMonitorProfitValue } from '../channel-monitor-profit'
import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

afterEach(cleanup)

test('unconfirmed profit stays visibly marked and missing data is not zero', () => {
  const view = render(
    <ChannelMonitorProfitValue
      summary={{
        ...analyticsMetrics,
        profit_nano_cny: -1_000_000_000,
        profit_confirmed: false,
      }}
    />
  )
  expect(screen.getByText('利润待确认')).toBeInTheDocument()
  expect(view.container.textContent).toContain('-')
  view.rerender(<ChannelMonitorProfitValue />)
  expect(view.container.textContent).toBe('-')
  expect(screen.queryByText('利润待确认')).not.toBeInTheDocument()
})

test('loss filter changes only the list and retains full scope summary and trend', async () => {
  const summary = {
    ...analyticsMetrics,
    income_nano_cny: 3_000_000_000,
    wallet_income_nano_cny: 2_000_000_000,
    subscription_income_nano_cny: 1_000_000_000,
    cost_nano_cny: 2_000_000_000,
    profit_nano_cny: 1_000_000_000,
    profit_rate: 1 / 3,
    profit_confirmed: true,
  }
  const view = renderAnalyticsQuery('profit', (params) => {
    const items = []
    if (params.group_by === 'channel') {
      items.push(
        analyticsItem('7', {
          ...summary,
          channel_id: 7,
          profit_nano_cny: -1_000_000_000,
        })
      )
      if (!params.only_loss) {
        items.push(analyticsItem('8', { ...summary, channel_id: 8 }))
      }
    }
    return analyticsResponse(params, items, { scope_summary: summary, summary })
  })
  await screen.findByRole('button', { name: '查看渠道 A明细' })
  for (const label of ['用户扣费', '总成本', '利润率', '利润']) {
    expect(screen.getAllByText(label, { exact: true }).length).toBeGreaterThan(0)
  }
  await screen.findByText('所选日期暂无利润记录')
  const filter = screen.getByRole('button', { name: '仅展示亏损行' })
  expect(filter).toHaveAttribute('aria-pressed', 'false')
  fireEvent.click(filter)
  await waitFor(() =>
    expect(
      view.requests.some(
        (request) => request.group_by === 'channel' && request.only_loss
      )
    ).toBe(true)
  )
  expect(filter).toHaveAttribute('aria-pressed', 'true')
  expect(
    view.requests
      .filter((request) => request.group_by === 'day')
      .every((request) => !request.only_loss)
  ).toBe(true)
  expect(screen.getByText(/钱包扣费.*订阅消耗/)).toHaveTextContent('2.0000')
  expect(screen.getByText(/钱包扣费.*订阅消耗/)).toHaveTextContent('1.0000')
})
