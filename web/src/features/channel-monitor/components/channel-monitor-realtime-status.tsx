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
  const costStreamPendingCount = props.metadata.cost_stream_pending_count
  const costStreamUnreadCount = props.metadata.cost_stream_unread_count
  const costOutboxPendingCount = props.metadata.cost_outbox_pending_count
  const writerQueueDepth = props.metadata.writer_queue_depth
  const writerQueueCapacity = props.metadata.writer_queue_capacity
  const redisAvailable =
    props.metadata.redis_available ??
    props.metadata.redis_status !== 'unavailable'
  const consumerRunning = props.metadata.redis_consumer_running ?? true
  const cutoffLabel = props.metadata.data_cutoff_at
    ? formatTimestampToDate(props.metadata.data_cutoff_at)
    : '暂无已处理事件'
  const generatedLabel = formatRealtimeTimestamp(props.metadata.generated_at)
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
  const costOutboxOldestPendingLabel = formatRealtimeTimestamp(
    props.metadata.cost_outbox_oldest_pending_at
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
        事件处理 {consumerRunning ? '运行中' : '已停止'}
      </Badge>
      {props.metadata.realtime_degraded ? (
        <Badge variant='destructive'>实时数据已降级</Badge>
      ) : null}
      {pendingCount > 0 ? (
        <Badge variant='warning'>实时事件待处理 {pendingCount}</Badge>
      ) : null}
      {writerQueueDepth !== undefined && writerQueueCapacity !== undefined ? (
        <Badge variant={writerQueueDepth > 0 ? 'warning' : 'outline'}>
          监控写入队列 {writerQueueDepth}/{writerQueueCapacity}
          {props.metadata.writer_queue_age_seconds ? (
            <>（{props.metadata.writer_queue_age_seconds} 秒）</>
          ) : null}
        </Badge>
      ) : null}
      {costQueuePendingCount !== undefined ? (
        <Badge
          variant={costQueuePendingCount > 0 ? 'warning' : 'outline'}
          title='当前节点已聚合、等待写入成本记录的条目数量'
        >
          已聚合成本待写入 {costQueuePendingCount}
        </Badge>
      ) : null}
      {costStreamPendingCount !== undefined &&
      costStreamUnreadCount !== undefined ? (
        <Badge
          variant={
            costStreamPendingCount > 0 || costStreamUnreadCount > 0
              ? 'warning'
              : 'outline'
          }
          title='成本事件处理队列：未读取是尚未开始处理的数量，待确认是已读取但尚未完成处理的数量'
        >
          成本事件未读取 {costStreamUnreadCount} / 待确认{' '}
          {costStreamPendingCount}
        </Badge>
      ) : null}
      {costOutboxPendingCount !== undefined ? (
        <Badge
          variant={costOutboxPendingCount > 0 ? 'warning' : 'outline'}
          title={`已排队等待写入成本账本、尚未完成记账的事件，最早待处理 ${costOutboxOldestPendingLabel}`}
        >
          待记入成本账本 {costOutboxPendingCount}
        </Badge>
      ) : null}
      {(props.metadata.cost_publish_failed_count ?? 0) > 0 ? (
        <Badge
          variant='destructive'
          title='成本事件未能进入可靠处理队列，需要检查事件发布链路'
        >
          成本事件排队失败
        </Badge>
      ) : null}
      {(props.metadata.cost_ledger_failed_count ?? 0) > 0 ? (
        <Badge
          variant='destructive'
          title='成本事件未能写入成本账本，需要检查数据库写入链路'
        >
          成本账本写入失败
        </Badge>
      ) : null}
      {(props.metadata.cost_dead_letter_count ?? 0) > 0 ? (
        <Badge
          variant='destructive'
          title='成本事件多次处理失败，已移入异常队列等待复核'
        >
          成本事件进入异常队列
        </Badge>
      ) : null}
      {props.metadata.marker_release_failure_active ? (
        <Badge variant='destructive'>事件标记清理故障</Badge>
      ) : null}
      {props.metadata.stream_trim_failure_active ? (
        <Badge variant='destructive'>实时事件清理故障</Badge>
      ) : null}
      <span
        className='text-muted-foreground max-w-full text-xs font-normal text-wrap'
        title={`处理于 ${processedLabel}`}
      >
        {props.metadata.generated_at ? `查询时间 ${generatedLabel}` : ''}
        {props.metadata.generated_at ? ' · ' : ''}数据截至 {cutoffLabel} ·
        已处理事件序号 {props.metadata.event_watermark} · 最早待处理{' '}
        {oldestPendingLabel} · 处理延迟{' '}
        {props.metadata.consumer_lag_seconds ?? 0} 秒 · 最近发布{' '}
        {publishedLabel} · 最近处理 {lastProcessedLabel} · 处理重试{' '}
        {props.metadata.retry_count ?? 0} 次 · 自动接管{' '}
        {props.metadata.takeover_count ?? 0} 次 · 异常隔离 {quarantineCount} 条
        {quarantineCount > 0 ? `（最近 ${lastQuarantinedLabel}）` : ''} ·
        事件标记清理失败 {props.metadata.marker_release_failure_count ?? 0} 次 ·
        实时事件清理失败 {props.metadata.stream_trim_failure_count ?? 0} 次 ·
        监控事件丢弃 {props.metadata.writer_dropped_events ?? 0} 次 ·
        监控写入重试 {props.metadata.writer_retry_events ?? 0} 次 ·
        最早待记账成本 {costOutboxOldestPendingLabel} · 成本账本写入重试{' '}
        {props.metadata.cost_outbox_retry_count ?? 0} 次 · 成本账本写入失败{' '}
        {props.metadata.cost_ledger_failed_count ?? 0} 次 · 成本事件排队失败{' '}
        {props.metadata.cost_publish_failed_count ?? 0} 次 · 成本异常事件{' '}
        {props.metadata.cost_dead_letter_count ?? 0} 条
      </span>
    </span>
  )
}

function formatRealtimeTimestamp(value: number | undefined) {
  return value ? formatTimestampToDate(value) : '暂无'
}
