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
import { Analytics01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import {
  formatMonitorRuntimeCount,
  formatMonitorRuntimeTime,
  getChannelMonitorRuntimeStatus,
  type ChannelMonitorRuntimeInput,
} from '../lib/runtime-status'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorHistoryNotice } from './channel-monitor-history-notice'
import { ChannelMonitorRuntimeDetails } from './channel-monitor-runtime-details'

type ChannelMonitorRealtimeStatusProps = ChannelMonitorRuntimeInput & {
  className?: string
}

export function ChannelMonitorRealtimeStatus(
  props: ChannelMonitorRealtimeStatusProps
) {
  if (
    !props.metadata &&
    !props.recovery &&
    !props.recoveryLoading &&
    !props.recoveryFailed
  ) {
    return null
  }

  const metadata = props.metadata
  const recovery =
    props.recoveryFailed || props.recoveryLoading ? undefined : props.recovery
  const status = getChannelMonitorRuntimeStatus(props)
  const cutoff =
    metadata?.data_cutoff_at === 0
      ? '暂无已处理事件'
      : formatMonitorRuntimeTime(metadata?.data_cutoff_at)
  const metrics = [
    { label: '数据截至', value: cutoff },
    {
      label: '处理延迟',
      value: formatMonitorRuntimeCount(metadata?.consumer_lag_seconds, '秒'),
      warning: (metadata?.consumer_lag_seconds ?? 0) > 0,
    },
    {
      label: '事件待处理',
      value: formatMonitorRuntimeCount(status.pendingCount, '条'),
    },
    {
      label: '成本汇总',
      value: status.costLabel,
      warning: status.costUnavailable,
    },
  ]

  return (
    <div
      className={cn('flex w-full min-w-0 flex-col gap-3', props.className)}
      data-channel-monitor-realtime-status
      role='group'
      aria-label='运行状态摘要'
    >
      <div className='flex min-w-0 flex-wrap items-center justify-between gap-2'>
        <div
          className='flex min-w-0 flex-wrap items-center gap-2'
          role='status'
          aria-label='监控恢复状态'
        >
          <Badge
            variant={status.variant}
            className={cn(
              'h-auto min-h-5 max-w-full break-words whitespace-normal',
              status.healthy && 'text-success'
            )}
          >
            {status.label}
          </Badge>
          {recovery?.status !== 'healthy' &&
          (recovery?.pending_count ?? 0) > 0 ? (
            <span className='text-muted-foreground text-xs'>
              恢复待处理{' '}
              {formatMonitorRuntimeCount(recovery?.pending_count, '条')}
            </span>
          ) : null}
        </div>
        <Dialog>
          <DialogTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='xs'
                className='shrink-0'
              />
            }
          >
            <HugeiconsIcon icon={Analytics01Icon} data-icon='inline-start' />
            运行详情
          </DialogTrigger>
          <DialogContent
            className={channelMonitorDialogContentClassName(
              'flex w-[calc(100%-2rem)] flex-col gap-4 sm:max-w-5xl'
            )}
          >
            <DialogHeader className='min-w-0 shrink-0 gap-2 pr-8 text-left'>
              <DialogTitle>监控运行详情</DialogTitle>
              <DialogDescription className='flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs'>
                <span>
                  查询于 {formatMonitorRuntimeTime(metadata?.generated_at)}
                </span>
                <span>
                  检查于 {formatMonitorRuntimeTime(props.recovery?.checked_at)}
                </span>
                <span className='min-w-0 break-all'>
                  节点：{props.recovery?.node_id || '未提供'}
                </span>
              </DialogDescription>
            </DialogHeader>
            <div
              className='min-h-0 min-w-0 overflow-y-auto overscroll-contain border-t pt-4'
              role='region'
              aria-label='实时运行完整诊断'
            >
              <ChannelMonitorRuntimeDetails
                metadata={metadata}
                recovery={props.recovery}
                recoveryFailed={props.recoveryFailed}
                recoveryLoading={props.recoveryLoading}
                status={status}
              />
            </div>
          </DialogContent>
        </Dialog>
      </div>
      <dl className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 md:grid-cols-4'>
        {metrics.map((metric) => (
          <div
            key={metric.label}
            className='min-w-0'
            role='group'
            aria-label={metric.label}
          >
            <dt className='text-muted-foreground text-xs'>{metric.label}</dt>
            <dd
              className={cn(
                'mt-1 min-w-0 text-sm font-medium break-words tabular-nums',
                metric.warning && 'text-warning'
              )}
            >
              {metric.value}
            </dd>
            {metric.label === '成本汇总' &&
            (metadata?.cost_outbox_pending_count ?? 0) > 0 ? (
              <div className='text-muted-foreground mt-0.5 text-xs tabular-nums'>
                待记账{' '}
                {formatMonitorRuntimeCount(
                  metadata?.cost_outbox_pending_count,
                  '条'
                )}
              </div>
            ) : null}
          </div>
        ))}
      </dl>
      {status.alerts.length > 0 ? (
        <ul
          className='text-warning flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs font-normal'
          aria-label='监控异常提示'
        >
          {status.alerts.map((alert) => (
            <li key={alert} className='min-w-0 break-words'>
              {alert}
            </li>
          ))}
        </ul>
      ) : null}
      <ChannelMonitorHistoryNotice
        metadata={metadata}
        recovery={props.recovery}
        recoveryFailed={props.recoveryFailed}
        recoveryLoading={props.recoveryLoading}
        notices={status.historyNotices}
        healthy={status.healthy}
      />
    </div>
  )
}
