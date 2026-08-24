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
import { Alert02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelMonitorPerformanceMetricCoverage,
  ChannelMonitorRealtimeMetadata,
} from '../types'

type ChannelMonitorPerformanceCoverageAlertProps = {
  coverage?: ChannelMonitorPerformanceMetricCoverage
  metadata?: ChannelMonitorRealtimeMetadata
  rangeLabel: string
}

export function ChannelMonitorPerformanceCoverageAlert(
  props: ChannelMonitorPerformanceCoverageAlertProps
) {
  if (!props.coverage?.aggregation_enabled || props.coverage.window_complete) {
    return null
  }

  const coveredFrom =
    props.coverage.aggregated_from > 0
      ? formatTimestampToDate(props.coverage.aggregated_from)
      : '尚未建立'
  const coveredThrough =
    props.coverage.aggregated_through > 0
      ? formatTimestampToDate(props.coverage.aggregated_through)
      : '尚未建立'
  const reasons = props.metadata?.degraded_reasons ?? []
  const issueDescriptions: string[] = []

  for (const reason of reasons) {
    switch (reason) {
      case 'redis_unavailable':
        issueDescriptions.push(
          'Redis 不可用或状态检查失败，无法确认实时事件处理进度。'
        )
        break
      case 'consumer_stopped':
        issueDescriptions.push('事件处理服务已停止，当前没有处理实时事件。')
        break
      case 'consumer_group_missing':
        issueDescriptions.push('实时事件处理组尚未建立，事件无法进入分钟汇总。')
        break
      case 'event_backlog': {
        const pendingCount = props.metadata?.pending_count ?? 0
        const oldestPendingAt = props.metadata?.oldest_pending_at ?? 0
        const consumerLagSeconds = props.metadata?.consumer_lag_seconds ?? 0
        let description = '实时事件队列中还有未处理完成的事件'
        if (pendingCount > 0) {
          description += `，其中 ${pendingCount} 条已交付但尚未确认`
        }
        if (oldestPendingAt > 0) {
          description += `；最早一条产生于 ${formatTimestampToDate(oldestPendingAt)}`
        }
        if (consumerLagSeconds > 0) {
          description += `，当前延迟 ${consumerLagSeconds} 秒`
        }
        issueDescriptions.push(`${description}。`)
        break
      }
      case 'publisher_unavailable':
        issueDescriptions.push(
          '最近一次实时事件发布失败，且之后尚无成功发布记录。'
        )
        break
      case 'marker_release_failure':
        issueDescriptions.push('事件标记清理失败，后续重试可能受到影响。')
        break
      case 'stream_trim_failure':
        issueDescriptions.push('实时事件队列清理失败，实时统计仍处于异常状态。')
        break
      default:
        issueDescriptions.push(`未识别的实时链路降级原因：${reason}。`)
    }
  }

  if (reasons.length === 0) {
    const redisAvailable =
      props.metadata?.redis_available ??
      props.metadata?.redis_status !== 'unavailable'
    if (!redisAvailable) {
      issueDescriptions.push(
        'Redis 不可用或状态检查失败，无法确认实时事件处理进度。'
      )
    }
    if (props.metadata?.redis_consumer_running === false) {
      issueDescriptions.push('事件处理服务已停止，当前没有处理实时事件。')
    }
    if (
      (props.metadata?.pending_count ?? props.metadata?.queue_depth ?? 0) > 0 ||
      (props.metadata?.oldest_pending_at ?? 0) > 0
    ) {
      issueDescriptions.push(
        `实时事件队列中还有未处理完成的事件，当前延迟 ${props.metadata?.consumer_lag_seconds ?? 0} 秒。`
      )
    }
    if (props.metadata?.marker_release_failure_active) {
      issueDescriptions.push('事件标记清理失败，后续重试可能受到影响。')
    }
    if (props.metadata?.stream_trim_failure_active) {
      issueDescriptions.push('实时事件队列清理失败，实时统计仍处于异常状态。')
    }
  }

  if (issueDescriptions.length === 0) {
    issueDescriptions.push(
      '实时统计链路已被标记为降级，但接口未返回具体故障原因，请查看服务端日志。'
    )
  }

  return (
    <Alert>
      <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
      <AlertTitle>{props.rangeLabel}统计窗口数据尚未覆盖完整</AlertTitle>
      <AlertDescription className='flex flex-col gap-1.5'>
        <span>
          当前分钟汇总覆盖从 {coveredFrom} 到 {coveredThrough}
          ，当前请求数、成功率和性能数据可能偏低。
        </span>
        <span>具体原因：</span>
        <ul className='list-disc pl-5'>
          {issueDescriptions.map((description) => (
            <li key={description}>{description}</li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
