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
import { Alert02Icon, Route01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'

import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardAction,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  formatChannelMonitorSmartSchedulePriorityWeightRange,
  getChannelMonitorSmartSchedulePoolStatus,
  summarizeChannelMonitorSmartScheduleOverview,
  summarizeChannelMonitorSmartSchedulePools,
  type ChannelMonitorSmartSchedulePoolStatus,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../types'

type ChannelMonitorSmartScheduleOverviewCardProps = {
  result: ChannelMonitorSmartScheduleRouteResult | undefined
  isLoading: boolean
  isError: boolean
  onOpen: () => void
}

type ChannelMonitorSmartScheduleOverviewDialogProps = {
  result: ChannelMonitorSmartScheduleRouteResult | undefined
  isLoading: boolean
  isError: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

const EMPTY_SMART_SCHEDULE_ROUTES: readonly ChannelMonitorSmartScheduleRoute[] =
  []

function OverviewMetric(props: { label: string; value: string | number }) {
  return (
    <div className='bg-background flex min-h-20 flex-col justify-center gap-1 px-3 py-3'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </span>
    </div>
  )
}

function getStatusVariant(status: ChannelMonitorSmartSchedulePoolStatus) {
  if (status === '低成功率') return 'destructive'
  if (status === '稳定性试放') return 'warning'
  if (status === '当前不可调度') return 'destructive'
  if (status === '部分可调度') return 'warning'
  if (status === '正常') return 'secondary'
  return 'outline'
}

export function ChannelMonitorSmartScheduleOverviewCard(
  props: ChannelMonitorSmartScheduleOverviewCardProps
) {
  const routes = props.result?.routes ?? EMPTY_SMART_SCHEDULE_ROUTES
  const summary = useMemo(
    () => summarizeChannelMonitorSmartScheduleOverview(routes),
    [routes]
  )
  const value =
    props.isLoading || props.isError
      ? '-'
      : `${summary.participatingCount}/${summary.routeCount}`
  let description = `${summary.poolCount} 个调度池 · 当前可调度 ${summary.activeCount}`
  if (props.isLoading) description = '正在加载调度路由'
  else if (props.isError) description = '调度统计加载失败'

  return (
    <Card
      size='sm'
      className='hover:bg-muted/50 focus-visible:ring-ring/50 h-full cursor-pointer transition-colors outline-none focus-visible:ring-3'
      role='button'
      tabIndex={0}
      aria-label={`查看智能调度总览，参与配置 ${value}`}
      onClick={props.onOpen}
      onKeyDown={(event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return
        event.preventDefault()
        props.onOpen()
      }}
    >
      <CardHeader>
        <CardDescription>智能调度</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{value}</CardTitle>
        <CardAction>
          <span className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={Route01Icon} aria-hidden='true' />
          </span>
        </CardAction>
        <CardDescription className='flex flex-wrap gap-x-3 gap-y-1'>
          <span>{description}</span>
          {!props.isLoading && !props.isError ? (
            <span>
              {props.result?.enabled ? '已启用' : '已禁用'} ·{' '}
              {summary.groupCount} 个分组 · 低成功率 {summary.degradedCount} ·
              试放 {summary.probingCount} · 最近失败 {summary.failedCount}
            </span>
          ) : null}
        </CardDescription>
      </CardHeader>
    </Card>
  )
}

export function ChannelMonitorSmartScheduleOverviewDialog(
  props: ChannelMonitorSmartScheduleOverviewDialogProps
) {
  const routes = props.result?.routes ?? EMPTY_SMART_SCHEDULE_ROUTES
  const summary = useMemo(
    () => summarizeChannelMonitorSmartScheduleOverview(routes),
    [routes]
  )
  const pools = useMemo(
    () => summarizeChannelMonitorSmartSchedulePools(routes),
    [routes]
  )
  const description = props.result?.generated_at
    ? `按分组和模型统计 · 更新于 ${new Date(props.result.generated_at * 1000).toLocaleString()}`
    : '按分组和模型统计全部智能调度路由'

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[85dvh] flex-col overflow-hidden sm:max-w-5xl'>
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>智能调度总览</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {props.isLoading ? (
          <div className='flex flex-col gap-3'>
            <Skeleton className='h-24 w-full' />
            <Skeleton className='h-64 w-full' />
          </div>
        ) : null}
        {!props.isLoading && props.isError ? (
          <Empty className='min-h-64 border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <HugeiconsIcon icon={Alert02Icon} />
              </EmptyMedia>
              <EmptyTitle>智能调度统计加载失败</EmptyTitle>
              <EmptyDescription>请刷新页面后重试</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : null}
        {!props.isLoading && !props.isError ? (
          <div className='min-h-0 flex-1 overflow-y-auto pr-1'>
            <div className='bg-border grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-3 lg:grid-cols-9'>
              <OverviewMetric label='路由总数' value={summary.routeCount} />
              <OverviewMetric
                label='参与配置'
                value={summary.participatingCount}
              />
              <OverviewMetric label='当前可调度' value={summary.activeCount} />
              <OverviewMetric label='渠道数' value={summary.channelCount} />
              <OverviewMetric label='分组数' value={summary.groupCount} />
              <OverviewMetric label='调度池' value={summary.poolCount} />
              <OverviewMetric label='低成功率' value={summary.degradedCount} />
              <OverviewMetric label='稳定性试放' value={summary.probingCount} />
              <OverviewMetric label='最近失败' value={summary.failedCount} />
            </div>

            <div className='mt-5 flex flex-col gap-2'>
              <div className='flex items-center justify-between gap-3'>
                <h3 className='font-medium'>分组 / 模型调度池</h3>
                <Badge
                  variant={props.result?.enabled ? 'secondary' : 'outline'}
                >
                  {props.result?.enabled ? '调度已启用' : '调度已禁用'}
                </Badge>
              </div>
              {pools.length === 0 ? (
                <Empty className='min-h-48 border-0'>
                  <EmptyHeader>
                    <EmptyTitle>暂无调度池</EmptyTitle>
                    <EmptyDescription>
                      请先关联渠道、分组和模型
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : (
                <div className='overflow-x-auto rounded-lg border'>
                  <Table className='min-w-[820px]'>
                    <TableHeader>
                      <TableRow>
                        <TableHead>分组</TableHead>
                        <TableHead>模型</TableHead>
                        <TableHead>参与 / 总路由</TableHead>
                        <TableHead>当前可调度</TableHead>
                        <TableHead>优先级 / 权重</TableHead>
                        <TableHead>状态</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {pools.map((pool) => {
                        const status =
                          getChannelMonitorSmartSchedulePoolStatus(pool)
                        const variant = getStatusVariant(status)
                        return (
                          <TableRow key={`${pool.group}\u0000${pool.model}`}>
                            <TableCell className='font-medium'>
                              {pool.group}
                            </TableCell>
                            <TableCell className='max-w-72 break-words whitespace-normal'>
                              {pool.model}
                            </TableCell>
                            <TableCell className='font-mono tabular-nums'>
                              {pool.participatingCount}/{pool.routeCount}
                            </TableCell>
                            <TableCell className='font-mono tabular-nums'>
                              {pool.activeCount}
                            </TableCell>
                            <TableCell className='font-mono tabular-nums'>
                              {formatChannelMonitorSmartSchedulePriorityWeightRange(
                                pool
                              )}
                            </TableCell>
                            <TableCell>
                              <Badge variant={variant}>{status}</Badge>
                            </TableCell>
                          </TableRow>
                        )
                      })}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
