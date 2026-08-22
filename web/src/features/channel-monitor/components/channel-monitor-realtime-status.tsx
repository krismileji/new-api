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
import { Badge } from '@/components/ui/badge'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ChannelMonitorRealtimeMetadata } from '../types'

type ChannelMonitorRealtimeStatusProps = {
  metadata: ChannelMonitorRealtimeMetadata | undefined
  className?: string
}

export function ChannelMonitorRealtimeStatus(
  props: ChannelMonitorRealtimeStatusProps
) {
  if (!props.metadata) return null

  const pendingCount =
    props.metadata.pending_count ?? props.metadata.queue_depth
  const costQueuePendingCount = props.metadata.cost_queue_pending_count
  const redisAvailable =
    props.metadata.redis_available ??
    props.metadata.redis_status !== 'unavailable'
  const consumerRunning = props.metadata.redis_consumer_running ?? true
  const cutoffLabel = props.metadata.data_cutoff_at
    ? formatTimestampToDate(props.metadata.data_cutoff_at)
    : '暂无已处理事件'
  const processedLabel = props.metadata.processed_at
    ? formatTimestampToDate(props.metadata.processed_at)
    : '暂无'
  const oldestPendingLabel = formatRealtimeTimestamp(
    props.metadata.oldest_pending_at
  )
  const publishedLabel = formatRealtimeTimestamp(
    props.metadata.last_published_at
  )
  const lastProcessedLabel = formatRealtimeTimestamp(
    props.metadata.last_processed_at
  )
  const lastQuarantinedLabel = formatRealtimeTimestamp(
    props.metadata.last_quarantined_at
  )
  const quarantineCount = props.metadata.quarantine_count ?? 0

  return (
    <span
      className={cn(
        'inline-flex min-w-0 max-w-full flex-wrap items-center gap-2',
        props.className
      )}
      data-channel-monitor-realtime-status
    >
      <Badge
        variant={redisAvailable ? 'outline' : 'destructive'}
        data-realtime-redis-status={
          redisAvailable ? 'available' : 'unavailable'
        }
      >
        Redis {redisAvailable ? '正常' : '故障'}
      </Badge>
      <Badge
        variant={consumerRunning ? 'outline' : 'destructive'}
        data-realtime-consumer-status={consumerRunning ? 'running' : 'stopped'}
      >
        消费者 {consumerRunning ? '运行中' : '已停止'}
      </Badge>
      {props.metadata.realtime_degraded ? (
        <Badge variant='destructive'>实时数据已降级</Badge>
      ) : null}
      {pendingCount > 0 ? (
        <Badge variant='warning'>Redis 待处理 {pendingCount}</Badge>
      ) : null}
      {costQueuePendingCount !== undefined ? (
        <Badge
          variant={costQueuePendingCount > 0 ? 'warning' : 'outline'}
          title='当前节点成本待写队列，按聚合条目计数'
        >
          成本待写队列 {costQueuePendingCount}
        </Badge>
      ) : null}
      {props.metadata.marker_release_failure_active ? (
        <Badge variant='destructive'>副作用标记释放故障</Badge>
      ) : null}
      {props.metadata.stream_trim_failure_active ? (
        <Badge variant='destructive'>Stream 裁剪故障</Badge>
      ) : null}
      <span
        className='text-muted-foreground max-w-full text-xs font-normal text-wrap'
        title={`处理于 ${processedLabel} · 事件水位 ${props.metadata.event_watermark}`}
      >
        数据截至 {cutoffLabel} · 最早未处理 {oldestPendingLabel} · 消费延迟{' '}
        {props.metadata.consumer_lag_seconds ?? 0} 秒 · 最后发布{' '}
        {publishedLabel} · 最后处理 {lastProcessedLabel} · 重试{' '}
        {props.metadata.retry_count ?? 0} 次 · 接管{' '}
        {props.metadata.takeover_count ?? 0} 次 · 隔离 {quarantineCount} 条
        {quarantineCount > 0 ? `（最近 ${lastQuarantinedLabel}）` : ''} ·
        标记释放失败 {props.metadata.marker_release_failure_count ?? 0} 次 ·
        Stream 裁剪失败 {props.metadata.stream_trim_failure_count ?? 0} 次
      </span>
    </span>
  )
}

function formatRealtimeTimestamp(value: number | undefined) {
  return value ? formatTimestampToDate(value) : '暂无'
}
