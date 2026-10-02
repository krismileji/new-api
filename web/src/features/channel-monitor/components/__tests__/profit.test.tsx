import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  ChannelMonitorProfitOverview,
  ChannelMonitorProfitCell,
  ChannelMonitorProfitValue,
} from '../channel-monitor-profit'
import {
  analyticsItem,
  analyticsMetrics,
  analyticsResponse,
  renderAnalyticsQuery,
} from './analytics-query.fixture'

afterEach(cleanup)
afterEach(() => vi.useRealTimers())

test.each([false, true])(
  'independent channel profit failure is visible and retryable with previous result=%s',
  async (hasPrevious) => {
    const adapter = api.defaults.adapter
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    let failed = !hasPrevious
    const summary = {
      ...analyticsMetrics,
      income_nano_cny: 10e9,
      profit_nano_cny: 7e9,
      profit_confirmed: true,
    }
    api.defaults.adapter = async (config) => {
      if (failed) throw new Error('渠道利润暂不可用')
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: analyticsResponse({ group_by: 'channel' }, [], {
            scope_summary: summary,
          }),
        },
      }
    }
    const view = render(
      <QueryClientProvider client={client}>
        <ChannelMonitorProfitCell
          channelId={201}
          channelName='补查渠道'
          needsQuery
          onOpen={() => undefined}
        />
      </QueryClientProvider>
    )
    try {
      if (hasPrevious) {
        await screen.findByText('¥7.0000')
        failed = true
        await act(async () => {
          await client.refetchQueries({
            queryKey: ['channel-monitor', 'analytics'],
          })
        })
      }
      const status = await screen.findByRole('status')
      expect(status).toHaveTextContent(
        hasPrevious ? '刷新失败，显示上次核对结果' : '利润加载失败'
      )
      expect(view.container).not.toHaveTextContent('¥0.0000')
      failed = false
      fireEvent.click(
        screen.getByRole('button', { name: '重试渠道 补查渠道 的利润查询' })
      )
      await waitFor(() =>
        expect(screen.queryByRole('status')).not.toBeInTheDocument()
      )
      expect(screen.getByText('¥7.0000')).toBeInTheDocument()
    } finally {
      view.unmount()
      client.clear()
      api.defaults.adapter = adapter
    }
  }
)

test('overview shows cost, income and profit from one snapshot and retains it on refresh failure', () => {
  const summary = {
    ...analyticsMetrics,
    income_nano_cny: 10e9,
    cost_nano_cny: 3e9,
    profit_nano_cny: 7e9,
    profit_rate: 0.7,
    profit_confirmed: true,
  }
  const view = render(
    <ChannelMonitorProfitOverview
      summary={summary}
      loading={false}
      failed={false}
    />
  )
  for (const value of ['¥3.0000', '¥7.0000']) {
    expect(screen.getByText(value)).toBeInTheDocument()
  }
  expect(view.container).toHaveTextContent('扣费 ¥10.0000 · 利润率 70.0%')
  view.rerender(
    <ChannelMonitorProfitOverview summary={summary} loading={false} failed />
  )
  expect(screen.getByText('¥3.0000')).toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent(
    '刷新失败，显示上次核对结果'
  )
})

test('overview does not invent zero cost while the shared snapshot is loading or unavailable', () => {
  const view = render(<ChannelMonitorProfitOverview loading failed={false} />)
  expect(screen.getByLabelText('成本与利润加载中')).toBeInTheDocument()
  view.rerender(<ChannelMonitorProfitOverview loading={false} failed />)
  expect(screen.getByRole('status')).toHaveTextContent('成本与利润加载失败')
  expect(view.container).not.toHaveTextContent('¥0.0000')
})

test('unconfirmed profit hides the misleading loss while missing data is not zero', () => {
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
  expect(screen.queryByText(/1\.0000/)).not.toBeInTheDocument()
  expect(view.container.querySelector('.text-destructive')).toBeNull()
  view.rerender(<ChannelMonitorProfitValue />)
  expect(view.container.textContent).toBe('-')
  expect(screen.queryByText('利润待确认')).not.toBeInTheDocument()
})

test('confirmed losses remain visible and turn neutral if confirmation is withdrawn', () => {
  const summary = {
    ...analyticsMetrics,
    profit_nano_cny: -2_000_000_000,
    profit_confirmed: true,
  }
  const view = render(<ChannelMonitorProfitValue summary={summary} />)
  expect(screen.getByText('-¥2.0000')).toHaveClass('text-destructive')
  expect(screen.queryByText('利润待确认')).not.toBeInTheDocument()
  view.rerender(
    <ChannelMonitorProfitValue
      summary={{ ...summary, profit_confirmed: false }}
    />
  )
  expect(screen.queryByText(/2\.0000/)).not.toBeInTheDocument()
  expect(screen.getByText('利润待确认')).toBeInTheDocument()
})

test('launch-day history gaps hide profit and rate in summaries, rows and daily values', async () => {
  const summary = {
    ...analyticsMetrics,
    income_nano_cny: 10_000_000_000,
    cost_nano_cny: 100_000_000_000,
    profit_nano_cny: -90_000_000_000,
    profit_rate: -9,
    profit_confirmed: false,
    income_started_at: 1790553600,
  }
  renderAnalyticsQuery('profit', (params) =>
    analyticsResponse(
      params,
      [
        analyticsItem('7', {
          ...summary,
          channel_id: 7,
          day_start: 1790524800,
        }),
      ],
      {
        scope_summary: summary,
        summary,
        coverage: {
          status: 'partial',
          covered_from: 1790553600,
          covered_through: 1790611200,
          reasons: ['income_history_unavailable'],
        },
      }
    )
  )
  await screen.findByRole('button', { name: '查看渠道 A明细' })
  expect(screen.getByText(/平台按 1:1 记账/)).toHaveTextContent(
    '用户扣费 7，收入也记 7，不乘美元展示汇率'
  )
  const trend = await screen.findByRole('region', { name: '利润历史趋势' })
  fireEvent.click(within(trend).getByText('查看每日数值'))
  expect(screen.queryAllByText('-¥90.0000')).toHaveLength(0)
  expect(screen.queryAllByText('-900.0%')).toHaveLength(0)
  expect(screen.getAllByText('利润待确认').length).toBeGreaterThan(1)
  expect(
    screen.getByText('历史缺失不会通过等待或刷新补齐。')
  ).toBeInTheDocument()
  expect(within(trend).getByText(/10\.0000/)).toBeInTheDocument()
  expect(within(trend).getByText(/100\.0000/)).toBeInTheDocument()
})

test('profit refresh reloads both income summary and trend for a new charged request', async () => {
  let income = 1_000_000_000
  const view = renderAnalyticsQuery('profit', (params) => {
    const summary = {
      ...analyticsMetrics,
      income_nano_cny: income,
      profit_confirmed: false,
    }
    return analyticsResponse(params, [], { scope_summary: summary, summary })
  })
  await screen.findByText('所选日期暂无利润记录')
  const before = view.requests.length
  income = 2_000_000_000
  fireEvent.click(screen.getByRole('button', { name: '刷新核对' }))
  await waitFor(() => {
    const refreshed = view.requests.slice(before)
    expect(refreshed.some((request) => request.group_by === 'channel')).toBe(
      true
    )
    expect(refreshed.some((request) => request.group_by === 'day')).toBe(true)
    expect(screen.getByText(/¥2\.0000/)).toBeInTheDocument()
  })
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
    expect(screen.getAllByText(label, { exact: true }).length).toBeGreaterThan(
      0
    )
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
  expect(screen.getAllByText('33.3%').length).toBeGreaterThan(0)
})

test('selecting full dates excludes launch-day history without clearing an independent recording gap', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T12:00:00+08:00'))
  const summary = {
    ...analyticsMetrics,
    income_started_at: new Date('2026-09-27T12:00:00+08:00').getTime() / 1000,
    profit_confirmed: false,
  }
  const view = renderAnalyticsQuery('profit', (params) =>
    analyticsResponse(params, [], {
      scope_summary: summary,
      coverage: {
        status: 'partial',
        covered_from: summary.income_started_at,
        covered_through: 1790611200,
        reasons:
          params.from === '2026-09-27'
            ? ['income_history_unavailable', 'income_recording_gap']
            : ['income_recording_gap'],
      },
    })
  )
  await screen.findByText('所选日期暂无利润记录')
  fireEvent.click(screen.getByRole('button', { name: '统计日期范围' }))
  fireEvent.change(screen.getByLabelText('开始日期'), {
    target: { value: '2026-09-27' },
  })
  fireEvent.click(screen.getByRole('button', { name: '应用' }))
  fireEvent.click(await screen.findByRole('button', { name: '仅看完整日期' }))
  await waitFor(() => {
    expect(
      screen.getByRole('button', { name: '统计日期范围' })
    ).toHaveTextContent('当日')
    expect(screen.getByText(/收入或成本曾写入失败/)).toBeInTheDocument()
    expect(
      screen.queryByText('历史缺失不会通过等待或刷新补齐。')
    ).not.toBeInTheDocument()
  })
  expect(screen.getByText('当前范围暂不能判断盈亏')).toBeInTheDocument()
  expect(view.requests.at(-1)?.from).toBe('2026-09-28')
})

test('failed profit refresh preserves prior income and reports failure instead of zero', async () => {
  let unavailable = false
  renderAnalyticsQuery('profit', (params) => {
    if (unavailable) throw new Error('核对服务暂不可用')
    return analyticsResponse(params, [], {
      scope_summary: { ...analyticsMetrics, income_nano_cny: 3_000_000_000 },
    })
  })
  await screen.findByText('所选日期暂无利润记录')
  unavailable = true
  fireEvent.click(screen.getByRole('button', { name: '刷新核对' }))
  await screen.findByText('统计更新失败，保留上次结果')
  expect(screen.getByText('¥3.0000')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '刷新核对' })).toBeEnabled()
})

test('expanding a loss row keeps profitable and unconfirmed children filtered out', async () => {
  const loss = {
    profit_nano_cny: -1_000_000_000,
    profit_confirmed: true,
  }
  const view = renderAnalyticsQuery('profit', (params) => {
    if (params.group_by === 'channel') {
      return analyticsResponse(params, [
        analyticsItem('7', { ...loss, channel_id: 7 }),
      ])
    }
    if (params.group_by === 'model') {
      const items = [
        analyticsItem('loss-model', {
          ...loss,
          model_key: 'loss-model',
          model_name: '亏损模型',
        }),
      ]
      if (!params.only_loss) {
        items.push(
          analyticsItem('profitable-model', {
            profit_nano_cny: 1_000_000_000,
            profit_confirmed: true,
            model_name: '盈利模型',
          }),
          analyticsItem('pending-model', {
            ...loss,
            profit_confirmed: false,
            model_name: '待确认模型',
          })
        )
      }
      return analyticsResponse(params, items)
    }
    return analyticsResponse(params, [])
  })

  await screen.findByRole('button', { name: '查看渠道 A明细' })
  fireEvent.click(screen.getByRole('button', { name: '仅展示亏损行' }))
  await waitFor(() =>
    expect(
      view.requests.some(
        (request) => request.group_by === 'channel' && request.only_loss
      )
    ).toBe(true)
  )
  fireEvent.click(await screen.findByRole('button', { name: '查看渠道 A明细' }))
  await screen.findByRole('button', { name: '查看亏损模型明细' })
  expect(
    screen.queryByRole('button', { name: '查看盈利模型明细' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: '查看待确认模型明细' })
  ).not.toBeInTheDocument()
})
