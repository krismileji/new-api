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
import { DatabaseAddIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import {
  Card,
  CardAction,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import type {
  ChannelMonitorTodayCacheWriteItem,
  ChannelMonitorTodaySuccessResult,
} from '../types'

type ChannelMonitorTodayCacheWriteCardProps = {
  result: ChannelMonitorTodaySuccessResult | undefined
  isLoading: boolean
  isError: boolean
  onOpen: () => void
}

const EMPTY_CACHE_WRITE_ITEMS: ChannelMonitorTodayCacheWriteItem[] = []

export function ChannelMonitorTodayCacheWriteCard(
  props: ChannelMonitorTodayCacheWriteCardProps
) {
  const metricsAvailable = props.result?.cache_write_metrics_available ?? false
  const items = props.result?.cache_write_items ?? EMPTY_CACHE_WRITE_ITEMS
  const hasMetrics = !props.isLoading && !props.isError && metricsAvailable
  const channelCount = hasMetrics ? items.length : null
  const requestCount = hasMetrics
    ? items.reduce((total, item) => total + item.request_count, 0)
    : 0

  let description = '按北京时间统计缓存写入请求'
  if (props.isLoading) {
    description = '正在统计今日缓存写'
  } else if (props.isError) {
    description = '今日缓存写统计加载失败'
  } else if (!metricsAvailable) {
    description = '消费日志未开启'
  } else if (items.length === 0) {
    description = '今日暂无缓存写'
  } else {
    description = `${requestCount} 次写入请求 · 点击查看渠道`
  }

  const channelCountLabel = channelCount == null ? '-' : `${channelCount}`

  return (
    <Card
      size='sm'
      className='hover:bg-muted/50 focus-visible:ring-ring/50 h-full cursor-pointer transition-colors outline-none focus-visible:ring-3'
      role='button'
      tabIndex={0}
      onClick={props.onOpen}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          props.onOpen()
        }
      }}
      aria-label={`查看今日缓存写渠道明细，共 ${channelCountLabel} 个渠道，${requestCount} 次写入请求`}
    >
      <CardHeader>
        <CardDescription>今日缓存写渠道</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>
          {props.isLoading ? (
            <Skeleton className='h-7 w-16' />
          ) : (
            <span>{channelCountLabel}</span>
          )}
        </CardTitle>
        <CardAction>
          <span className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={DatabaseAddIcon} aria-hidden='true' />
          </span>
        </CardAction>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
    </Card>
  )
}
