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
import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelMonitorRealtimeDegradedReason,
  ChannelMonitorRealtimeMetadata,
} from '../types'
import type { ChannelMonitorRecovery } from '../types-recovery'

export type ChannelMonitorRuntimeInput = {
  metadata?: ChannelMonitorRealtimeMetadata
  recovery?: ChannelMonitorRecovery
  recoveryLoading?: boolean
  recoveryFailed?: boolean
}

const degradedReasonLabels: Record<
  ChannelMonitorRealtimeDegradedReason,
  string
> = {
  redis_unavailable: 'Redis 故障',
  consumer_stopped: '事件处理已停止',
  consumer_group_missing: '事件处理组尚未就绪',
  event_backlog: '实时事件存在积压',
  publisher_unavailable: '事件发布不可用',
  marker_release_failure: '事件标记清理故障',
  stream_trim_failure: '实时事件清理故障',
  writer_queue_full: '监控写入队列已满',
  cost_stream_backlog: '成本事件存在积压',
  cost_outbox_backlog: '成本账本存在积压',
  cost_publish_failure: '成本事件发布失败',
  cost_dead_letter: '存在待复核的成本异常事件',
  cost_projection_unavailable: '成本汇总暂不可用',
  cost_projection_pending: '成本汇总更新中',
  daily_replay_incomplete: '日统计存在恢复缺口',
  redis_pool_congested: 'Redis 连接池拥塞',
  redis_pool_timeout: 'Redis 连接池等待超时',
  redis_context_deadline: 'Redis 请求超时',
}

export function getChannelMonitorRuntimeStatus(
  input: ChannelMonitorRuntimeInput
) {
  const metadata = input.metadata
  const recovery =
    input.recoveryFailed || input.recoveryLoading ? undefined : input.recovery
  const reasons = metadata?.degraded_reasons ?? []
  const historicalIncomplete =
    reasons.includes('daily_replay_incomplete') ||
    (recovery?.data_gap_reasons.length ?? 0) > 0 ||
    recovery?.recovery_status === 'data_incomplete'
  const quarantineOnly =
    recovery?.data_gap_reasons.length === 1 &&
    recovery.data_gap_reasons[0] === 'events_quarantined' &&
    !reasons.includes('daily_replay_incomplete')
  const runtimeConfirmedHealthy =
    recovery?.status === 'healthy' &&
    recovery.checked_at >= (metadata?.generated_at ?? recovery.checked_at) - 30
  const historicalCostEvents =
    runtimeConfirmedHealthy &&
    recovery?.cost_dead_letter_count !== undefined &&
    (metadata?.cost_dead_letter_count ?? 0) <= recovery.cost_dead_letter_count
  const historicalDrops =
    runtimeConfirmedHealthy &&
    recovery?.dropped_sample_count !== undefined &&
    (metadata?.writer_dropped_events ?? 0) <= recovery.dropped_sample_count
  const historicalPublishFailures =
    runtimeConfirmedHealthy &&
    recovery?.cost_publish_failed_count !== undefined &&
    (metadata?.cost_publish_failed_count ?? 0) <=
      recovery.cost_publish_failed_count
  const newQuarantine =
    recovery?.quarantine_count !== undefined &&
    (metadata?.quarantine_count ?? 0) > recovery.quarantine_count
  const runtimeReasons = reasons.filter(
    (reason) =>
      reason !== 'daily_replay_incomplete' &&
      !(reason === 'cost_dead_letter' && historicalCostEvents) &&
      !(reason === 'writer_queue_full' && historicalDrops) &&
      !(reason === 'cost_publish_failure' && historicalPublishFailures) &&
      !(
        runtimeConfirmedHealthy &&
        (reason === 'cost_projection_pending' ||
          ((reason === 'event_backlog' || reason === 'cost_stream_backlog') &&
            (metadata?.consumer_lag_seconds ?? 0) < 30))
      )
  )
  // An unexplained degradation must remain visible even after runtime recovery.
  const realtimeDegraded =
    metadata?.realtime_degraded === true &&
    (metadata.unexplained_realtime_degraded === true ||
      reasons.length === 0 ||
      runtimeReasons.length > 0)
  const redisAvailable =
    metadata?.redis_available ??
    (metadata?.redis_status === undefined
      ? undefined
      : metadata.redis_status === 'available')
  const consumerRunning = metadata?.redis_consumer_running
  const pendingCount = metadata?.pending_count ?? metadata?.queue_depth
  const costUnavailable =
    metadata?.cost_projection?.failed === true ||
    reasons.includes('cost_projection_unavailable')
  const costQueues = [
    metadata?.cost_queue_pending_count,
    metadata?.cost_stream_pending_count,
    metadata?.cost_stream_unread_count,
    metadata?.cost_outbox_pending_count,
  ]
  const costUpdating =
    metadata?.cost_projection?.pending === true ||
    reasons.includes('cost_projection_pending') ||
    costQueues.some((count) => count !== undefined && count > 0)
  let costLabel = '未提供'
  if (costUnavailable) costLabel = '暂不可用'
  else if (costUpdating) costLabel = '更新中'
  else if ((metadata?.cost_projection?.checked_at ?? 0) > 0) {
    costLabel = '已更新'
  } else if (costQueues.every((count) => count === 0)) costLabel = '无待处理'

  const alerts = new Set<string>()
  const historyNotices = new Set<string>()
  for (const reason of runtimeReasons) {
    if (reason === 'cost_projection_pending' && costUnavailable) continue
    alerts.add(degradedReasonLabels[reason] ?? `监控异常：${reason}`)
  }
  if (redisAvailable === false) alerts.add('Redis 故障')
  if (consumerRunning === false) alerts.add('事件处理已停止')
  if (realtimeDegraded) alerts.add('监控数据不完整')
  if (costUnavailable) alerts.add('成本汇总暂不可用')
  if (metadata?.marker_release_failure_active) alerts.add('事件标记清理故障')
  if (metadata?.stream_trim_failure_active) alerts.add('实时事件清理故障')
  if (newQuarantine) alerts.add('新增监控事件隔离')
  if ((metadata?.cost_dead_letter_count ?? 0) > 0 && !historicalCostEvents) {
    alerts.add(
      `成本异常事件 ${formatMonitorRuntimeCount(metadata?.cost_dead_letter_count, '条')}`
    )
  }
  if (historicalIncomplete) {
    historyNotices.add(
      quarantineOnly ? '存在历史隔离记录' : '部分历史统计不完整'
    )
  }
  if (recovery?.action) {
    if (recovery.status === 'healthy' && historicalIncomplete) {
      historyNotices.add(recovery.action)
    } else {
      alerts.add(recovery.action)
    }
  }
  if (recovery?.notification_error) alerts.add(recovery.notification_error)
  if (input.recoveryFailed) alerts.add('监控恢复状态获取失败')

  const critical =
    redisAvailable === false ||
    consumerRunning === false ||
    costUnavailable ||
    newQuarantine ||
    metadata?.marker_release_failure_active === true ||
    metadata?.stream_trim_failure_active === true ||
    ((metadata?.cost_dead_letter_count ?? 0) > 0 && !historicalCostEvents) ||
    runtimeReasons.some((reason) =>
      [
        'redis_unavailable',
        'consumer_stopped',
        'publisher_unavailable',
        'cost_publish_failure',
        'cost_dead_letter',
      ].includes(reason)
    )
  const incomplete = realtimeDegraded || runtimeReasons.length > 0
  let label = '监控状态未确认'
  let variant: 'outline' | 'warning' | 'destructive' = 'outline'
  let healthy = false
  if (
    recovery?.recovery_status === 'manual_required' ||
    recovery?.status === 'unavailable'
  ) {
    label = recovery.message || '需要人工处理'
    variant = 'destructive'
  } else if (critical) {
    label = '监控异常'
    variant = 'destructive'
  } else if (input.recoveryFailed) {
    label = '监控状态暂不可用'
    variant = 'warning'
  } else if (
    recovery?.status === 'degraded' ||
    recovery?.recovery_status === 'recovering'
  ) {
    label = recovery.message || '自动恢复中'
    variant = 'warning'
  } else if (incomplete) {
    label = '监控数据不完整'
    variant = 'warning'
  } else if (input.recoveryLoading) {
    label = '监控状态检查中'
  } else if (
    recovery?.status === 'healthy' ||
    (redisAvailable === true && consumerRunning === true)
  ) {
    label = recovery?.message || '监控正常'
    if (historicalIncomplete) label = '监控运行正常'
    healthy = true
  }

  return {
    label,
    variant,
    healthy,
    redisAvailable,
    consumerRunning,
    pendingCount,
    costLabel,
    costUnavailable,
    alerts: [...alerts],
    historyNotices: [...historyNotices],
  }
}

export function formatMonitorRuntimeCount(
  value: number | undefined,
  unit = ''
) {
  if (value === undefined || !Number.isFinite(value)) return '未提供'
  return `${value.toLocaleString('zh-CN')}${unit ? ` ${unit}` : ''}`
}

export function formatMonitorRuntimeTime(value: number | undefined) {
  if (value === undefined || !Number.isFinite(value)) return '未提供'
  return value > 0 ? formatTimestampToDate(value) : '暂无'
}
