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
  Alert02Icon,
  Analytics01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, type ReactNode } from 'react'

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
  EmptyContent,
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
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatMonitorRatio } from '../lib/format'
import type {
  ChannelMonitorItem,
  ChannelMonitorSuccessSummary,
  ChannelMonitorTodaySuccessResult,
} from '../types'
import { ChannelMonitorStatusBadge } from './channel-monitor-status-badge'
import { ChannelMonitorSuccessAPIKeyTable } from './channel-monitor-success-api-key-table'

type ChannelMonitorTodaySuccessChannelMetadata = Pick<
  ChannelMonitorItem,
  'id' | 'name' | 'status' | 'status_reason' | 'cost_ratio' | 'channel_remark'
>

type ChannelMonitorTodaySuccessDialogProps = {
  result: ChannelMonitorTodaySuccessResult | undefined
  channels: readonly ChannelMonitorTodaySuccessChannelMetadata[]
  isLoading: boolean
  isError: boolean
  isFetching: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onRetry: () => void
}

type TodaySuccessSummaryValueProps = {
  label: string
  value: ReactNode
  valueClassName?: string
}

export type ChannelMonitorTodaySuccessDialogContentProps = Omit<
  ChannelMonitorTodaySuccessDialogProps,
  'open' | 'onOpenChange'
>

const percentFormatter = new Intl.NumberFormat(undefined, {
  style: 'percent',
  maximumFractionDigits: 2,
})

function formatRate(rate: number, sampleCount: number) {
  return sampleCount > 0 ? percentFormatter.format(rate) : '-'
}

function getRateClassName(rate: number) {
  if (rate >= 0.9) return 'text-success'
  if (rate >= 0.7) return 'text-warning'
  return 'text-destructive'
}

function TodaySuccessSummaryValue(props: TodaySuccessSummaryValueProps) {
  return (
    <div className='flex min-h-20 flex-col justify-center gap-1 px-4 py-3'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span
        className={cn(
          'font-mono text-xl font-semibold tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function TodaySuccessSummary(props: { summary: ChannelMonitorSuccessSummary }) {
  const successRateClassName =
    props.summary.actual_sample_count > 0
      ? getRateClassName(props.summary.actual_success_rate)
      : 'text-muted-foreground'
  const cacheRateClassName =
    props.summary.cache_sample_count > 0
      ? 'text-foreground'
      : 'text-muted-foreground'

  return (
    <div className='grid shrink-0 grid-cols-1 divide-y rounded-lg border sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
      <TodaySuccessSummaryValue
        label='今日请求数'
        value={`${props.summary.actual_sample_count} 次`}
      />
      <TodaySuccessSummaryValue
        label='今日成功率'
        value={formatRate(
          props.summary.actual_success_rate,
          props.summary.actual_sample_count
        )}
        valueClassName={successRateClassName}
      />
      <TodaySuccessSummaryValue
        label='今日缓存率'
        value={formatRate(
          props.summary.cache_hit_rate,
          props.summary.cache_sample_count
        )}
        valueClassName={cacheRateClassName}
      />
    </div>
  )
}

export function ChannelMonitorTodaySuccessDialogContent(
  props: ChannelMonitorTodaySuccessDialogContentProps
) {
  const result = props.result
  const summary = result?.summary
  const channelRows = useMemo(() => {
    const channelById = new Map(
      props.channels.map((channel) => [channel.id, channel])
    )

    return (result?.channel_items ?? [])
      .map((metric) => ({
        metric,
        channel: channelById.get(metric.channel_id) ?? null,
      }))
      .sort((first, second) => {
        const firstEnabled = first.channel?.status === CHANNEL_STATUS.ENABLED
        const secondEnabled = second.channel?.status === CHANNEL_STATUS.ENABLED
        if (firstEnabled !== secondEnabled) return firstEnabled ? -1 : 1

        const firstRatio =
          first.channel?.cost_ratio != null &&
          Number.isFinite(first.channel.cost_ratio)
            ? first.channel.cost_ratio
            : null
        const secondRatio =
          second.channel?.cost_ratio != null &&
          Number.isFinite(second.channel.cost_ratio)
            ? second.channel.cost_ratio
            : null
        if (firstRatio == null && secondRatio != null) return 1
        if (firstRatio != null && secondRatio == null) return -1
        if (
          firstRatio != null &&
          secondRatio != null &&
          firstRatio !== secondRatio
        ) {
          return firstRatio - secondRatio
        }

        const firstName =
          first.channel?.name ||
          first.metric.channel_name ||
          `渠道 #${first.metric.channel_id}`
        const secondName =
          second.channel?.name ||
          second.metric.channel_name ||
          `渠道 #${second.metric.channel_id}`
        const nameOrder = firstName.localeCompare(secondName)
        return nameOrder !== 0
          ? nameOrder
          : first.metric.channel_id - second.metric.channel_id
      })
  }, [props.channels, result?.channel_items])

  if (props.isLoading) {
    return (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    )
  }

  if (props.isError) {
    return (
      <Empty className='min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} />
          </EmptyMedia>
          <EmptyTitle>今日请求统计加载失败</EmptyTitle>
          <EmptyDescription>网络或服务暂时不可用</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button
            type='button'
            variant='outline'
            onClick={props.onRetry}
            disabled={props.isFetching}
          >
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重新加载
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  if (!result?.success_metrics_available) {
    return (
      <Empty className='min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} />
          </EmptyMedia>
          <EmptyTitle>成功率统计不可用</EmptyTitle>
          <EmptyDescription>需要同时开启消费日志和错误日志</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  if (
    !summary ||
    summary.actual_sample_count === 0 ||
    channelRows.length === 0
  ) {
    return (
      <Empty className='min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Analytics01Icon} />
          </EmptyMedia>
          <EmptyTitle>今日暂无请求数据</EmptyTitle>
          <EmptyDescription>今日尚未记录可统计的请求</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex min-h-0 flex-col gap-4 overflow-y-auto pr-1'>
      <TodaySuccessSummary summary={summary} />
      <div className='flex min-h-0 flex-col gap-2'>
        <h3 className='font-medium'>渠道明细</h3>
        <div className='min-h-0 overflow-hidden rounded-lg border'>
          <Table className='w-full table-fixed'>
            <TableHeader>
              <TableRow>
                <TableHead className='w-[22%] whitespace-normal'>
                  渠道
                </TableHead>
                <TableHead className='w-[26%] whitespace-normal'>
                  备注
                </TableHead>
                <TableHead className='w-[12%] text-right whitespace-normal'>
                  成本倍率
                </TableHead>
                <TableHead className='w-[14%] text-right whitespace-normal'>
                  今日请求数
                </TableHead>
                <TableHead className='w-[13%] text-right whitespace-normal'>
                  成功率
                </TableHead>
                <TableHead className='w-[13%] text-right whitespace-normal'>
                  缓存率
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {channelRows.map((row) => {
                const item = row.metric
                const channelName =
                  row.channel?.name ||
                  item.channel_name ||
                  `渠道 #${item.channel_id}`
                const channelRemark = row.channel
                  ? row.channel.channel_remark
                  : item.channel_remark
                const successRateClassName =
                  item.actual_sample_count > 0
                    ? getRateClassName(item.actual_success_rate)
                    : 'text-muted-foreground'
                const cacheRateClassName =
                  item.cache_sample_count > 0
                    ? 'text-foreground'
                    : 'text-muted-foreground'
                return (
                  <TableRow key={item.channel_id}>
                    <TableCell className='min-w-0 whitespace-normal'>
                      <div className='flex min-w-0 flex-col gap-1'>
                        <div className='flex min-w-0 flex-wrap items-center gap-2'>
                          <span className='min-w-0 font-medium break-words'>
                            {channelName}
                          </span>
                          {row.channel ? (
                            <ChannelMonitorStatusBadge
                              status={row.channel.status}
                              reason={row.channel.status_reason}
                              className='shrink-0'
                            />
                          ) : null}
                        </div>
                        <span className='text-muted-foreground text-xs'>
                          ID {item.channel_id}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className='max-w-72 whitespace-normal'>
                      {channelRemark ? (
                        <span
                          className='text-muted-foreground text-sm break-words'
                          title={channelRemark}
                        >
                          {channelRemark}
                        </span>
                      ) : (
                        <span className='text-muted-foreground'>-</span>
                      )}
                    </TableCell>
                    <TableCell className='text-right font-mono font-medium tabular-nums'>
                      {formatMonitorRatio(row.channel?.cost_ratio)}
                    </TableCell>
                    <TableCell className='text-right font-mono tabular-nums'>
                      {item.actual_sample_count}
                    </TableCell>
                    <TableCell
                      className={cn(
                        'text-right font-mono font-semibold tabular-nums',
                        successRateClassName
                      )}
                    >
                      {formatRate(
                        item.actual_success_rate,
                        item.actual_sample_count
                      )}
                    </TableCell>
                    <TableCell
                      className={cn(
                        'text-right font-mono font-semibold tabular-nums',
                        cacheRateClassName
                      )}
                    >
                      {formatRate(item.cache_hit_rate, item.cache_sample_count)}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      </div>
      <ChannelMonitorSuccessAPIKeyTable items={result.api_key_items ?? []} />
    </div>
  )
}

export function ChannelMonitorTodaySuccessDialog(
  props: ChannelMonitorTodaySuccessDialogProps
) {
  let description = '按北京时间统计真实调用结果'
  if (props.result?.generated_at) {
    description += ` · 更新于 ${formatTimestampToDate(props.result.generated_at)}`
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[85dvh] flex-col overflow-hidden sm:max-w-5xl'>
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>今日成功率与缓存率</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <ChannelMonitorTodaySuccessDialogContent
          result={props.result}
          channels={props.channels}
          isLoading={props.isLoading}
          isError={props.isError}
          isFetching={props.isFetching}
          onRetry={props.onRetry}
        />
      </DialogContent>
    </Dialog>
  )
}
