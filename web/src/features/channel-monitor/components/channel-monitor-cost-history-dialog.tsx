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

import { getChannelMonitorCostOverview } from '../api'
import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
} from '../lib/format'
import type { ChannelMonitorCostOverview } from '../types'
import { ChannelMonitorAPIKeyCostTable } from './channel-monitor-api-key-cost-table'
import { ChannelMonitorChannelCostTable } from './channel-monitor-channel-cost-table'
import { ChannelMonitorDailyBarChart } from './channel-monitor-daily-bar-chart'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'

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
  const [detailDate, setDetailDate] = useState(() =>
    formatChannelMonitorBeijingDate(new Date())
  )
  const query = useQuery({
    queryKey: [
      'channel-monitor',
      'cost',
      props.channelId ?? 'all',
      days,
      datePage,
      detailDate,
    ],
    queryFn: () =>
      getChannelMonitorCostOverview(
        days,
        props.channelId,
        datePage,
        false,
        detailDate
      ),
    enabled: props.open,
    staleTime: 30_000,
  })

  useEffect(() => {
    setDatePage(1)
    setDetailDate(formatChannelMonitorBeijingDate(new Date()))
  }, [props.channelId])

  const handleDetailDateChange = (value: string) => {
    setDetailDate(value)
    const chartItems = query.data?.data.chart_items ?? []
    const selectedIndex = chartItems.findIndex((item) => item.date === value)
    if (selectedIndex < 0) return
    const newestFirstIndex = chartItems.length - 1 - selectedIndex
    const pageSize = query.data?.data.item_page_size || 7
    setDatePage(Math.floor(newestFirstIndex / pageSize) + 1)
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'flex flex-col sm:max-w-5xl'
        )}
      >
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>
            {props.channelName ? `渠道成本：${props.channelName}` : '渠道成本'}
          </DialogTitle>
          <DialogDescription>
            按北京时间记录请求结算时固化的已结算上游成本；未解析尝试单独展示，后续配置更新不会改写历史金额。
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
                      setDetailDate(formatChannelMonitorBeijingDate(new Date()))
                      break
                    case '30':
                      setDays(30)
                      setDatePage(1)
                      setDetailDate(formatChannelMonitorBeijingDate(new Date()))
                      break
                    case '90':
                      setDays(90)
                      setDatePage(1)
                      setDetailDate(formatChannelMonitorBeijingDate(new Date()))
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
              onDatePageChange={setDatePage}
              selectedDate={detailDate}
              onDetailDateChange={handleDetailDateChange}
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
        label='今日已结算成本'
        value={props.overview?.today_cost_cny}
        probeValue={props.overview?.today_probe_cost_cny}
      />
      <CostSummaryValue
        label='昨日已结算成本'
        value={props.overview?.yesterday_cost_cny}
        probeValue={props.overview?.yesterday_probe_cost_cny}
      />
      <CostSummaryValue
        label='区间已结算成本'
        value={props.overview?.total_cost_cny}
        probeValue={props.overview?.total_probe_cost_cny}
      />
    </div>
  )
}

function CostSummaryValue(props: {
  label: string
  value: number | undefined
  probeValue: number | undefined
}) {
  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='truncate font-mono text-base font-semibold tabular-nums sm:text-lg'>
        {formatChannelMonitorCost(props.value)}
      </span>
      <span className='text-muted-foreground truncate text-xs'>
        其中探测 {formatChannelMonitorCost(props.probeValue)}
      </span>
    </div>
  )
}

function CostHistoryContent(props: {
  loading: boolean
  error: boolean
  overview: ChannelMonitorCostOverview | undefined
  onDatePageChange: (page: number) => void
  selectedDate?: string
  onDetailDateChange: (date: string) => void
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
    props.overview.coverage.settled_count +
      props.overview.coverage.unresolved_count ===
    0
  ) {
    content = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={MoneyBag02Icon} />
          </EmptyMedia>
          <EmptyTitle>暂无成本记录</EmptyTitle>
          <EmptyDescription>
            从功能启用后的上游请求尝试开始记录
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <CostHistoryData
        overview={props.overview}
        onDatePageChange={props.onDatePageChange}
        selectedDate={props.selectedDate}
        onDetailDateChange={props.onDetailDateChange}
      />
    )
  }
  return content
}

export function CostHistoryData(props: {
  overview: ChannelMonitorCostOverview
  onDatePageChange?: (page: number) => void
  selectedDate?: string
  onDetailDateChange?: (date: string) => void
}) {
  const datePageCount = Math.max(1, props.overview.item_page_count || 1)
  const currentDatePage = Math.min(props.overview.item_page || 1, datePageCount)
  const dateItems = useMemo(() => {
    return [...props.overview.items].reverse()
  }, [props.overview.items])

  const chartItems = props.overview.chart_items ?? props.overview.items
  const detailDateOptions = useMemo(() => {
    return [...chartItems].reverse().map((item) => ({
      value: item.date,
      label: item.date,
    }))
  }, [chartItems])
  const detailDate =
    props.selectedDate ||
    props.overview.detail_date ||
    detailDateOptions[0]?.value ||
    ''

  const chartSpec = useMemo(
    () => ({
      type: 'bar' as const,
      data: [
        {
          id: 'channel-cost',
          values: chartItems.map((item) => ({
            date: item.date,
            cost: item.cost_cny,
            probeCost: item.probe_cost_cny ?? 0,
            settledCount: item.settled_count,
            unresolvedCount: item.unresolved_count,
            resolutionRate: formatChannelMonitorResolutionRate(
              item.settled_count,
              item.unresolved_count
            ),
            selected: item.date === detailDate,
          })),
        },
      ],
      xField: 'date',
      yField: 'cost',
      bar: {
        style: {
          cornerRadius: [4, 4, 0, 0],
          cursor: 'pointer',
          fillOpacity: (datum: { selected: boolean }) =>
            datum.selected ? 1 : 0.55,
        },
      },
      legends: { visible: false },
      tooltip: {
        mark: {
          title: { value: (datum: { date: string }) => datum.date },
          content: [
            {
              key: '已结算成本',
              value: (datum: { cost: number }) =>
                formatChannelMonitorCost(datum.cost),
            },
            {
              key: '探测成本',
              value: (datum: { probeCost: number }) =>
                formatChannelMonitorCost(datum.probeCost),
            },
            {
              key: '已结算请求',
              value: (datum: { settledCount: number }) => datum.settledCount,
            },
            {
              key: '未解析请求',
              value: (datum: { unresolvedCount: number }) =>
                datum.unresolvedCount,
            },
            {
              key: '解析率',
              value: (datum: { resolutionRate: string }) =>
                datum.resolutionRate,
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
    [chartItems, detailDate]
  )

  const coverage = props.overview.coverage
  return (
    <Tabs defaultValue='overview' className='min-h-0'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <TabsList className='grid w-full grid-cols-3 sm:w-fit'>
          <TabsTrigger value='overview'>成本趋势</TabsTrigger>
          <TabsTrigger value='channels'>渠道汇总</TabsTrigger>
          <TabsTrigger value='api-keys'>API Key 明细</TabsTrigger>
        </TabsList>
        <Select
          items={detailDateOptions}
          value={detailDate}
          disabled={detailDateOptions.length === 0}
          onValueChange={(value) => {
            if (value) props.onDetailDateChange?.(value)
          }}
        >
          <SelectTrigger
            className='w-full font-mono sm:w-36'
            aria-label='成本明细日期'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {detailDateOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
      <div className='mt-3'>
        <ChannelMonitorDailyBarChart
          ariaLabel='每日成本柱状图'
          chartKey={`cost:${props.overview.days}`}
          spec={chartSpec}
          onDateChange={(date) => props.onDetailDateChange?.(date)}
        />
      </div>
      <TabsContent value='overview' className='mt-3 min-h-0'>
        <div className='flex flex-col gap-3'>
          <CostCoverage coverage={coverage} />
          <section className='flex min-w-0 flex-col gap-2'>
            <h3 className='text-sm font-medium'>按日成本</h3>
            <div className='overflow-auto rounded-md border'>
              <Table className='min-w-[760px]'>
                <TableHeader>
                  <TableRow>
                    <TableHead>日期</TableHead>
                    <TableHead className='text-right'>已结算成本</TableHead>
                    <TableHead className='text-right'>探测成本</TableHead>
                    <TableHead className='text-right'>已结算</TableHead>
                    <TableHead className='text-right'>未解析</TableHead>
                    <TableHead className='text-right'>解析率</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {dateItems.map((item) => {
                    const selected = item.date === detailDate
                    return (
                      <TableRow
                        key={item.start_at}
                        data-selected-date={selected || undefined}
                        data-state={selected ? 'selected' : undefined}
                        aria-current={selected ? 'date' : undefined}
                      >
                        <TableCell className='font-mono'>{item.date}</TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatChannelMonitorCost(item.cost_cny)}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatChannelMonitorCost(item.probe_cost_cny)}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {item.settled_count}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {item.unresolved_count}
                        </TableCell>
                        <TableCell className='text-right font-mono tabular-nums'>
                          {formatChannelMonitorResolutionRate(
                            item.settled_count,
                            item.unresolved_count
                          )}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          </section>
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
        </div>
      </TabsContent>
      <TabsContent value='channels' className='mt-3 min-h-0'>
        <ChannelMonitorChannelCostTable
          items={props.overview.channels ?? []}
          detailDate={detailDate}
        />
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
  const resolutionRate = formatChannelMonitorResolutionRate(
    props.coverage.settled_count,
    props.coverage.unresolved_count
  )
  return (
    <div className='bg-muted/30 flex items-start gap-2 rounded-md border px-3 py-2 text-xs'>
      <HugeiconsIcon
        icon={ChartLineData01Icon}
        className='text-muted-foreground mt-0.5 shrink-0'
      />
      <div className='flex min-w-0 flex-col gap-1'>
        <span className='font-medium'>
          已结算请求 {props.coverage.settled_count} · 未解析请求{' '}
          {props.coverage.unresolved_count} · 解析率 {resolutionRate}
        </span>
        <span className='text-muted-foreground'>
          已结算渠道 {props.coverage.included_channel_count} 个 ·
          存在未解析尝试的渠道 {props.coverage.unresolved_channel_count} 个
        </span>
        {props.coverage.unresolved_count > 0 ? (
          <span className='text-warning'>
            当前金额不包含 {props.coverage.unresolved_count}{' '}
            次未解析的上游请求尝试。
          </span>
        ) : null}
        {props.coverage.missing_cost_config_channel_count > 0 ? (
          <span className='text-warning'>
            其中 {props.coverage.missing_cost_config_channel_count}{' '}
            个渠道缺少有效成本配置。
          </span>
        ) : null}
      </div>
    </div>
  )
}
