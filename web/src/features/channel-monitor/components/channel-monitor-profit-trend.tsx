import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  XAxis,
  YAxis,
} from 'recharts'

import { Button } from '@/components/ui/button'
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'

import { useChannelMonitorAnalytics } from '../hooks/use-channel-monitor-analytics'
import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import { formatProfitMoney, getProfitDisplayStatus } from '../lib/profit-format'
import type { ChannelMonitorAnalyticsQuery } from '../types-analytics'
import { ChannelMonitorAnalyticsCoverage } from './channel-monitor-analytics-coverage'
import { ChannelMonitorProfitValue } from './channel-monitor-profit'

export function ChannelMonitorProfitTrend({
  request,
}: {
  request: ChannelMonitorAnalyticsQuery
}) {
  const query = useChannelMonitorAnalytics({
    ...request,
    groupBy: 'day',
    onlyLoss: undefined,
    page: 1,
    pageSize: 200,
  })
  if (query.isLoading) return <Skeleton className='h-48 w-full' />
  if (query.isError) {
    return (
      <div role='alert' className='text-sm'>
        利润趋势加载失败{' '}
        <Button
          variant='outline'
          size='sm'
          onClick={() => void query.refetch()}
        >
          重试
        </Button>
      </div>
    )
  }
  const response = query.data?.data
  if (!response || response.items.length === 0) {
    return <p className='text-muted-foreground text-sm'>所选日期暂无利润记录</p>
  }
  const rows = [...response.items].sort(
    (a, b) => (a.day_start ?? 0) - (b.day_start ?? 0)
  )
  const estimated = rows.some(
    (row) =>
      getProfitDisplayStatus(row, response.coverage, row.day_start) ===
      'estimated'
  )
  const data = rows.map((row) => {
    const status = getProfitDisplayStatus(row, response.coverage, row.day_start)
    return {
      date: formatChannelMonitorBeijingDate(
        new Date((row.day_start ?? 0) * 1000)
      ),
      income: (row.income_nano_cny ?? 0) / 1e9,
      cost: (row.cost_nano_cny ?? 0) / 1e9,
      profit:
        (status === 'confirmed' || status === 'estimated') &&
        row.profit_nano_cny != null
          ? row.profit_nano_cny / 1e9
          : null,
    }
  })
  return (
    <section aria-label='利润历史趋势' className='rounded-lg border p-3'>
      <div className='mb-2 text-sm font-medium'>每日趋势（人民币）</div>
      <ChartContainer
        className='h-48 w-full'
        config={{
          income: { label: '用户扣费', color: 'var(--chart-1)' },
          cost: { label: '成本', color: 'var(--chart-2)' },
          profit: {
            label: estimated ? '利润（含暂估）' : '利润',
            color: 'var(--chart-3)',
          },
        }}
      >
        <LineChart data={data} accessibilityLayer>
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey='date'
            tickFormatter={(value: string) => value.slice(5)}
            tickLine={false}
            axisLine={false}
            minTickGap={24}
          />
          <YAxis tickLine={false} axisLine={false} width={60} />
          <ReferenceLine y={0} stroke='var(--border)' />
          <ChartTooltip content={<ChartTooltipContent />} />
          <ChartLegend content={<ChartLegendContent />} />
          <Line
            dataKey='income'
            stroke='var(--color-income)'
            strokeDasharray='4 3'
            isAnimationActive={false}
            dot={rows.length === 1}
          />
          <Line
            dataKey='cost'
            stroke='var(--color-cost)'
            strokeDasharray='2 3'
            isAnimationActive={false}
            dot={rows.length === 1}
          />
          <Line
            dataKey='profit'
            stroke='var(--color-profit)'
            strokeWidth={2}
            isAnimationActive={false}
            dot={rows.length === 1}
          />
        </LineChart>
      </ChartContainer>
      {response.coverage.reasons.includes('income_history_unavailable') ||
      response.coverage.reasons.includes('profit_history_expired') ||
      response.coverage.status === 'unavailable' ? (
        <ChannelMonitorAnalyticsCoverage
          coverage={response.coverage}
          scope='趋势'
        />
      ) : null}
      <details className='mt-2 text-xs'>
        <summary className='cursor-pointer'>查看每日数值</summary>
        <div className='mt-2 overflow-x-auto'>
          <table className='w-full text-right tabular-nums'>
            <thead>
              <tr>
                <th className='text-left'>日期</th>
                <th>扣费</th>
                <th>成本</th>
                <th>利润</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.key}>
                  <td className='text-left'>
                    {formatChannelMonitorBeijingDate(
                      new Date((row.day_start ?? 0) * 1000)
                    )}
                  </td>
                  <td>{formatProfitMoney(row.income_nano_cny)}</td>
                  <td>{formatProfitMoney(row.cost_nano_cny)}</td>
                  <td>
                    <ChannelMonitorProfitValue
                      summary={row}
                      coverage={response.coverage}
                      dayStart={row.day_start}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </section>
  )
}
