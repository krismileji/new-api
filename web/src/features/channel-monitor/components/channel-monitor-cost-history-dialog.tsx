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
import {
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ChartLineData01Icon,
  MoneyBag02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { useEffect, useMemo, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { getChannelMonitorCostOverview } from '../api'
import { formatChannelMonitorCost } from '../lib/format'
import type { ChannelMonitorCostOverview } from '../types'
import { ChannelMonitorAPIKeyCostTable } from './channel-monitor-api-key-cost-table'

const COST_HISTORY_RANGE_OPTIONS = [
  { value: '7', label: '近 7 天' },
  { value: '30', label: '近 30 天' },
  { value: '90', label: '近 90 天' },
]

type ChannelMonitorCostHistoryDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelId?: number
  channelName?: string
}

export function ChannelMonitorCostHistoryDialog(
  props: ChannelMonitorCostHistoryDialogProps
) {
  const [days, setDays] = useState(30)
  const [datePage, setDatePage] = useState(1)
  const query = useQuery({
    queryKey: [
      'channel-monitor',
      'cost',
      props.channelId ?? 'all',
      days,
      datePage,
    ],
    queryFn: () =>
      getChannelMonitorCostOverview(days, props.channelId, datePage),
    enabled: props.open,
    staleTime: 30_000,
  })

  useEffect(() => {
    setDatePage(1)
  }, [props.channelId, days])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[min(78dvh,48rem)] flex-col overflow-hidden sm:max-w-5xl'>
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>
            {props.channelName ? `渠道成本：${props.channelName}` : '渠道成本'}
          </DialogTitle>
          <DialogDescription>
            按北京时间记录请求结算时固化的上游成本；后续配置更新不会改写历史金额。
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 flex-1 overflow-y-auto pr-1'>
          <div className='flex flex-col gap-4 pb-1'>
            <div className='flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-end sm:justify-between'>
              <CostSummary
                overview={query.data?.data}
                loading={query.isLoading}
              />
              <Select
                items={COST_HISTORY_RANGE_OPTIONS}
                value={String(days)}
                onValueChange={(value) => {
                  switch (value) {
                    case '7':
                      setDays(7)
                      setDatePage(1)
                      break
                    case '30':
                      setDays(30)
                      setDatePage(1)
                      break
                    case '90':
                      setDays(90)
                      setDatePage(1)
                      break
                  }
                }}
              >
                <SelectTrigger
                  className='w-full sm:w-32'
                  aria-label='成本统计时间范围'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {COST_HISTORY_RANGE_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <CostHistoryContent
              loading={query.isLoading}
              error={query.isError}
              overview={query.data?.data}
              channelId={props.channelId}
              onDatePageChange={setDatePage}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function CostSummary(props: {
  overview: ChannelMonitorCostOverview | undefined
  loading: boolean
}) {
  if (props.loading) {
    return <Skeleton className='h-16 w-full sm:w-96' />
  }

  return (
    <div className='grid min-w-0 grid-cols-3 gap-4 sm:gap-8'>
      <CostSummaryValue
        label='今日成本'
        value={props.overview?.today_cost_cny}
      />
      <CostSummaryValue
        label='昨日成本'
        value={props.overview?.yesterday_cost_cny}
      />
      <CostSummaryValue
        label='区间累计'
        value={props.overview?.total_cost_cny}
      />
    </div>
  )
}

function CostSummaryValue(props: { label: string; value: number | undefined }) {
  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='truncate font-mono text-base font-semibold tabular-nums sm:text-lg'>
        {formatChannelMonitorCost(props.value)}
      </span>
    </div>
  )
}

function CostHistoryContent(props: {
  loading: boolean
  error: boolean
  overview: ChannelMonitorCostOverview | undefined
  channelId?: number
  onDatePageChange: (page: number) => void
}) {
  let content: ReactNode
  if (props.loading) {
    content = (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-56 w-full' />
        <Skeleton className='h-48 w-full' />
      </div>
    )
  } else if (props.error || !props.overview) {
    content = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={MoneyBag02Icon} />
          </EmptyMedia>
          <EmptyTitle>成本统计加载失败</EmptyTitle>
          <EmptyDescription>请稍后重试</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (
    props.overview.coverage.included_channel_count === 0 &&
    props.overview.coverage.unresolved_channel_count === 0
  ) {
    content = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={MoneyBag02Icon} />
          </EmptyMedia>
          <EmptyTitle>暂无成本记录</EmptyTitle>
          <EmptyDescription>
            从功能启用后的成功上游请求开始记录
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <CostHistoryData
        overview={props.overview}
        channelId={props.channelId}
        onDatePageChange={props.onDatePageChange}
      />
    )
  }
  return content
}

export function CostHistoryData(props: {
  overview: ChannelMonitorCostOverview
  channelId?: number
  onDatePageChange?: (page: number) => void
}) {
  const { resolvedTheme, themeReady } = useChartTheme()
  const datePageCount = Math.max(1, props.overview.item_page_count || 1)
  const currentDatePage = Math.min(props.overview.item_page || 1, datePageCount)
  const dateItems = useMemo(() => {
    return [...props.overview.items].reverse()
  }, [props.overview.items])

  const chartItems = props.overview.chart_items ?? props.overview.items

  const chartSpec = useMemo(
    () => ({
      type: 'bar' as const,
      data: [
        {
          id: 'channel-cost',
          values: chartItems.map((item) => ({
            date: item.date,
            cost: item.cost_cny,
          })),
        },
      ],
      xField: 'date',
      yField: 'cost',
      bar: {
        style: {
          cornerRadius: [4, 4, 0, 0],
        },
      },
      legends: { visible: false },
      tooltip: {
        mark: {
          title: { value: (datum: { date: string }) => datum.date },
          content: [
            {
              key: '成本',
              value: (datum: { cost: number }) =>
                formatChannelMonitorCost(datum.cost),
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: { autoHide: true },
          tick: { visible: false },
        },
        {
          orient: 'left',
          label: {
            formatMethod: (value: number | string) =>
              formatChannelMonitorCost(Number(value)),
          },
        },
      ],
    }),
    [chartItems]
  )

  const coverage = props.overview.coverage
  return (
    <Tabs defaultValue='overview' className='min-h-0'>
      <TabsList className='grid w-full grid-cols-2 sm:w-fit'>
        <TabsTrigger value='overview'>成本趋势</TabsTrigger>
        <TabsTrigger value='api-keys'>API Key 明细</TabsTrigger>
      </TabsList>
      <TabsContent value='overview' className='mt-3 min-h-0'>
        <div className='flex flex-col gap-3'>
          <div className='h-48 overflow-hidden rounded-md border sm:h-56'>
            {themeReady && (
              <VChart
                key={`${props.overview.days}:${resolvedTheme}`}
                spec={{
                  ...chartSpec,
                  theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                  background: 'transparent',
                }}
                option={VCHART_OPTION}
              />
            )}
          </div>
          <CostCoverage coverage={coverage} />
          <div className='grid gap-3 lg:grid-cols-2'>
            <section className='flex min-w-0 flex-col gap-2'>
              <h3 className='text-sm font-medium'>按日成本</h3>
              <div className='overflow-auto rounded-md border'>
                <Table className='min-w-[360px]'>
                  <TableHeader>
                    <TableRow>
                      <TableHead>日期</TableHead>
                      <TableHead className='text-right'>成本</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {dateItems.map((item) => (
                      <TableRow key={item.start_at}>
                        <TableCell className='font-mono'>{item.date}</TableCell>
                        <TableCell className='text-right'>
                          <div className='flex flex-col items-end gap-0.5 font-mono tabular-nums'>
                            <span>
                              {formatChannelMonitorCost(item.cost_cny)}
                            </span>
                            {item.unresolved_count > 0 ? (
                              <span className='text-warning font-sans text-xs'>
                                不完整
                              </span>
                            ) : null}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              {datePageCount > 1 ? (
                <div className='flex items-center justify-end gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='icon-sm'
                    aria-label='上一页日期'
                    title='上一页日期'
                    onClick={() =>
                      props.onDatePageChange?.(Math.max(1, currentDatePage - 1))
                    }
                    disabled={currentDatePage <= 1}
                  >
                    <HugeiconsIcon icon={ArrowLeft01Icon} />
                  </Button>
                  <span className='text-muted-foreground min-w-24 text-center text-xs tabular-nums'>
                    日期第 {currentDatePage} / {datePageCount} 页
                  </span>
                  <Button
                    type='button'
                    variant='outline'
                    size='icon-sm'
                    aria-label='下一页日期'
                    title='下一页日期'
                    onClick={() =>
                      props.onDatePageChange?.(
                        Math.min(datePageCount, currentDatePage + 1)
                      )
                    }
                    disabled={currentDatePage >= datePageCount}
                  >
                    <HugeiconsIcon icon={ArrowRight01Icon} />
                  </Button>
                </div>
              ) : null}
            </section>
            {props.channelId == null && props.overview.channels.length > 0 ? (
              <section className='flex min-w-0 flex-col gap-2'>
                <h3 className='text-sm font-medium'>渠道汇总</h3>
                <div className='max-h-64 overflow-auto rounded-md border'>
                  <Table className='min-w-[360px]'>
                    <TableHeader>
                      <TableRow>
                        <TableHead>渠道</TableHead>
                        <TableHead className='text-right'>区间成本</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {props.overview.channels.map((channel) => (
                        <TableRow key={channel.channel_id}>
                          <TableCell className='max-w-72'>
                            <span
                              className='block truncate'
                              title={channel.channel_name}
                            >
                              {channel.channel_name}
                            </span>
                          </TableCell>
                          <TableCell className='text-right'>
                            <div className='flex flex-col items-end gap-0.5 font-mono tabular-nums'>
                              <span>
                                {formatChannelMonitorCost(channel.cost_cny)}
                              </span>
                              {channel.unresolved_count > 0 ? (
                                <span className='text-warning font-sans text-xs'>
                                  不完整
                                </span>
                              ) : null}
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </section>
            ) : null}
          </div>
        </div>
      </TabsContent>
      <TabsContent value='api-keys' className='mt-3 min-h-0'>
        <ChannelMonitorAPIKeyCostTable items={props.overview.api_keys ?? []} />
      </TabsContent>
    </Tabs>
  )
}

function CostCoverage(props: {
  coverage: ChannelMonitorCostOverview['coverage']
}) {
  const values = [`已结算 ${props.coverage.included_channel_count} 个渠道`]
  if (props.coverage.unresolved_channel_count > 0) {
    values.push(
      `${props.coverage.unresolved_channel_count} 个渠道存在未确认成本`
    )
  }
  return (
    <div className='text-muted-foreground flex items-start gap-2 text-xs'>
      <HugeiconsIcon icon={ChartLineData01Icon} className='mt-0.5 shrink-0' />
      <span>{values.join('；')}</span>
    </div>
  )
}
