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
  Activity01Icon,
  Alert02Icon,
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  CloudDownloadIcon,
  Coins01Icon,
  Exchange01Icon,
  HistoryIcon,
  InformationCircleIcon,
  Link01Icon,
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
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getChannelMonitorTaskDetails,
  getChannelMonitorTasks,
  runChannelMonitorRatioUpdate,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
} from '../lib/query-options'
import {
  getLatestCompletedChannelMonitorTaskTime,
  isActiveChannelMonitorTask,
} from '../lib/task-status'
import type {
  ChannelMonitorTask,
  ChannelMonitorTaskBalanceUpdate,
  ChannelMonitorTaskFailure,
  ChannelMonitorTaskRatioChange,
  ChannelMonitorTaskSkippedChannel,
  ChannelMonitorTaskStatus,
} from '../types'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorTaskAdjustmentDetails } from './channel-monitor-task-adjustment-details'

const TASK_PAGE_SIZE = 20
type RatioTaskDetailTab = 'changed' | 'balance' | 'failures' | 'skipped'

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

function isTaskPartiallyFailed(task: ChannelMonitorTask) {
  return (
    task.status === 'succeeded' &&
    ((task.result?.failed ?? 0) > 0 || task.result?.email_status === 'failed')
  )
}

function ChannelTaskStatusBadge(props: { task: ChannelMonitorTask }) {
  const partiallyFailed = isTaskPartiallyFailed(props.task)
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

function getRatioTaskImpact(task: ChannelMonitorTask) {
  const result = task.result
  return {
    total: result?.total ?? task.state?.total ?? 0,
    changed: result?.changed ?? 0,
    balanceUpdated: result?.balance_updated ?? 0,
    balanceWarnings: result?.balance_warnings ?? 0,
    failed: result?.failed ?? 0,
    linkedActions:
      (result?.groups_updated ?? 0) +
      (result?.group_memberships_removed ?? 0) +
      (result?.channels_disabled ?? 0) +
      (result?.channels_enabled ?? 0),
  }
}

function getRatioTaskDetailCount(task: ChannelMonitorTask) {
  const result = task.result
  return (
    (result?.changed_channels?.length ?? 0) +
    (result?.balance_updates?.length ?? 0) +
    (result?.failures?.length ?? 0) +
    (result?.skipped_channels?.length ?? 0)
  )
}

function hasPersistedRatioTaskDetails(task: ChannelMonitorTask) {
  const result = task.result
  return (
    (result?.changed_channels?.length ?? 0) > 0 ||
    (result?.balance_updates?.length ?? 0) > 0 ||
    (result?.failures?.length ?? 0) > 0 ||
    (result?.skipped_channels?.length ?? 0) > 0
  )
}

function hasAggregateOnlyRatioTaskDetails(task: ChannelMonitorTask) {
  const result = task.result
  return Boolean(
    result &&
    !hasPersistedRatioTaskDetails(task) &&
    ((result.changed ?? 0) > 0 ||
      (result.balance_updated ?? 0) > 0 ||
      (result.failed ?? 0) > 0 ||
      (result.skipped ?? 0) > 0)
  )
}

function getFirstRatioTaskDetailTab(
  changedCount: number,
  balanceCount: number,
  failureCount: number
): RatioTaskDetailTab {
  if (changedCount > 0) return 'changed'
  if (balanceCount > 0) return 'balance'
  if (failureCount > 0) return 'failures'
  return 'skipped'
}

function hasRatioTaskDetailTruncation(task: ChannelMonitorTask) {
  const result = task.result
  return Boolean(
    result?.changed_details_truncated ||
    result?.balance_details_truncated ||
    result?.failure_details_truncated ||
    result?.skipped_details_truncated
  )
}

function getRatioTaskHeadline(task: ChannelMonitorTask) {
  if (task.status === 'pending') return '任务已创建，正在等待执行。'
  if (task.status === 'running') {
    if (!task.state) return '正在拉取上游倍率与余额。'
    return `正在处理 ${task.state.processed} / ${task.state.total} 个渠道。`
  }
  if (task.status === 'failed') {
    return task.result
      ? `已处理 ${task.result.updated} / ${task.result.total} 个渠道，任务未完整完成。`
      : '任务执行失败，未生成完整的更新结果。'
  }
  if (!task.result) return '任务已完成，但没有记录结果摘要。'

  const changed = task.result.changed ?? 0
  const balanceUpdated = task.result.balance_updated ?? 0
  const failed = task.result.failed ?? 0
  const summary =
    changed > 0
      ? `检查 ${task.result.total} 个渠道，${changed} 个倍率发生变化`
      : `检查 ${task.result.total} 个渠道，倍率无需调整`
  const balanceSummary =
    balanceUpdated > 0 ? `，刷新 ${balanceUpdated} 个渠道余额` : ''
  const failureSummary = failed > 0 ? `，${failed} 个渠道失败` : ''
  return `${summary}${balanceSummary}${failureSummary}。`
}

function RatioTaskMetric(props: {
  label: string
  value: number | string
  tone?: 'default' | 'success' | 'warning' | 'destructive'
}) {
  return (
    <div className='bg-muted/35 rounded-md px-3 py-2.5'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd
        className={cn(
          'mt-1 text-lg font-semibold tabular-nums',
          props.tone === 'success' && 'text-success',
          props.tone === 'warning' && 'text-warning',
          props.tone === 'destructive' && 'text-destructive'
        )}
      >
        {props.value}
      </dd>
    </div>
  )
}

function formatRatio(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '-'
  return value.toLocaleString(undefined, {
    maximumFractionDigits: 12,
    useGrouping: false,
  })
}

function formatBalance(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '首次记录'
  return formatCurrencyFromUSD(value, {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })
}

function formatChannelName(channel: {
  channel_id: number
  channel_name?: string
}) {
  return channel.channel_name || `渠道 ${channel.channel_id}`
}

function ChannelIdentity(props: {
  channel: {
    channel_id: number
    channel_name?: string
    channel_remark?: string
  }
}) {
  return (
    <div className='min-w-0'>
      <p className='truncate font-medium' title={props.channel.channel_name}>
        {formatChannelName(props.channel)}
      </p>
      <p className='text-muted-foreground mt-0.5 truncate text-xs'>
        ID {props.channel.channel_id}
        {props.channel.channel_remark
          ? ` · ${props.channel.channel_remark}`
          : ''}
      </p>
    </div>
  )
}

function RatioTaskDetailEmpty(props: { label: string }) {
  return (
    <div className='text-muted-foreground flex min-h-24 items-center justify-center px-4 text-sm'>
      本次任务没有{props.label}明细
    </div>
  )
}

function RatioTaskTruncationNote(props: { visible: number; total: number }) {
  if (props.total <= props.visible) return null
  return (
    <p className='text-muted-foreground px-1 text-xs'>
      仅显示前 {props.visible} 条，共 {props.total} 条；完整数量仍计入任务汇总。
    </p>
  )
}

function ChangedChannelsTable(props: {
  items: ChannelMonitorTaskRatioChange[]
}) {
  if (props.items.length === 0) return <RatioTaskDetailEmpty label='倍率变化' />
  return (
    <div className='overflow-hidden rounded-md border'>
      <Table className='min-w-[620px]'>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>上游倍率</TableHead>
            <TableHead>成本倍率</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.items.map((item) => {
            const percent =
              item.old_ratio === 0
                ? null
                : ((item.new_ratio - item.old_ratio) / item.old_ratio) * 100
            return (
              <TableRow key={item.channel_id}>
                <TableCell className='max-w-64'>
                  <ChannelIdentity channel={item} />
                </TableCell>
                <TableCell>
                  <div className='flex items-center gap-2 font-mono'>
                    <span>{formatRatio(item.old_ratio)}</span>
                    <span className='text-muted-foreground'>→</span>
                    <strong>{formatRatio(item.new_ratio)}</strong>
                    <span
                      className={cn(
                        'text-xs',
                        percent != null && percent > 0
                          ? 'text-warning'
                          : 'text-success'
                      )}
                    >
                      {percent == null
                        ? '-'
                        : `${percent > 0 ? '+' : ''}${percent.toFixed(2)}%`}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex items-center gap-2 font-mono'>
                    <span>{formatRatio(item.old_cost_ratio)}</span>
                    <span className='text-muted-foreground'>→</span>
                    <strong>{formatRatio(item.new_cost_ratio)}</strong>
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function BalanceUpdatesTable(props: {
  items: ChannelMonitorTaskBalanceUpdate[]
}) {
  if (props.items.length === 0) return <RatioTaskDetailEmpty label='余额刷新' />
  return (
    <div className='overflow-hidden rounded-md border'>
      <Table className='min-w-[620px]'>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>余额变化</TableHead>
            <TableHead>状态</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.items.map((item) => (
            <TableRow key={item.channel_id}>
              <TableCell className='max-w-64'>
                <ChannelIdentity channel={item} />
              </TableCell>
              <TableCell>
                <div className='flex items-center gap-2 font-mono'>
                  <span>{formatBalance(item.previous_balance)}</span>
                  <span className='text-muted-foreground'>→</span>
                  <strong>{formatBalance(item.balance)}</strong>
                </div>
              </TableCell>
              <TableCell>
                {item.warning ? (
                  <Badge variant='warning'>
                    低于预警值 {formatBalance(item.warning_threshold)}
                  </Badge>
                ) : (
                  <span className='text-muted-foreground text-xs'>正常</span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function failureKindLabel(kind: string | undefined) {
  if (kind === 'balance') return '余额'
  if (kind === 'ratio') return '倍率'
  return '同步'
}

function RatioTaskFailuresTable(props: { items: ChannelMonitorTaskFailure[] }) {
  if (props.items.length === 0) return <RatioTaskDetailEmpty label='上游失败' />
  return (
    <div className='overflow-hidden rounded-md border'>
      <Table className='min-w-[620px]'>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>失败类型</TableHead>
            <TableHead>失败原因</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.items.map((item) => (
            <TableRow key={`${item.channel_id}-${item.kind}-${item.error}`}>
              <TableCell className='max-w-64'>
                <ChannelIdentity channel={item} />
              </TableCell>
              <TableCell>
                <Badge variant='destructive'>
                  {failureKindLabel(item.kind)}
                </Badge>
              </TableCell>
              <TableCell className='max-w-[28rem] break-words whitespace-normal'>
                {item.error || '上游同步失败'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function SkippedChannelsTable(props: {
  items: ChannelMonitorTaskSkippedChannel[]
}) {
  if (props.items.length === 0) return <RatioTaskDetailEmpty label='跳过渠道' />
  return (
    <div className='overflow-hidden rounded-md border'>
      <Table className='min-w-[560px]'>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>跳过原因</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.items.map((item) => (
            <TableRow key={item.channel_id}>
              <TableCell className='max-w-64'>
                <ChannelIdentity channel={item} />
              </TableCell>
              <TableCell className='whitespace-normal'>{item.reason}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ChannelMonitorRatioTaskFailureDetails(props: {
  task: ChannelMonitorTask
  id: string
}) {
  const detailQuery = useQuery({
    queryKey: [
      ...CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      'details',
      props.task.task_id,
    ],
    queryFn: () => getChannelMonitorTaskDetails(props.task.task_id),
    enabled:
      props.task.result === null && !isActiveChannelMonitorTask(props.task),
    staleTime: 60_000,
  })
  const result = detailQuery.data?.data ?? props.task.result
  const failures = result?.failures ?? []
  const changedChannels = result?.changed_channels ?? []
  const balanceUpdates = result?.balance_updates ?? []
  const skippedChannels = result?.skipped_channels ?? []
  const hasPersistedDetails =
    changedChannels.length > 0 ||
    balanceUpdates.length > 0 ||
    failures.length > 0 ||
    skippedChannels.length > 0
  const hasAggregateOnlyDetails = Boolean(
    result &&
    !hasPersistedDetails &&
    ((result.changed ?? 0) > 0 ||
      (result.balance_updated ?? 0) > 0 ||
      (result.failed ?? 0) > 0 ||
      (result.skipped ?? 0) > 0)
  )
  const firstTab = getFirstRatioTaskDetailTab(
    changedChannels.length,
    balanceUpdates.length,
    failures.length
  )
  const [activeTab, setActiveTab] = useState<RatioTaskDetailTab | null>(null)
  const selectedTab = activeTab ?? firstTab

  let detailContent: ReactNode = null
  if (detailQuery.isLoading) {
    detailContent = (
      <div className='text-muted-foreground flex min-h-24 items-center justify-center gap-2 text-sm'>
        <Spinner aria-label='更新明细加载中' />
        正在加载渠道明细
      </div>
    )
  } else if (detailQuery.isError) {
    detailContent = (
      <Alert variant='destructive'>
        <HugeiconsIcon icon={Alert02Icon} />
        <AlertTitle>渠道明细加载失败</AlertTitle>
        <AlertDescription>
          {detailQuery.error instanceof Error
            ? detailQuery.error.message
            : '请稍后重试'}
        </AlertDescription>
      </Alert>
    )
  } else if (result && hasPersistedDetails) {
    detailContent = (
      <Tabs
        value={selectedTab}
        onValueChange={(value) => setActiveTab(value as RatioTaskDetailTab)}
        className='min-h-0'
      >
        <TabsList className='no-scrollbar bg-muted/30 flex h-auto w-full flex-nowrap justify-start overflow-x-auto rounded-md border p-1'>
          <TabsTrigger value='changed'>
            倍率变化 {result.changed ?? 0}
          </TabsTrigger>
          <TabsTrigger value='balance'>
            余额刷新 {result.balance_updated ?? 0}
          </TabsTrigger>
          <TabsTrigger value='failures'>
            上游失败 {result.failed ?? 0}
          </TabsTrigger>
          <TabsTrigger value='skipped'>
            已跳过 {result.skipped ?? 0}
          </TabsTrigger>
        </TabsList>
        <TabsContent value='changed' className='mt-2 space-y-2'>
          <ChangedChannelsTable items={changedChannels} />
          {result.changed_details_truncated && (
            <RatioTaskTruncationNote
              visible={changedChannels.length}
              total={result.changed ?? changedChannels.length}
            />
          )}
        </TabsContent>
        <TabsContent value='balance' className='mt-2 space-y-2'>
          <BalanceUpdatesTable items={balanceUpdates} />
          {result.balance_details_truncated && (
            <RatioTaskTruncationNote
              visible={balanceUpdates.length}
              total={result.balance_updated ?? balanceUpdates.length}
            />
          )}
        </TabsContent>
        <TabsContent value='failures' className='mt-2 space-y-2'>
          <RatioTaskFailuresTable items={failures} />
          {result.failure_details_truncated && (
            <RatioTaskTruncationNote
              visible={failures.length}
              total={result.failed}
            />
          )}
        </TabsContent>
        <TabsContent value='skipped' className='mt-2 space-y-2'>
          <SkippedChannelsTable items={skippedChannels} />
          {result.skipped_details_truncated && (
            <RatioTaskTruncationNote
              visible={skippedChannels.length}
              total={result.skipped ?? skippedChannels.length}
            />
          )}
        </TabsContent>
      </Tabs>
    )
  } else if (hasAggregateOnlyDetails) {
    detailContent = (
      <Alert>
        <HugeiconsIcon icon={InformationCircleIcon} />
        <AlertTitle>该记录仅保存了汇总</AlertTitle>
        <AlertDescription>
          这条历史记录生成于渠道明细采集启用前，无法还原当时的具体渠道列表。
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div
      id={props.id}
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
      {detailContent}
    </div>
  )
}

function ChannelMonitorRatioTaskCard(props: {
  task: ChannelMonitorTask
  expanded: boolean
  isLast: boolean
  onToggleDetails: () => void
}) {
  const result = props.task.result
  const progressState = props.task.state
  const impact = getRatioTaskImpact(props.task)
  const detailsId = `channel-monitor-ratio-task-details-${props.task.task_id}`
  const canExpand =
    getRatioTaskDetailCount(props.task) > 0 ||
    hasAggregateOnlyRatioTaskDetails(props.task) ||
    (props.task.result === null && !isActiveChannelMonitorTask(props.task)) ||
    Boolean(props.task.error)
  const detailsExpanded = props.expanded && canExpand
  const partiallyFailed = isTaskPartiallyFailed(props.task)
  const linkedActions = [
    (result?.groups_updated ?? 0) > 0
      ? `更新分组 ${result?.groups_updated}`
      : '',
    (result?.group_memberships_removed ?? 0) > 0
      ? `移出分组 ${result?.group_memberships_removed}`
      : '',
    (result?.channels_disabled ?? 0) > 0
      ? `禁用渠道 ${result?.channels_disabled}`
      : '',
    (result?.channels_enabled ?? 0) > 0
      ? `恢复渠道 ${result?.channels_enabled}`
      : '',
    (result?.groups_skipped ?? 0) > 0
      ? `跳过分组 ${result?.groups_skipped}`
      : '',
  ].filter(Boolean)

  let statusIcon = Clock01Icon
  let statusIconClassName = 'bg-muted text-muted-foreground'
  if (props.task.status === 'running') {
    statusIcon = Activity01Icon
    statusIconClassName = 'bg-warning/15 text-warning'
  } else if (props.task.status === 'failed' || partiallyFailed) {
    statusIcon = Alert02Icon
    statusIconClassName = 'bg-destructive/10 text-destructive'
  } else if (props.task.status === 'succeeded') {
    statusIcon = CheckmarkCircle02Icon
    statusIconClassName = 'bg-success/10 text-success'
  }

  return (
    <li className='relative grid grid-cols-[2rem_minmax(0,1fr)] gap-3'>
      <div className='relative flex justify-center'>
        <span
          className={cn(
            'relative z-10 flex size-8 items-center justify-center rounded-full',
            statusIconClassName
          )}
          aria-hidden='true'
        >
          <HugeiconsIcon icon={statusIcon} className='size-4' />
        </span>
        {!props.isLast && (
          <span
            className='bg-border absolute top-8 bottom-[-0.75rem] w-px'
            aria-hidden='true'
          />
        )}
      </div>
      <article
        data-ratio-task-record
        data-task-status={props.task.status}
        className='bg-card text-card-foreground min-w-0 rounded-xl border p-4 shadow-xs'
      >
        <header className='flex flex-wrap items-start justify-between gap-3'>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-x-3 gap-y-1'>
              <time
                className='font-medium tabular-nums'
                dateTime={new Date(props.task.created_at * 1000).toISOString()}
              >
                {formatTimestampToDate(props.task.created_at)}
              </time>
              <span className='text-muted-foreground inline-flex items-center gap-1 text-xs tabular-nums'>
                <HugeiconsIcon icon={Clock01Icon} className='size-3.5' />
                耗时 {formatTaskDuration(props.task)}
              </span>
            </div>
            <p className='text-muted-foreground mt-1.5 text-sm leading-6'>
              {getRatioTaskHeadline(props.task)}
            </p>
          </div>
          <ChannelTaskStatusBadge task={props.task} />
        </header>

        {isActiveChannelMonitorTask(props.task) && progressState ? (
          <Progress
            className='mt-4'
            value={Math.max(0, Math.min(100, progressState.progress))}
          >
            <ProgressLabel>执行进度</ProgressLabel>
            <ProgressValue>
              {() =>
                `${progressState.processed} / ${progressState.total}（${progressState.progress}%）`
              }
            </ProgressValue>
          </Progress>
        ) : null}

        <dl
          data-ratio-task-metrics
          className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4'
        >
          <RatioTaskMetric label='检查渠道' value={impact.total} />
          <RatioTaskMetric
            label='倍率变化'
            value={result ? impact.changed : '—'}
            tone={impact.changed > 0 ? 'success' : 'default'}
          />
          <RatioTaskMetric
            label='余额刷新'
            value={result ? impact.balanceUpdated : '—'}
            tone={impact.balanceUpdated > 0 ? 'success' : 'default'}
          />
          <RatioTaskMetric
            label='上游失败'
            value={result ? impact.failed : '—'}
            tone={impact.failed > 0 ? 'destructive' : 'default'}
          />
        </dl>

        <div className='mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end'>
          <div className='min-w-0'>
            <span className='text-muted-foreground text-xs font-medium'>
              联动影响
            </span>
            <div className='mt-2 flex min-w-0 flex-wrap items-center gap-2'>
              {linkedActions.length > 0 ? (
                linkedActions.map((action) => (
                  <Badge key={action} variant='outline'>
                    {action}
                  </Badge>
                ))
              ) : (
                <span className='text-muted-foreground text-sm'>
                  未触发分组或渠道状态联动
                </span>
              )}
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2 lg:justify-end'>
            {impact.balanceWarnings > 0 && (
              <Badge variant='warning'>余额预警 {impact.balanceWarnings}</Badge>
            )}
            {(result?.retried ?? 0) > 0 && (
              <Badge variant='outline'>
                重试 {result?.retried}，恢复{' '}
                {result?.recovered_after_retry ?? 0}
              </Badge>
            )}
            {result?.email_status === 'sent' && (
              <Badge variant='outline'>邮件已发送</Badge>
            )}
            {result?.email_status === 'failed' && (
              <Badge variant='destructive'>邮件发送失败</Badge>
            )}
          </div>
        </div>

        {canExpand && (
          <>
            <Separator className='my-3' />
            <ChannelMonitorTaskRowDisclosure
              adjustmentCount={getRatioTaskDetailCount(props.task)}
              truncated={hasRatioTaskDetailTruncation(props.task)}
              expanded={detailsExpanded}
              controlsId={detailsId}
              onToggle={props.onToggleDetails}
            />
            {detailsExpanded && (
              <div className='mt-3'>
                <ChannelMonitorRatioTaskFailureDetails
                  task={props.task}
                  id={detailsId}
                />
              </div>
            )}
          </>
        )}
      </article>
    </li>
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
          最长稳定性评分窗口 {result.stability_window_minutes ?? 0} 分钟
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
  const detailsId = `channel-monitor-task-details-${props.task.task_id}`
  const canExpand =
    props.task.type === 'channel_smart_schedule'
      ? props.task.result !== null || Boolean(props.task.error)
      : getRatioTaskDetailCount(props.task) > 0 ||
        hasAggregateOnlyRatioTaskDetails(props.task) ||
        (props.task.result === null &&
          !isActiveChannelMonitorTask(props.task)) ||
        Boolean(props.task.error)
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
                  : getRatioTaskDetailCount(props.task)
              }
              truncated={
                props.task.type === 'channel_smart_schedule'
                  ? false
                  : hasRatioTaskDetailTruncation(props.task)
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
              <ChannelMonitorRatioTaskFailureDetails
                task={props.task}
                id={detailsId}
              />
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
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
  })
  const tasks = query.data?.data.items ?? []
  const total = query.data?.data.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / TASK_PAGE_SIZE))
  const rangeStart = total === 0 ? 0 : (page - 1) * TASK_PAGE_SIZE + 1
  const rangeEnd = Math.min(page * TASK_PAGE_SIZE, total)
  const latestCompletedTaskTime =
    getLatestCompletedChannelMonitorTaskTime(tasks)
  const pageOverview = {
    succeeded: 0,
    attention: 0,
    active: 0,
    changed: 0,
    balanceUpdated: 0,
    linkedActions: 0,
  }
  for (const task of tasks) {
    const impact = getRatioTaskImpact(task)
    pageOverview.changed += impact.changed
    pageOverview.balanceUpdated += impact.balanceUpdated
    pageOverview.linkedActions += impact.linkedActions
    if (isActiveChannelMonitorTask(task)) pageOverview.active++
    if (task.status === 'failed' || isTaskPartiallyFailed(task)) {
      pageOverview.attention++
    } else if (task.status === 'succeeded') {
      pageOverview.succeeded++
    }
  }

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
          <Skeleton key={key} className='h-40 w-full' />
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
      <ol
        data-ratio-task-timeline
        aria-label='倍率与余额更新时间线'
        className='flex flex-col gap-3 p-3 sm:p-4'
      >
        {tasks.map((task, index) => (
          <ChannelMonitorRatioTaskCard
            key={task.task_id}
            task={task}
            expanded={expandedTaskId === task.task_id}
            isLast={index === tasks.length - 1}
            onToggleDetails={() =>
              setExpandedTaskId((current) =>
                current === task.task_id ? null : task.task_id
              )
            }
          />
        ))}
      </ol>
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
            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground text-xs tabular-nums'>
                共 {total} 条更新任务
              </span>
              <Badge variant='secondary'>成功 {pageOverview.succeeded}</Badge>
              {pageOverview.attention > 0 && (
                <Badge variant='destructive'>
                  需关注 {pageOverview.attention}
                </Badge>
              )}
              {pageOverview.active > 0 && (
                <Badge variant='warning'>执行中 {pageOverview.active}</Badge>
              )}
            </div>
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
            data-ratio-task-overview
            className='grid grid-cols-2 gap-2 lg:grid-cols-4'
            aria-label='本页更新影响'
          >
            <div className='bg-card flex items-center gap-3 rounded-lg border p-3'>
              <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-md'>
                <HugeiconsIcon icon={Exchange01Icon} className='size-5' />
              </span>
              <div>
                <span className='text-muted-foreground block text-xs'>
                  倍率变化
                </span>
                <strong className='mt-0.5 block text-xl tabular-nums'>
                  {pageOverview.changed}
                </strong>
              </div>
            </div>
            <div className='bg-card flex items-center gap-3 rounded-lg border p-3'>
              <span className='bg-success/10 text-success flex size-9 shrink-0 items-center justify-center rounded-md'>
                <HugeiconsIcon icon={Coins01Icon} className='size-5' />
              </span>
              <div>
                <span className='text-muted-foreground block text-xs'>
                  余额刷新
                </span>
                <strong className='mt-0.5 block text-xl tabular-nums'>
                  {pageOverview.balanceUpdated}
                </strong>
              </div>
            </div>
            <div className='bg-card flex items-center gap-3 rounded-lg border p-3'>
              <span className='bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-md'>
                <HugeiconsIcon icon={Link01Icon} className='size-5' />
              </span>
              <div>
                <span className='text-muted-foreground block text-xs'>
                  联动动作
                </span>
                <strong className='mt-0.5 block text-xl tabular-nums'>
                  {pageOverview.linkedActions}
                </strong>
              </div>
            </div>
            <div className='bg-card flex items-center gap-3 rounded-lg border p-3'>
              <span
                className={cn(
                  'flex size-9 shrink-0 items-center justify-center rounded-md',
                  pageOverview.attention > 0
                    ? 'bg-destructive/10 text-destructive'
                    : 'bg-muted text-muted-foreground'
                )}
              >
                <HugeiconsIcon icon={Alert02Icon} className='size-5' />
              </span>
              <div>
                <span className='text-muted-foreground block text-xs'>
                  需关注任务
                </span>
                <strong
                  className={cn(
                    'mt-0.5 block text-xl tabular-nums',
                    pageOverview.attention > 0 && 'text-destructive'
                  )}
                >
                  {pageOverview.attention}
                </strong>
              </div>
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
