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
  FingerPrintScanIcon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTimestampToDate } from '@/lib/format'

import {
  channelModelDetectionPresetLabel,
  channelModelDetectionRunStatusLabel,
  channelModelDetectionPresetSourceLabel,
  isChannelModelDetectionRunActive,
} from '../lib/model-detection'
import { getChannelModelDetectionRun } from '../lib/model-detection-channel-api'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import { CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS } from '../lib/query-options'
import type { ChannelModelDetectionRunStatus } from '../types-model-detection'
import { ChannelModelDetectionReport } from './channel-model-detection-report'

export type ChannelModelDetectionRunDetailSheetProps = {
  runId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onTerminal?: (runId: string, status: ChannelModelDetectionRunStatus) => void
}

export function ChannelModelDetectionRunDetailSheet(
  props: ChannelModelDetectionRunDetailSheetProps
) {
  const terminalNotificationRef = useRef('')
  const onTerminal = props.onTerminal
  const query = useQuery({
    queryKey: ['channel-monitor', 'model-detection', 'run', props.runId],
    queryFn: () => getChannelModelDetectionRun(props.runId ?? ''),
    enabled: props.open && Boolean(props.runId),
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
  const detail = query.data

  useEffect(() => {
    if (!detail || isChannelModelDetectionRunActive(detail.run.status)) return
    const terminalKey = `${detail.run.run_id}:${detail.run.status}:${detail.run.updated_at}`
    if (terminalNotificationRef.current === terminalKey) return
    terminalNotificationRef.current = terminalKey
    onTerminal?.(detail.run.run_id, detail.run.status)
  }, [detail, onTerminal])

  let content
  if (query.isPending && !detail) {
    content = (
      <div
        className='flex min-w-0 flex-col gap-4 p-4'
        aria-label='正在加载轮次详情'
      >
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-48 w-full' />
      </div>
    )
  } else if (query.isError && !detail) {
    content = (
      <Empty className='min-h-72 rounded-none'>
        <EmptyHeader>
          <EmptyTitle>轮次详情加载失败</EmptyTitle>
          <EmptyDescription>
            {channelModelDetectionRequestErrorMessage(query.error)}
          </EmptyDescription>
        </EmptyHeader>
        <Button
          type='button'
          variant='outline'
          onClick={() => void query.refetch()}
        >
          <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
          重试
        </Button>
      </Empty>
    )
  } else if (!detail) {
    content = (
      <Empty className='min-h-72 rounded-none'>
        <EmptyHeader>
          <EmptyTitle>暂无轮次详情</EmptyTitle>
          <EmptyDescription>选择历史轮次后查看检测报告</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    const run = detail.run
    const progressValue = run.progress.planned
      ? Math.min(
          100,
          (run.progress.logical_completed / run.progress.planned) * 100
        )
      : 0

    content = (
      <div
        className='flex min-w-0 flex-col gap-4 p-3 sm:p-4'
        data-slot='model-detection-run-detail'
        data-run-status={run.status}
      >
        <section className='min-w-0 rounded-lg border p-3 sm:p-4'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <Badge
              variant={
                isChannelModelDetectionRunActive(run.status)
                  ? 'warning'
                  : 'secondary'
              }
            >
              {channelModelDetectionRunStatusLabel(run.status)}
            </Badge>
            <span className='text-sm font-medium'>
              {channelModelDetectionPresetLabel(run.preset)} ·{' '}
              {channelModelDetectionPresetSourceLabel(run.preset_source)}
            </span>
          </div>
          <div className='text-muted-foreground mt-2 text-xs break-all'>
            轮次 {run.run_id}
          </div>
          <div className='text-muted-foreground mt-3 flex min-w-0 flex-wrap justify-between gap-2 text-xs tabular-nums'>
            <span>
              逻辑完成 {run.progress.logical_completed} / {run.progress.planned}
            </span>
            <span>
              HTTP 尝试 {run.progress.http_attempts} · 重试{' '}
              {run.progress.retries}
            </span>
          </div>
          <Progress
            className='mt-1.5'
            value={progressValue}
            aria-label={`轮次 ${run.run_id} 逻辑进度 ${run.progress.logical_completed} / ${run.progress.planned}`}
          />
          <dl className='mt-4 grid min-w-0 grid-cols-1 gap-3 text-xs sm:grid-cols-3'>
            <div>
              <dt className='text-muted-foreground'>排队时间</dt>
              <dd className='mt-0.5 tabular-nums'>
                {formatTimestampToDate(run.queued_at)}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>开始时间</dt>
              <dd className='mt-0.5 tabular-nums'>
                {formatTimestampToDate(run.started_at)}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>完成时间</dt>
              <dd className='mt-0.5 tabular-nums'>
                {formatTimestampToDate(run.finished_at)}
              </dd>
            </div>
          </dl>
        </section>

        {run.error_message ? (
          <Alert variant='destructive'>
            <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
            <AlertTitle>任务基础设施状态</AlertTitle>
            <AlertDescription>
              {run.error_code ? `${run.error_code}：` : ''}
              {run.error_message}
            </AlertDescription>
          </Alert>
        ) : null}

        <ChannelModelDetectionReport executions={detail.executions} />
      </div>
    )
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full max-w-full min-w-0 overflow-x-hidden sm:max-w-4xl'>
        <SheetHeader className='min-w-0 pr-12'>
          <div className='flex min-w-0 items-start justify-between gap-3'>
            <div className='min-w-0'>
              <SheetTitle className='flex min-w-0 items-center gap-2'>
                <HugeiconsIcon icon={FingerPrintScanIcon} aria-hidden='true' />
                <span className='truncate'>模型检测轮次详情</span>
              </SheetTitle>
              <SheetDescription className='truncate'>
                {props.runId ? `轮次 ${props.runId}` : '选择轮次查看报告'}
              </SheetDescription>
            </div>
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              className='shrink-0'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
              aria-label='刷新模型检测轮次详情'
              title='刷新'
            >
              <HugeiconsIcon
                icon={Refresh01Icon}
                className={query.isFetching ? 'animate-spin' : undefined}
              />
            </Button>
          </div>
        </SheetHeader>
        <div className='min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto'>
          {content}
        </div>
      </SheetContent>
    </Sheet>
  )
}
