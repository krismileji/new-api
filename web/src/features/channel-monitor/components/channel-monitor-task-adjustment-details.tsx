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
  HistoryIcon,
  InformationCircleIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { formatTimestampToDate } from '@/lib/format'

import { formatChannelMonitorSmartScheduleFailureStage } from '../lib/smart-schedule-execution'
import type { ChannelMonitorTask, ChannelMonitorTaskAdjustment } from '../types'
import { ChannelMonitorSmartScheduleScoreDetails } from './channel-monitor-smart-schedule-score-details'

const ACTION_LABELS: Record<ChannelMonitorTaskAdjustment['action'], string> = {
  updated: '已调整',
  unchanged: '保持',
  skipped: '已跳过',
  failed: '失败',
}

function AdjustmentActionBadge(props: {
  action: ChannelMonitorTaskAdjustment['action']
}) {
  if (props.action === 'failed') {
    return <Badge variant='destructive'>{ACTION_LABELS[props.action]}</Badge>
  }
  if (props.action === 'updated') {
    return <Badge>{ACTION_LABELS[props.action]}</Badge>
  }
  if (props.action === 'unchanged') {
    return <Badge variant='outline'>{ACTION_LABELS[props.action]}</Badge>
  }
  return <Badge variant='secondary'>{ACTION_LABELS[props.action]}</Badge>
}

function ScheduleFailuresWithoutAdjustments(props: {
  task: ChannelMonitorTask
}) {
  const failures = props.task.result?.failures ?? []
  return (
    <div className='flex flex-col gap-2'>
      <Alert>
        <HugeiconsIcon icon={InformationCircleIcon} />
        <AlertTitle>本次任务未记录逐路由调整明细</AlertTitle>
        <AlertDescription>
          任务在生成逐路由明细前结束，下面展示已记录的失败原因。
        </AlertDescription>
      </Alert>
      {failures.map((failure) => (
        <Alert
          key={`${failure.channel_id}-${failure.group}-${failure.model}`}
          variant='destructive'
        >
          <HugeiconsIcon icon={Alert02Icon} />
          <AlertTitle>
            {failure.channel_name
              ? `${failure.channel_name}（ID ${failure.channel_id}）`
              : `渠道 ID ${failure.channel_id}`}
          </AlertTitle>
          <AlertDescription className='flex flex-col gap-1 text-left break-all'>
            {failure.group || failure.model ? (
              <span>
                {failure.group || '-'} / {failure.model || '-'}
              </span>
            ) : null}
            {failure.failure_stage ? (
              <span>
                失败阶段：
                {formatChannelMonitorSmartScheduleFailureStage(
                  failure.failure_stage
                )}
              </span>
            ) : null}
            <span>{failure.error || '智能调度更新失败'}</span>
          </AlertDescription>
        </Alert>
      ))}
    </div>
  )
}

export function ChannelMonitorTaskAdjustmentDetails(props: {
  task: ChannelMonitorTask
  id: string
}) {
  const result = props.task.result
  const adjustments = result?.adjustments ?? []
  if (adjustments.length === 0 && (result?.failures?.length ?? 0) > 0) {
    return (
      <div id={props.id} role='region' aria-label='智能调度调整明细'>
        <ScheduleFailuresWithoutAdjustments task={props.task} />
      </div>
    )
  }
  if (adjustments.length === 0) {
    const noScheduledRoutes = result?.total === 0
    return (
      <div id={props.id} role='region' aria-label='智能调度调整明细'>
        <Empty className='min-h-32 border-0 py-6'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <HugeiconsIcon icon={HistoryIcon} />
            </EmptyMedia>
            <EmptyTitle>
              {noScheduledRoutes
                ? '本次没有符合调度范围的路由'
                : '本次任务未记录逐路由调整明细'}
            </EmptyTitle>
            <EmptyDescription>
              {noScheduledRoutes
                ? '请检查分组策略、模型范围和渠道参与状态。'
                : '任务可能在生成计划前结束，请结合任务状态排查。'}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    )
  }

  return (
    <div
      id={props.id}
      role='region'
      aria-label='智能调度调整明细'
      className='flex flex-col gap-2'
    >
      <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-xs'>
        <span>已记录 {adjustments.length} 条路由结果</span>
        <span>按结果、分组和模型排列</span>
      </div>
      <ol className='bg-background divide-y rounded-md border'>
        {adjustments.map((adjustment) => (
          <li
            key={`${adjustment.channel_id}-${adjustment.group}-${adjustment.model}`}
            className='grid min-w-0 gap-x-4 gap-y-2 p-3 lg:grid-cols-[minmax(12rem,1fr)_minmax(15rem,auto)_auto]'
          >
            <div className='min-w-0'>
              <p
                className='truncate font-medium'
                title={adjustment.channel_name || undefined}
              >
                {adjustment.channel_name || `渠道 ${adjustment.channel_id}`}
              </p>
              <p className='text-muted-foreground mt-0.5 text-xs break-all'>
                ID {adjustment.channel_id} · {adjustment.group} /{' '}
                {adjustment.model}
              </p>
            </div>
            <div className='flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs tabular-nums'>
              <span>
                <span className='text-muted-foreground'>优先级 </span>
                {adjustment.old_priority} →{' '}
                <strong>{adjustment.new_priority}</strong>
              </span>
              <span>
                <span className='text-muted-foreground'>权重 </span>
                {adjustment.old_weight} →{' '}
                <strong>{adjustment.new_weight}</strong>
              </span>
              {adjustment.score !== undefined && (
                <span>
                  <span className='text-muted-foreground'>得分 </span>
                  <strong>{adjustment.score.toFixed(4)}</strong>
                </span>
              )}
            </div>
            <div className='flex flex-wrap gap-1 lg:justify-self-end'>
              <AdjustmentActionBadge action={adjustment.action} />
              {adjustment.failure_stage ? (
                <Badge variant='destructive'>
                  失败阶段：
                  {formatChannelMonitorSmartScheduleFailureStage(
                    adjustment.failure_stage
                  )}
                </Badge>
              ) : null}
            </div>
            <p className='text-muted-foreground min-w-0 text-xs break-words lg:col-span-3'>
              <span className='text-foreground font-medium'>调整原因：</span>
              {adjustment.reason || '未记录调整原因'}
            </p>
            <p className='text-muted-foreground min-w-0 text-xs break-words lg:col-span-3'>
              <span className='text-foreground font-medium'>
                上一轮生效结果：
              </span>
              {(adjustment.previous_effective_time ?? 0) > 0
                ? `P${adjustment.previous_effective_priority ?? 0} / W${adjustment.previous_effective_weight ?? 0} · ${formatTimestampToDate(adjustment.previous_effective_time ?? 0)}`
                : '未记录已生效结果'}
              {adjustment.action === 'failed' &&
              (adjustment.previous_effective_time ?? 0) > 0
                ? '；本轮失败未覆盖，上一轮结果继续生效'
                : ''}
            </p>
            <ChannelMonitorSmartScheduleScoreDetails
              details={adjustment.score_details}
              className='lg:col-span-3'
              snapshotLabel='本次执行快照'
            />
          </li>
        ))}
      </ol>
      {result?.adjustment_details_truncated && (
        <Alert>
          <HugeiconsIcon icon={InformationCircleIcon} />
          <AlertTitle>调整明细已截断</AlertTitle>
          <AlertDescription>
            路由数量较多，仅优先保留失败和已调整等前 {adjustments.length}{' '}
            条结果，任务汇总计数不受影响。
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
