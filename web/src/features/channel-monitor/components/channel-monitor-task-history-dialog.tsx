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
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  CloudDownloadIcon,
  HistoryIcon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getChannelMonitorTasks, runChannelMonitorRatioUpdate } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
} from '../lib/query-options'
import {
  getLatestCompletedChannelMonitorTaskTime,
  isActiveChannelMonitorTask,
} from '../lib/task-status'
import type { ChannelMonitorTask, ChannelMonitorTaskStatus } from '../types'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorTaskAdjustmentDetails } from './channel-monitor-task-adjustment-details'

const TASK_PAGE_SIZE = 20

const STATUS_LABELS: Record<ChannelMonitorTaskStatus, string> = {
  pending: '待执行',
  running: '执行中',
  succeeded: '成功',
  failed: '失败',
}

type ChannelMonitorTaskHistoryDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function formatTaskDuration(task: ChannelMonitorTask) {
  if (isActiveChannelMonitorTask(task)) {
    return task.status === 'running' ? '执行中' : '-'
  }

  const seconds = Math.max(0, task.updated_at - task.created_at)
  if (seconds < 1) return '< 1 秒'
  if (seconds < 60) return `${seconds} 秒`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  if (minutes < 60) return `${minutes} 分 ${remainingSeconds} 秒`
  const hours = Math.floor(minutes / 60)
  return `${hours} 小时 ${minutes % 60} 分`
}

function ChannelTaskStatusBadge(props: { task: ChannelMonitorTask }) {
  const partiallyFailed =
    props.task.status === 'succeeded' &&
    ((props.task.result?.failed ?? 0) > 0 ||
      props.task.result?.email_status === 'failed')
  const label = partiallyFailed ? '部分失败' : STATUS_LABELS[props.task.status]
  let variant: 'destructive' | 'outline' | 'secondary' | 'warning' = 'outline'
  if (props.task.status === 'failed') variant = 'destructive'
  if (props.task.status === 'running' || partiallyFailed) variant = 'warning'
  if (props.task.status === 'succeeded' && !partiallyFailed) {
    variant = 'secondary'
  }

  return <Badge variant={variant}>{label}</Badge>
}

function FailureDot(props: { label: string }) {
  return (
    <span
      role='img'
      aria-label={props.label}
      title={props.label}
      className='bg-destructive size-2 shrink-0 rounded-full'
    />
  )
}

export function ChannelMonitorTaskRowDisclosure(props: {
  adjustmentCount: number
  truncated: boolean
  expanded: boolean
  controlsId: string
  onToggle: () => void
}) {
  let label = props.expanded ? '收起执行详情' : '查看执行详情'
  if (!props.expanded && props.adjustmentCount > 0) {
    label = props.truncated
      ? `查看执行详情，至少 ${props.adjustmentCount} 条`
      : `查看执行详情，共 ${props.adjustmentCount} 条`
  }

  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className='text-muted-foreground hover:text-foreground ml-auto'
      onClick={props.onToggle}
      aria-label={label}
      aria-expanded={props.expanded}
      aria-controls={props.controlsId}
    >
      {props.expanded ? '收起详情' : '查看详情'}
      <HugeiconsIcon
        icon={ArrowDown01Icon}
        className={cn('transition-transform', props.expanded && 'rotate-180')}
        data-icon='inline-end'
        aria-hidden='true'
      />
    </Button>
  )
}

function ChannelTaskProgress(props: { task: ChannelMonitorTask }) {
  const result = props.task.result
  if (result) {
    if (props.task.type === 'channel_smart_schedule') {
      return (
        <div className='flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs sm:min-w-52'>
          <span>
            更新 <strong>{result.updated}</strong>
          </span>
          <span>
            保持 <strong>{result.unchanged ?? 0}</strong>
          </span>
          <span>
            跳过 <strong>{result.skipped ?? 0}</strong>
          </span>
          <span
            className={cn(
              'inline-flex items-center gap-1.5',
              result.failed > 0 && 'text-destructive'
            )}
          >
            失败 <strong>{result.failed}</strong>
            {result.failed > 0 && <FailureDot label='智能调度更新失败' />}
          </span>
        </div>
      )
    }
    return (
      <div className='flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs sm:min-w-52'>
        <span>
          成功 <strong>{result.updated}</strong> / {result.total}
        </span>
        <span>
          变化 <strong>{result.changed ?? 0}</strong>
        </span>
        <span>
          余额更新 <strong>{result.balance_updated ?? 0}</strong>
        </span>
        {(result.balance_warnings ?? 0) > 0 && (
          <span className='text-destructive'>
            余额预警 <strong>{result.balance_warnings}</strong>
          </span>
        )}
        {(result.skipped ?? 0) > 0 && (
          <span>
            已跳过 <strong>{result.skipped}</strong>
          </span>
        )}
        <span
          className={cn(
            'inline-flex items-center gap-1.5',
            result.failed > 0 && 'text-destructive'
          )}
        >
          失败 <strong>{result.failed}</strong>
          {result.failed > 0 && <FailureDot label='上游更新失败' />}
        </span>
        {(result.retried ?? 0) > 0 && (
          <span>
            重试 <strong>{result.retried}</strong>
          </span>
        )}
        {(result.recovered_after_retry ?? 0) > 0 && (
          <span>
            重试恢复 <strong>{result.recovered_after_retry}</strong>
          </span>
        )}
        {result.email_status === 'sent' && <span>邮件 已发送</span>}
        {result.email_status === 'failed' && (
          <span className='text-destructive' title={result.email_error}>
            邮件 发送失败
          </span>
        )}
      </div>
    )
  }

  const state = props.task.state
  if (!state) return <span className='text-muted-foreground'>-</span>
  return (
    <span className='text-sm tabular-nums'>
      已处理 {state.processed} / {state.total}（{state.progress}%）
    </span>
  )
}

export function ChannelMonitorTaskPolicySummary(props: {
  task: ChannelMonitorTask
}) {
  const result = props.task.result
  if (!result) return <span className='text-muted-foreground'>-</span>
  if (props.task.type === 'channel_smart_schedule') {
    const groupPolicies = result.group_policies ?? []
    const groupNames = groupPolicies.map((policy) => policy.group).join('、')
    const policySummary =
      groupPolicies.length > 0
        ? `${groupPolicies.length} 个分组策略 · ${groupNames}`
        : '未记录分组策略'
    return (
      <div className='flex min-w-0 flex-col gap-1 text-xs sm:min-w-48'>
        <span className='max-w-80 truncate' title={policySummary}>
          {policySummary}
          {result.force_reset ? ' · 强制重算' : ''}
        </span>
        <span className='text-muted-foreground'>
          性能窗口 {result.performance_window_minutes ?? 0} 分钟 ·
          稳定性评分窗口 {result.stability_window_minutes ?? 0} 分钟
        </span>
      </div>
    )
  }
  return (
    <div className='flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs sm:min-w-44'>
      <span className='inline-flex items-center gap-1.5'>
        更新分组 {result.groups_updated ?? 0}
        {result.group_update_failed && <FailureDot label='分组更新失败' />}
      </span>
      <span>移出分组 {result.group_memberships_removed ?? 0}</span>
      <span>禁用渠道 {result.channels_disabled ?? 0}</span>
      <span>恢复渠道 {result.channels_enabled ?? 0}</span>
      <span>跳过分组 {result.groups_skipped ?? 0}</span>
    </div>
  )
}

export function ChannelMonitorTaskHistoryEntry(props: {
  task: ChannelMonitorTask
  expanded: boolean
  onToggleDetails: () => void
}) {
  const failures = props.task.result?.failures ?? []
  const detailsId = `channel-monitor-task-details-${props.task.task_id}`
  const canExpand =
    props.task.type === 'channel_smart_schedule'
      ? props.task.result !== null || Boolean(props.task.error)
      : failures.length > 0 || Boolean(props.task.error)
  const detailsExpanded = props.expanded && canExpand

  return (
    <>
      <TableRow
        data-expandable={canExpand ? 'true' : undefined}
        className={cn(
          'grid !h-auto grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-3 p-3 sm:table-row sm:!h-15 sm:p-0',
          canExpand &&
            'cursor-pointer focus-visible:outline-ring focus-visible:outline-2 focus-visible:outline-offset-[-2px]'
        )}
        tabIndex={canExpand ? 0 : undefined}
        aria-expanded={canExpand ? detailsExpanded : undefined}
        aria-controls={canExpand ? detailsId : undefined}
        onClick={
          canExpand
            ? (event) => {
                const target = event.target as EventTarget & {
                  closest?: (selectors: string) => Element | null
                }
                if (target.closest?.('button, a, input, select, textarea')) {
                  return
                }
                props.onToggleDetails()
              }
            : undefined
        }
        onKeyDown={
          canExpand
            ? (event) => {
                if (event.target !== event.currentTarget) return
                if (event.key !== 'Enter' && event.key !== ' ') return
                event.preventDefault()
                props.onToggleDetails()
              }
            : undefined
        }
      >
        <TableCell className='min-w-0 p-0 whitespace-nowrap sm:p-2'>
          <span className='block'>
            {formatTimestampToDate(props.task.created_at)}
          </span>
          <span className='text-muted-foreground mt-1 block text-xs sm:hidden'>
            耗时 {formatTaskDuration(props.task)}
          </span>
        </TableCell>
        <TableCell className='p-0 text-right sm:p-2 sm:text-left'>
          <ChannelTaskStatusBadge task={props.task} />
        </TableCell>
        <TableCell className='col-span-2 p-0 whitespace-normal sm:table-cell sm:p-2'>
          <span className='text-muted-foreground mb-1 block text-xs font-medium sm:hidden'>
            执行结果
          </span>
          <ChannelTaskProgress task={props.task} />
        </TableCell>
        <TableCell className='col-span-2 p-0 whitespace-normal sm:table-cell sm:p-2'>
          <span className='text-muted-foreground mb-1 block text-xs font-medium sm:hidden'>
            联动操作
          </span>
          <ChannelMonitorTaskPolicySummary task={props.task} />
        </TableCell>
        <TableCell className='hidden whitespace-nowrap sm:table-cell'>
          {formatTaskDuration(props.task)}
        </TableCell>
        <TableCell
          className={cn(
            'col-span-2 border-t p-0 pt-2 text-right sm:table-cell sm:border-0 sm:p-2',
            !canExpand && 'hidden sm:table-cell'
          )}
        >
          {canExpand ? (
            <ChannelMonitorTaskRowDisclosure
              adjustmentCount={
                props.task.type === 'channel_smart_schedule'
                  ? (props.task.result?.adjustments?.length ?? 0)
                  : failures.length
              }
              truncated={
                props.task.type === 'channel_smart_schedule'
                  ? false
                  : props.task.result?.failure_details_truncated === true
              }
              expanded={detailsExpanded}
              controlsId={detailsId}
              onToggle={props.onToggleDetails}
            />
          ) : (
            <span className='text-muted-foreground'>-</span>
          )}
        </TableCell>
      </TableRow>
      {detailsExpanded && (
        <TableRow className='bg-muted/20 hover:bg-muted/20 block !h-auto sm:table-row'>
          <TableCell
            colSpan={6}
            className='block p-3 whitespace-normal sm:table-cell'
          >
            {props.task.type === 'channel_smart_schedule' ? (
              <ChannelMonitorTaskAdjustmentDetails
                task={props.task}
                id={detailsId}
              />
            ) : (
              <div
                id={detailsId}
                role='region'
                aria-label='倍率与余额更新详情'
                className='flex flex-col gap-2'
              >
                {props.task.error && (
                  <Alert variant='destructive'>
                    <HugeiconsIcon icon={Alert02Icon} />
                    <AlertTitle>任务执行失败</AlertTitle>
                    <AlertDescription className='text-left break-all'>
                      {props.task.error}
                    </AlertDescription>
                  </Alert>
                )}
                {failures.map((failure) => (
                  <Alert key={failure.channel_id} variant='destructive'>
                    <HugeiconsIcon icon={Alert02Icon} />
                    <AlertTitle>
                      {failure.channel_name
                        ? `${failure.channel_name}（ID ${failure.channel_id}）`
                        : `渠道 ID ${failure.channel_id}`}
                    </AlertTitle>
                    <AlertDescription className='text-left break-all'>
                      {failure.error || '上游倍率获取失败'}
                    </AlertDescription>
                  </Alert>
                ))}
                {props.task.result?.failure_details_truncated && (
                  <p className='text-muted-foreground text-xs'>
                    失败渠道较多，仅显示前 {failures.length} 条明细
                  </p>
                )}
              </div>
            )}
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

export function ChannelMonitorTaskHistoryDialog(
  props: ChannelMonitorTaskHistoryDialogProps
) {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [expandedTaskId, setExpandedTaskId] = useState<string | null>(null)
  const ratioUpdateMutation = useMutation({
    mutationFn: runChannelMonitorRatioUpdate,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.created
          ? '倍率更新任务已创建'
          : '已有倍率更新任务正在执行'
      )
      setPage(1)
      setExpandedTaskId(null)
    },
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })
  const query = useQuery({
    queryKey: [
      ...CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      'ratio',
      page,
      TASK_PAGE_SIZE,
    ],
    queryFn: () => getChannelMonitorTasks(page, TASK_PAGE_SIZE, 'ratio'),
    enabled: props.open,
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
  const tasks = query.data?.data.items ?? []
  const total = query.data?.data.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / TASK_PAGE_SIZE))
  const rangeStart = total === 0 ? 0 : (page - 1) * TASK_PAGE_SIZE + 1
  const rangeEnd = Math.min(page * TASK_PAGE_SIZE, total)
  const latestCompletedTaskTime =
    getLatestCompletedChannelMonitorTaskTime(tasks)
  const activeCount = tasks.filter(isActiveChannelMonitorTask).length
  const failedCount = tasks.filter((task) => task.status === 'failed').length
  const partialFailureCount = tasks.filter(
    (task) =>
      task.status === 'succeeded' &&
      ((task.result?.failed ?? 0) > 0 || task.result?.email_status === 'failed')
  ).length
  const succeededCount = tasks.filter(
    (task) =>
      task.status === 'succeeded' &&
      (task.result?.failed ?? 0) === 0 &&
      task.result?.email_status !== 'failed'
  ).length

  useEffect(() => {
    if (latestCompletedTaskTime <= 0) return
    queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    queryClient.invalidateQueries({ queryKey: ['channels'] })
  }, [latestCompletedTaskTime, queryClient])

  useEffect(() => {
    if (page <= totalPages) return
    setPage(totalPages)
    setExpandedTaskId(null)
  }, [page, totalPages])

  const changePage = (nextPage: number) => {
    setExpandedTaskId(null)
    setPage(nextPage)
  }

  let content: ReactNode
  if (query.isLoading) {
    content = (
      <div className='flex h-full flex-col gap-3 p-4'>
        {['first', 'second', 'third', 'fourth'].map((key) => (
          <Skeleton key={key} className='h-14 w-full' />
        ))}
      </div>
    )
  } else if (query.isError && tasks.length === 0) {
    content = (
      <Empty className='h-full min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} />
          </EmptyMedia>
          <EmptyTitle>倍率与余额记录加载失败</EmptyTitle>
          <EmptyDescription>
            {query.error instanceof Error ? query.error.message : '请稍后重试'}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant='outline' size='sm' onClick={() => query.refetch()}>
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重试
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else if (tasks.length === 0) {
    content = (
      <Empty className='h-full min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={HistoryIcon} />
          </EmptyMedia>
          <EmptyTitle>暂无倍率与余额更新记录</EmptyTitle>
          <EmptyDescription>
            开启自动更新或手动执行后，任务会在这里留下记录。
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <Table className='block min-w-0 sm:table sm:min-w-[920px]'>
        <TableHeader className='hidden sm:table-header-group'>
          <TableRow>
            <TableHead>执行时间</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>执行结果</TableHead>
            <TableHead>联动操作</TableHead>
            <TableHead>耗时</TableHead>
            <TableHead className='text-right'>详情</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody className='block sm:table-row-group'>
          {tasks.map((task) => (
            <ChannelMonitorTaskHistoryEntry
              key={task.task_id}
              task={task}
              expanded={expandedTaskId === task.task_id}
              onToggleDetails={() =>
                setExpandedTaskId((current) =>
                  current === task.task_id ? null : task.task_id
                )
              }
            />
          ))}
        </TableBody>
      </Table>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && ratioUpdateMutation.isPending) return
        props.onOpenChange(open)
      }}
    >
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'grid-rows-[auto_minmax(0,1fr)] sm:max-w-6xl'
        )}
        showCloseButton={!ratioUpdateMutation.isPending}
      >
        <DialogHeader className='pr-10'>
          <DialogTitle>倍率与余额更新记录</DialogTitle>
          <DialogDescription>
            查看每次上游倍率与余额更新的结果、联动操作、失败原因和耗时。
          </DialogDescription>
        </DialogHeader>
        <div className='grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] gap-3 overflow-hidden'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <span className='text-muted-foreground text-xs tabular-nums'>
              共 {total} 条更新任务
            </span>
            <Button
              variant='outline'
              size='sm'
              onClick={() => ratioUpdateMutation.mutate()}
              disabled={ratioUpdateMutation.isPending}
            >
              {ratioUpdateMutation.isPending ? (
                <Spinner />
              ) : (
                <HugeiconsIcon
                  icon={CloudDownloadIcon}
                  data-icon='inline-start'
                />
              )}
              立即更新倍率和余额
            </Button>
          </div>
          <div
            className='bg-border grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-4'
            aria-label='当前页执行概览'
          >
            <div className='bg-background p-3'>
              <span className='text-muted-foreground block text-xs'>
                当前页
              </span>
              <strong className='mt-1 block text-lg tabular-nums'>
                {tasks.length} 条
              </strong>
            </div>
            <div className='bg-background p-3'>
              <span className='text-muted-foreground block text-xs'>成功</span>
              <strong className='mt-1 block text-lg tabular-nums'>
                {succeededCount} 条
              </strong>
            </div>
            <div className='bg-background p-3'>
              <span className='text-muted-foreground block text-xs'>
                需关注
              </span>
              <strong
                className={cn(
                  'mt-1 block text-lg tabular-nums',
                  partialFailureCount + failedCount > 0 && 'text-destructive'
                )}
              >
                {partialFailureCount + failedCount} 条
              </strong>
            </div>
            <div className='bg-background p-3'>
              <span className='text-muted-foreground block text-xs'>
                执行中
              </span>
              <strong className='mt-1 block text-lg tabular-nums'>
                {activeCount} 条
              </strong>
            </div>
          </div>
          <div
            className='min-h-0 min-w-0 overflow-auto rounded-lg border'
            aria-busy={query.isFetching}
          >
            {query.isError && tasks.length > 0 ? (
              <Alert variant='destructive' className='m-3'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>更新记录刷新失败</AlertTitle>
                <AlertDescription>
                  当前显示上一次成功加载的记录，可点击下方刷新重试。
                </AlertDescription>
              </Alert>
            ) : null}
            {content}
          </div>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-muted-foreground text-xs tabular-nums'>
              显示 {rangeStart}-{rangeEnd}，共 {total} 条
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='icon-sm'
                aria-label='上一页'
                title='上一页'
                onClick={() => changePage(Math.max(1, page - 1))}
                disabled={page <= 1 || query.isFetching}
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} />
              </Button>
              <span className='text-muted-foreground min-w-20 text-center text-xs tabular-nums'>
                第 {page} / {totalPages} 页
              </span>
              <Button
                variant='outline'
                size='icon-sm'
                aria-label='下一页'
                title='下一页'
                onClick={() => changePage(Math.min(totalPages, page + 1))}
                disabled={page >= totalPages || query.isFetching}
              >
                <HugeiconsIcon icon={ArrowRight01Icon} />
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={() => query.refetch()}
                disabled={query.isFetching}
              >
                <HugeiconsIcon
                  icon={Refresh01Icon}
                  className={cn(query.isFetching && 'animate-spin')}
                  data-icon='inline-start'
                />
                刷新
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
