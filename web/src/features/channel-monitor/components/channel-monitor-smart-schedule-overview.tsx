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
  HistoryIcon,
  Refresh01Icon,
  Route01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorSmartScheduleOverviewSummary } from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRouteSnapshotStatus } from '../types'
import { ChannelMonitorSmartScheduleSnapshotStatus } from './channel-monitor-smart-schedule-snapshot-status'

type ChannelMonitorSmartScheduleOverviewProps = {
  enabled: boolean
  summary: ChannelMonitorSmartScheduleOverviewSummary
  snapshot?: ChannelMonitorSmartScheduleRouteSnapshotStatus
  refreshFailed: boolean
  stale: boolean
  runDisabled: boolean
  running: boolean
  onOpenHistory: () => void
  onOpenSettings: () => void
  onRun: () => void
}

export function ChannelMonitorSmartScheduleOverview(
  props: ChannelMonitorSmartScheduleOverviewProps
) {
  const routingAvailable =
    props.snapshot?.available === true &&
    !props.snapshot.stale &&
    !props.snapshot.protection_mode

  return (
    <section
      className='border-border bg-muted/15 flex min-w-0 flex-col gap-4 border-y px-4 py-3'
      aria-label='智能调度概览'
    >
      <header className='flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='flex min-w-0 flex-wrap items-center gap-2'>
          <HugeiconsIcon
            icon={Route01Icon}
            className='text-muted-foreground size-4 shrink-0'
            aria-hidden='true'
          />
          <h2 className='text-sm font-semibold'>调度概览</h2>
          <Badge variant={props.enabled ? 'secondary' : 'outline'}>
            {props.enabled ? '已启用' : '已禁用'}
          </Badge>
          {props.refreshFailed ? (
            <Badge variant='destructive'>刷新失败，显示上次结果</Badge>
          ) : null}
          {props.enabled && props.stale ? (
            <Badge variant='warning'>调度数据可能已过期</Badge>
          ) : null}
        </div>

        <div className='flex shrink-0 items-center gap-2 self-end sm:self-auto'>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  variant='outline'
                  size='icon-sm'
                  aria-label='智能调度记录'
                  onClick={props.onOpenHistory}
                />
              }
            >
              <HugeiconsIcon icon={HistoryIcon} aria-hidden='true' />
            </TooltipTrigger>
            <TooltipContent role='tooltip'>智能调度记录</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  variant='outline'
                  size='icon-sm'
                  aria-label='调度设置'
                  onClick={props.onOpenSettings}
                />
              }
            >
              <HugeiconsIcon icon={Settings02Icon} aria-hidden='true' />
            </TooltipTrigger>
            <TooltipContent role='tooltip'>调度设置</TooltipContent>
          </Tooltip>
          <Button
            type='button'
            size='sm'
            className='min-w-28'
            disabled={props.runDisabled || props.running || !props.enabled}
            aria-busy={props.running}
            onClick={props.onRun}
          >
            {props.running ? (
              <Spinner data-icon='inline-start' aria-hidden='true' />
            ) : (
              <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            )}
            立即调度
          </Button>
        </div>
      </header>

      {props.enabled ? (
        <>
          <dl className='grid min-w-0 grid-cols-2 gap-x-6 gap-y-4 lg:grid-cols-4'>
            <div className='min-w-0' role='group' aria-label='调度池'>
              <dt className='text-muted-foreground text-xs'>调度池</dt>
              <dd className='mt-1 text-xl font-semibold tabular-nums'>
                {props.summary.poolCount}
              </dd>
              <dd className='text-muted-foreground mt-1 text-xs'>
                {props.summary.groupCount} 个分组
              </dd>
            </div>
            <div className='min-w-0' role='group' aria-label='参与路由'>
              <dt className='text-muted-foreground text-xs'>参与路由</dt>
              <dd className='mt-1 text-xl font-semibold tabular-nums'>
                {props.summary.participatingCount}
                <span className='text-muted-foreground text-sm font-normal'>
                  /{props.summary.routeCount}
                </span>
              </dd>
              <dd className='text-muted-foreground mt-1 text-xs'>
                覆盖 {props.summary.channelCount} 个渠道
              </dd>
            </div>
            <div className='min-w-0' role='group' aria-label='当前可调度'>
              <dt className='text-muted-foreground text-xs'>当前可调度</dt>
              <dd className='mt-1 text-xl font-semibold tabular-nums'>
                {routingAvailable ? props.summary.activeCount : '未知'}
              </dd>
            </div>
            <div className='min-w-0' role='group' aria-label='最近执行'>
              <dt className='text-muted-foreground text-xs'>最近执行</dt>
              <dd className='mt-2 text-sm font-medium break-words tabular-nums'>
                {props.summary.lastScheduleTime > 0 ? (
                  <time
                    dateTime={new Date(
                      props.summary.lastScheduleTime * 1000
                    ).toISOString()}
                  >
                    {formatTimestampToDate(props.summary.lastScheduleTime)}
                  </time>
                ) : (
                  '暂无执行记录'
                )}
              </dd>
            </div>
          </dl>

          <div className='flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
            <span className='text-muted-foreground'>路由快照</span>
            <ChannelMonitorSmartScheduleSnapshotStatus
              snapshot={props.snapshot}
            />
          </div>
        </>
      ) : null}
    </section>
  )
}
