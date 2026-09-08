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
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
  const requestedFrom =
    props.coverage.window_start > 0
      ? formatTimestampToDate(props.coverage.window_start)
      : '未知'
  const requestedThrough =
    props.metadata?.generated_at && props.metadata.generated_at > 0
      ? formatTimestampToDate(props.metadata.generated_at)
      : '当前'
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
        issueDescriptions.push(
          '负责汇总实时事件的处理服务已停止，新的统计数据暂时不会更新。'
        )
        break
      case 'consumer_group_missing':
        issueDescriptions.push(
          '实时事件处理组尚未建立，事件还不能进入分钟汇总。'
        )
        break
      case 'event_backlog': {
        const pendingCount = props.metadata?.pending_count ?? 0
        const oldestPendingAt = props.metadata?.oldest_pending_at ?? 0
        const consumerLagSeconds = props.metadata?.consumer_lag_seconds ?? 0
        let description = '实时事件队列中还有事件没有处理完'
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
          '最近的实时事件没有成功发布，后续统计可能收不到新数据。'
        )
        break
      case 'marker_release_failure':
        issueDescriptions.push(
          '事件处理完成后的清理步骤失败，可能导致重试或统计延迟。'
        )
        break
      case 'stream_trim_failure':
        issueDescriptions.push(
          '实时事件队列清理失败，异常事件可能继续占用队列。'
        )
        break
      default:
        issueDescriptions.push(
          `系统返回了未分类的实时统计异常（${reason}），请查看服务端日志。`
        )
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
      issueDescriptions.push(
        '负责汇总实时事件的处理服务已停止，新的统计数据暂时不会更新。'
      )
    }
    if (
      (props.metadata?.pending_count ?? props.metadata?.queue_depth ?? 0) > 0 ||
      (props.metadata?.oldest_pending_at ?? 0) > 0
    ) {
      issueDescriptions.push(
        `实时事件队列中还有事件没有处理完，当前延迟 ${props.metadata?.consumer_lag_seconds ?? 0} 秒。`
      )
    }
    if (props.metadata?.marker_release_failure_active) {
      issueDescriptions.push(
        '事件处理完成后的清理步骤失败，可能导致重试或统计延迟。'
      )
    }
    if (props.metadata?.stream_trim_failure_active) {
      issueDescriptions.push(
        '实时事件队列清理失败，异常事件可能继续占用队列。'
      )
    }
  }

  if (issueDescriptions.length === 0) {
    issueDescriptions.push(
      '系统检测到实时统计链路异常，但没有返回更具体的原因。若几分钟后仍未恢复，请查看服务端日志。'
    )
  }

  return (
    <Alert>
      <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
      <AlertTitle>{props.rangeLabel}监控数据暂不完整</AlertTitle>
      <AlertDescription className='flex flex-col gap-1.5'>
        <span>请求数可能偏低，成功率和性能指标可能暂时不准确。</span>
        <Collapsible className='w-full min-w-0'>
          <CollapsibleTrigger render={<Button variant='ghost' size='xs' />}>
            查看统计详情
          </CollapsibleTrigger>
          <CollapsibleContent className='mt-2 flex min-w-0 flex-col gap-1.5'>
            <span>这段时间的监控数据还没有全部写入分钟汇总。</span>
            <div className='bg-muted/50 rounded-md px-3 py-2 text-xs leading-5'>
              <div>
                <span className='text-muted-foreground'>查询范围：</span>
                {requestedFrom} 至 {requestedThrough}
              </div>
              <div>
                <span className='text-muted-foreground'>已汇总范围：</span>
                {coveredFrom} 至 {coveredThrough}
              </div>
            </div>
            <span>这只影响监控页面的统计展示，不影响实际渠道请求。</span>
            <span className='font-medium'>可能原因：</span>
            <ul className='list-disc pl-5'>
              {issueDescriptions.map((description) => (
                <li key={description}>{description}</li>
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      </AlertDescription>
    </Alert>
  )
}
