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
  DatabaseAddIcon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
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

import { formatMonitorRatio } from '../lib/format'
import type {
  ChannelMonitorItem,
  ChannelMonitorTodaySuccessResult,
} from '../types'
import { ChannelMonitorStatusBadge } from './channel-monitor-status-badge'

type ChannelMonitorTodayCacheWriteChannelMetadata = Pick<
  ChannelMonitorItem,
  'id' | 'name' | 'status' | 'status_reason' | 'cost_ratio' | 'channel_remark'
>

export type ChannelMonitorTodayCacheWriteDialogContentProps = {
  result: ChannelMonitorTodaySuccessResult | undefined
  channels: readonly ChannelMonitorTodayCacheWriteChannelMetadata[]
  isLoading: boolean
  isError: boolean
  isFetching: boolean
  onRetry: () => void
  history?: ReactNode
  detailDate?: string
}

function TodayCacheWriteContentLayout(props: {
  history?: ReactNode
  children: ReactNode
}) {
  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1'>
      {props.history}
      {props.children}
    </div>
  )
}

export function ChannelMonitorTodayCacheWriteDialogContent(
  props: ChannelMonitorTodayCacheWriteDialogContentProps
) {
  const rows = useMemo(() => {
    const channelById = new Map(
      props.channels.map((channel) => [channel.id, channel])
    )

    return (props.result?.cache_write_items ?? [])
      .filter((item) => item.request_count > 0)
      .map((item) => ({
        item,
        channel: channelById.get(item.channel_id) ?? null,
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
          first.item.channel_name ||
          `渠道 #${first.item.channel_id}`
        const secondName =
          second.channel?.name ||
          second.item.channel_name ||
          `渠道 #${second.item.channel_id}`
        const nameOrder = firstName.localeCompare(secondName)
        return nameOrder !== 0
          ? nameOrder
          : first.item.channel_id - second.item.channel_id
      })
  }, [props.channels, props.result?.cache_write_items])
  const totalRequestCount = rows.reduce(
    (total, row) => total + row.item.request_count,
    0
  )

  if (props.isLoading) {
    return (
      <TodayCacheWriteContentLayout history={props.history}>
        <div className='flex flex-col gap-3'>
          <Skeleton className='h-20 w-full' />
          <Skeleton className='h-56 w-full' />
        </div>
      </TodayCacheWriteContentLayout>
    )
  }

  if (props.isError) {
    return (
      <TodayCacheWriteContentLayout history={props.history}>
        <Empty className='min-h-64 border-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={Alert02Icon} />
            </EmptyMedia>
            <EmptyTitle>缓存写统计加载失败</EmptyTitle>
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
      </TodayCacheWriteContentLayout>
    )
  }

  if (!props.result?.cache_write_metrics_available) {
    return (
      <TodayCacheWriteContentLayout history={props.history}>
        <Empty className='min-h-64 border-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={Alert02Icon} />
            </EmptyMedia>
            <EmptyTitle>缓存写统计不可用</EmptyTitle>
            <EmptyDescription>需要开启消费日志</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </TodayCacheWriteContentLayout>
    )
  }

  if (rows.length === 0) {
    return (
      <TodayCacheWriteContentLayout history={props.history}>
        <Empty className='min-h-64 border-0'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={DatabaseAddIcon} />
            </EmptyMedia>
            <EmptyTitle>{props.detailDate || '所选日期'}暂无缓存写</EmptyTitle>
            <EmptyDescription>
              所选日期尚未记录包含缓存写的请求
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </TodayCacheWriteContentLayout>
    )
  }

  return (
    <TodayCacheWriteContentLayout history={props.history}>
      <div className='grid shrink-0 grid-cols-2 divide-x rounded-lg border'>
        <div className='flex min-h-20 flex-col justify-center gap-1 px-4 py-3'>
          <span className='text-muted-foreground text-xs'>缓存写渠道</span>
          <span className='font-mono text-xl font-semibold tabular-nums'>
            {rows.length} 个
          </span>
        </div>
        <div className='flex min-h-20 flex-col justify-center gap-1 px-4 py-3'>
          <span className='text-muted-foreground text-xs'>写入请求数</span>
          <span className='font-mono text-xl font-semibold tabular-nums'>
            {totalRequestCount} 次
          </span>
        </div>
      </div>
      <div
        data-slot='today-cache-write-channel-details'
        className='flex shrink-0 flex-col gap-2'
      >
        <h3 className='font-medium'>渠道明细</h3>
        <div className='rounded-lg border'>
          <Table className='w-full table-fixed'>
            <TableHeader>
              <TableRow>
                <TableHead className='w-[32%] whitespace-normal'>
                  渠道
                </TableHead>
                <TableHead className='w-[38%] whitespace-normal'>
                  备注
                </TableHead>
                <TableHead className='w-[15%] text-right whitespace-normal'>
                  成本倍率
                </TableHead>
                <TableHead className='w-[15%] text-right whitespace-normal'>
                  写入请求数
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const channelName =
                  row.channel?.name ||
                  row.item.channel_name ||
                  `渠道 #${row.item.channel_id}`
                const channelRemark = row.channel
                  ? row.channel.channel_remark
                  : row.item.channel_remark
                return (
                  <TableRow key={row.item.channel_id}>
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
                          ID {row.item.channel_id}
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
                    <TableCell className='text-right font-mono font-semibold tabular-nums'>
                      {row.item.request_count}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      </div>
    </TodayCacheWriteContentLayout>
  )
}
