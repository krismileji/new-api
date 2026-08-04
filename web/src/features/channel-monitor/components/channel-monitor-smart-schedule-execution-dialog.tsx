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
  ArrowLeft01Icon,
  ArrowRight01Icon,
  HistoryIcon,
  PinIcon,
  Refresh01Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

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
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getChannelMonitorSmartScheduleExecutionDetails,
  getChannelMonitorTasks,
} from '../api'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
} from '../lib/query-options'
import { formatChannelMonitorSmartScheduleFailureStage } from '../lib/smart-schedule-execution'
import { isActiveChannelMonitorTask } from '../lib/task-status'
import type {
  ChannelMonitorTask,
  ChannelMonitorTaskAdjustment,
  ChannelMonitorTaskStatus,
} from '../types'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import {
  ChannelMonitorSmartScheduleExecutionAdjustments,
  ChannelMonitorSmartScheduleExecutionLayout,
} from './channel-monitor-smart-schedule-execution-layout'
import { ChannelMonitorSmartScheduleScoreDetails } from './channel-monitor-smart-schedule-score-details'

const PAGE_SIZE = 20
const DETAIL_PAGE_SIZE = 50
const ACTIVE_REFRESH_INTERVAL_MS = 5000

const STATUS_LABELS: Record<ChannelMonitorTaskStatus, string> = {
  pending: '待执行',
  running: '执行中',
  succeeded: '成功',
  failed: '失败',
}

const ACTION_LABELS: Record<ChannelMonitorTaskAdjustment['action'], string> = {
  updated: '已调整',
  unchanged: '保持',
  skipped: '已跳过',
  failed: '失败',
}

const ACTION_FILTER_OPTIONS = [
  { value: 'all', label: '全部结果' },
  { value: 'updated', label: '已调整' },
  { value: 'unchanged', label: '保持' },
  { value: 'skipped', label: '已跳过' },
  { value: 'failed', label: '失败' },
]

function formatDuration(task: ChannelMonitorTask) {
  if (isActiveChannelMonitorTask(task)) {
    return task.status === 'running' ? '执行中' : '-'
  }
  const seconds = Math.max(0, task.updated_at - task.created_at)
  if (seconds < 1) return '< 1 秒'
  if (seconds < 60) return `${seconds} 秒`
  const minutes = Math.floor(seconds / 60)
  return minutes < 60
    ? `${minutes} 分 ${seconds % 60} 秒`
    : `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分`
}

function statusVariant(status: ChannelMonitorTaskStatus) {
  if (status === 'failed') return 'destructive' as const
  if (status === 'running') return 'warning' as const
  if (status === 'succeeded') return 'secondary' as const
  return 'outline' as const
}

function actionVariant(action: ChannelMonitorTaskAdjustment['action']) {
  if (action === 'failed') return 'destructive' as const
  if (action === 'updated') return 'default' as const
  if (action === 'unchanged') return 'outline' as const
  return 'secondary' as const
}

function taskSummary(task: ChannelMonitorTask) {
  const result = task.result
  if (!result) return '尚未生成执行结果'
  return `调整 ${result.updated} · 保持 ${result.unchanged ?? 0} · 跳过 ${result.skipped ?? 0} · 失败 ${result.failed}`
}

export function ChannelMonitorSmartScheduleAdjustmentRow(props: {
  adjustment: ChannelMonitorTaskAdjustment
  channelNameById?: ReadonlyMap<number, string>
}) {
  const adjustment = props.adjustment
  const failureStage = formatChannelMonitorSmartScheduleFailureStage(
    adjustment.failure_stage
  )
  const hasPreviousEffectiveResult =
    (adjustment.previous_effective_time ?? 0) > 0
  return (
    <li className='grid min-w-0 gap-3 border-b p-3 last:border-b-0 lg:grid-cols-[minmax(12rem,1fr)_minmax(15rem,auto)_auto]'>
      <div className='min-w-0'>
        <div className='truncate font-medium' title={adjustment.channel_name}>
          {adjustment.channel_name || `渠道 ${adjustment.channel_id}`}
        </div>
        <div className='text-muted-foreground mt-1 text-xs break-all'>
          ID {adjustment.channel_id} · {adjustment.group} / {adjustment.model}
        </div>
      </div>
      <div className='flex flex-wrap items-center gap-x-4 gap-y-1 text-xs tabular-nums'>
        <span>
          <span className='text-muted-foreground'>评分 </span>
          {adjustment.score == null
            ? '-'
            : `${(adjustment.score * 100).toFixed(2)} 分`}
        </span>
        <span>
          <span className='text-muted-foreground'>优先级 </span>
          {adjustment.old_priority} → <strong>{adjustment.new_priority}</strong>
        </span>
        <span>
          <span className='text-muted-foreground'>权重 </span>
          {adjustment.old_weight} → <strong>{adjustment.new_weight}</strong>
        </span>
      </div>
      <div className='flex flex-wrap gap-1 lg:justify-self-end'>
        <Badge variant={actionVariant(adjustment.action)}>
          {ACTION_LABELS[adjustment.action]}
        </Badge>
        {failureStage ? (
          <Badge variant='destructive'>失败阶段：{failureStage}</Badge>
        ) : null}
      </div>
      <p className='text-muted-foreground min-w-0 text-xs break-words lg:col-span-3'>
        <span className='text-foreground font-medium'>原因：</span>
        {adjustment.reason || '未记录原因'}
      </p>
      <p className='text-muted-foreground min-w-0 text-xs break-words lg:col-span-3'>
        <span className='text-foreground font-medium'>上一轮生效结果：</span>
        {hasPreviousEffectiveResult
          ? `P${adjustment.previous_effective_priority ?? 0} / W${adjustment.previous_effective_weight ?? 0} · ${formatTimestampToDate(adjustment.previous_effective_time ?? 0)}`
          : '未记录已生效结果'}
        {adjustment.action === 'failed' && hasPreviousEffectiveResult
          ? '；本轮失败未覆盖，上一轮结果继续生效'
          : ''}
      </p>
      <ChannelMonitorSmartScheduleScoreDetails
        details={adjustment.score_details}
        className='lg:col-span-3'
        snapshotLabel='本次执行快照'
        channelNameById={props.channelNameById}
      />
      {adjustment.manual_primary || adjustment.manual_primary_until ? (
        <div className='flex flex-wrap items-center gap-2 text-xs lg:col-span-3'>
          <Badge variant='secondary'>
            <HugeiconsIcon icon={PinIcon} data-icon='inline-start' />
            管理员固定
          </Badge>
          <Badge variant='outline'>
            {adjustment.manual_primary_allow_stability_degrade
              ? '允许稳定性降级'
              : '固定期间不降级'}
          </Badge>
          <span className='text-muted-foreground'>
            固定到期：
            {adjustment.manual_primary_until
              ? formatTimestampToDate(adjustment.manual_primary_until)
              : '未记录'}
          </span>
        </div>
      ) : null}
    </li>
  )
}

function TaskListItem(props: {
  task: ChannelMonitorTask
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type='button'
      className={cn(
        'w-full border-b px-3 py-3 text-left transition-colors hover:bg-muted/60',
        props.selected && 'bg-muted'
      )}
      aria-pressed={props.selected}
      onClick={props.onSelect}
    >
      <div className='flex items-center justify-between gap-2'>
        <span className='text-xs tabular-nums'>
          {formatTimestampToDate(props.task.created_at)}
        </span>
        <Badge variant={statusVariant(props.task.status)}>
          {STATUS_LABELS[props.task.status]}
        </Badge>
      </div>
      <div className='text-muted-foreground mt-2 truncate text-xs'>
        {taskSummary(props.task)}
      </div>
      <div className='text-muted-foreground mt-1 text-[11px]'>
        耗时 {formatDuration(props.task)}
      </div>
    </button>
  )
}

type ChannelMonitorSmartScheduleExecutionPanelProps = {
  active: boolean
}

export function ChannelMonitorSmartScheduleExecutionPanel(
  props: ChannelMonitorSmartScheduleExecutionPanelProps
) {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [groupFilter, setGroupFilter] = useState('all')
  const [modelFilter, setModelFilter] = useState('all')
  const [actionFilter, setActionFilter] = useState('all')
  const [detailPage, setDetailPage] = useState(1)
  const query = useQuery({
    queryKey: [...CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY, page],
    queryFn: () => getChannelMonitorTasks(page, PAGE_SIZE, 'schedule'),
    enabled: props.active,
    staleTime: 15 * 1000,
    refetchInterval: (result) =>
      result.state.data?.data.items.some(isActiveChannelMonitorTask)
        ? ACTIVE_REFRESH_INTERVAL_MS
        : false,
  })
  const tasks = useMemo(
    () => query.data?.data.items ?? [],
    [query.data?.data.items]
  )
  const total = query.data?.data.total ?? 0
  const selectedTask =
    tasks.find((task) => task.task_id === selectedTaskId) ?? tasks[0]
  const detailQuery = useQuery({
    queryKey: [
      ...CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      'details',
      selectedTask?.task_id,
      detailPage,
      search.trim(),
      groupFilter,
      modelFilter,
      actionFilter,
    ],
    queryFn: () =>
      getChannelMonitorSmartScheduleExecutionDetails(
        selectedTask?.task_id ?? '',
        {
          page: detailPage,
          pageSize: DETAIL_PAGE_SIZE,
          search: search.trim(),
          group: groupFilter === 'all' ? undefined : groupFilter,
          model: modelFilter === 'all' ? undefined : modelFilter,
          action: actionFilter === 'all' ? undefined : actionFilter,
        }
      ),
    enabled:
      props.active &&
      selectedTask != null &&
      !isActiveChannelMonitorTask(selectedTask),
    staleTime: Number.POSITIVE_INFINITY,
  })
  const detailResult = detailQuery.data?.data
  const adjustments = detailResult?.items ?? []
  const channelNameById = useMemo(
    () =>
      new Map(
        Object.entries(detailResult?.channel_names ?? {}).map(
          ([channelId, channelName]) => [Number(channelId), channelName]
        )
      ),
    [detailResult?.channel_names]
  )
  const groups = useMemo(
    () => detailResult?.groups ?? [],
    [detailResult?.groups]
  )
  const models = useMemo(
    () => detailResult?.models ?? [],
    [detailResult?.models]
  )
  const groupOptions = useMemo(
    () => [
      { value: 'all', label: '全部分组' },
      ...groups.map((group) => ({ value: group, label: group })),
    ],
    [groups]
  )
  const modelOptions = useMemo(
    () => [
      { value: 'all', label: '全部模型' },
      ...models.map((model) => ({ value: model, label: model })),
    ],
    [models]
  )
  const latestCompletedTaskId = tasks.find(
    (task) => !isActiveChannelMonitorTask(task)
  )?.task_id

  useEffect(() => {
    if (tasks.length === 0) {
      setSelectedTaskId(null)
      return
    }
    if (!tasks.some((task) => task.task_id === selectedTaskId)) {
      setSelectedTaskId(tasks[0].task_id)
      setSearch('')
      setGroupFilter('all')
      setModelFilter('all')
      setActionFilter('all')
      setDetailPage(1)
    }
  }, [selectedTaskId, tasks])

  useEffect(() => {
    if (!latestCompletedTaskId) return
    queryClient.invalidateQueries({
      queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
    })
  }, [latestCompletedTaskId, queryClient])

  const resetFilters = () => {
    setSearch('')
    setGroupFilter('all')
    setModelFilter('all')
    setActionFilter('all')
    setDetailPage(1)
  }
  const selectTask = (taskId: string) => {
    if (taskId !== selectedTask?.task_id) resetFilters()
    setSelectedTaskId(taskId)
  }
  const changeTaskPage = (nextPage: number) => {
    resetFilters()
    setSelectedTaskId(null)
    setPage(nextPage)
  }
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  useEffect(() => {
    if (!query.data || page <= totalPages) return
    changeTaskPage(totalPages)
  }, [page, query.data, totalPages])

  const result = selectedTask?.result
  const detailTotal = detailResult?.total ?? 0
  const detailTotalPages = Math.max(
    1,
    Math.ceil(detailTotal / DETAIL_PAGE_SIZE)
  )

  useEffect(() => {
    if (!detailResult || detailPage <= detailTotalPages) return
    setDetailPage(detailTotalPages)
  }, [detailPage, detailResult, detailTotalPages])

  let detailBody
  if (selectedTask && isActiveChannelMonitorTask(selectedTask)) {
    detailBody = (
      <Empty className='min-h-48 border-0'>
        <EmptyHeader>
          <EmptyTitle>任务正在执行</EmptyTitle>
          <EmptyDescription>执行完成后会加载本次调度明细。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (detailQuery.isLoading) {
    detailBody = (
      <div className='space-y-3 p-4'>
        <Skeleton className='h-28 w-full' />
        <Skeleton className='h-28 w-full' />
      </div>
    )
  } else if (detailQuery.isError) {
    detailBody = (
      <Empty className='min-h-48 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} />
          </EmptyMedia>
          <EmptyTitle>执行明细加载失败</EmptyTitle>
          <EmptyDescription>
            {detailQuery.error instanceof Error
              ? detailQuery.error.message
              : '请稍后重试'}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button
            variant='outline'
            size='sm'
            onClick={() => detailQuery.refetch()}
          >
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重试
          </Button>
        </EmptyContent>
      </Empty>
    )
  } else if (adjustments.length > 0) {
    detailBody = (
      <ol className='divide-y'>
        {adjustments.map((adjustment) => (
          <ChannelMonitorSmartScheduleAdjustmentRow
            key={`${adjustment.channel_id}-${adjustment.group}-${adjustment.model}`}
            adjustment={adjustment}
            channelNameById={channelNameById}
          />
        ))}
      </ol>
    )
  } else {
    detailBody = (
      <Empty className='min-h-48 border-0'>
        <EmptyHeader>
          <EmptyTitle>没有匹配的路由记录</EmptyTitle>
          <EmptyDescription>调整筛选条件后重试。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  let body
  if (query.isLoading) {
    body = (
      <div className='grid h-full gap-4 p-4 lg:grid-cols-[18rem_minmax(0,1fr)]'>
        <Skeleton className='h-full min-h-56' />
        <Skeleton className='h-full min-h-56' />
      </div>
    )
  } else if (query.isError && tasks.length === 0) {
    body = (
      <Empty className='h-full min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} />
          </EmptyMedia>
          <EmptyTitle>执行记录加载失败</EmptyTitle>
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
    body = (
      <Empty className='h-full min-h-64 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={HistoryIcon} />
          </EmptyMedia>
          <EmptyTitle>暂无智能调度执行记录</EmptyTitle>
          <EmptyDescription>
            开启智能调度或手动执行后，详细结果会在这里保留。
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    body = (
      <ChannelMonitorSmartScheduleExecutionLayout
        taskList={
          <>
            <div className='bg-muted/20 border-b px-3 py-2 text-xs font-medium'>
              执行批次（共 {total} 条）
            </div>
            {tasks.map((task) => (
              <TaskListItem
                key={task.task_id}
                task={task}
                selected={task.task_id === selectedTask?.task_id}
                onSelect={() => selectTask(task.task_id)}
              />
            ))}
          </>
        }
      >
        {selectedTask && (
          <>
            {query.isError ? (
              <Alert variant='destructive' className='m-3 mb-0'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>执行记录刷新失败</AlertTitle>
                <AlertDescription>
                  当前显示上一次成功加载的记录，可点击下方刷新重试。
                </AlertDescription>
              </Alert>
            ) : null}
            <div className='border-b p-4'>
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='min-w-0'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <h3 className='font-medium'>智能调度执行详情</h3>
                    <Badge variant={statusVariant(selectedTask.status)}>
                      {STATUS_LABELS[selectedTask.status]}
                    </Badge>
                    {result?.force_reset ? (
                      <Badge variant='outline'>强制重算</Badge>
                    ) : null}
                  </div>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {formatTimestampToDate(selectedTask.created_at)} · 任务 ID{' '}
                    {selectedTask.task_id} · 耗时 {formatDuration(selectedTask)}
                  </p>
                </div>
                <div className='text-muted-foreground text-xs tabular-nums'>
                  {taskSummary(selectedTask)}
                </div>
              </div>
              {selectedTask.error ? (
                <Alert variant='destructive' className='mt-3'>
                  <HugeiconsIcon icon={Alert02Icon} />
                  <AlertTitle>任务错误</AlertTitle>
                  <AlertDescription className='break-all'>
                    {selectedTask.error}
                  </AlertDescription>
                </Alert>
              ) : null}
              <div className='mt-4 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4'>
                <div className='bg-muted/40 rounded-md p-2'>
                  <span className='text-muted-foreground block'>总路由</span>
                  <strong>{result?.total ?? 0}</strong>
                </div>
                <div className='bg-muted/40 rounded-md p-2'>
                  <span className='text-muted-foreground block'>已调整</span>
                  <strong>{result?.updated ?? 0}</strong>
                </div>
                <div className='bg-muted/40 rounded-md p-2'>
                  <span className='text-muted-foreground block'>保持/跳过</span>
                  <strong>
                    {(result?.unchanged ?? 0) + (result?.skipped ?? 0)}
                  </strong>
                </div>
                <div className='bg-muted/40 rounded-md p-2'>
                  <span className='text-muted-foreground block'>失败</span>
                  <strong
                    className={result?.failed ? 'text-destructive' : undefined}
                  >
                    {result?.failed ?? 0}
                  </strong>
                </div>
              </div>
              {result ? (
                <p className='text-muted-foreground mt-3 text-xs'>
                  {(result.group_policy_count ?? result.group_policies?.length)
                    ? `按 ${result.group_policy_count ?? result.group_policies?.length} 个分组策略执行`
                    : '未记录分组策略'}{' '}
                  · 性能窗口 {result.performance_window_minutes ?? 0} 分钟 ·
                  稳定性窗口 {result.stability_window_minutes ?? 0} 分钟
                </p>
              ) : null}
            </div>
            <div className='flex flex-wrap items-center gap-2 border-b p-3'>
              <InputGroup className='min-w-48 flex-1 sm:max-w-xs'>
                <InputGroupAddon>
                  <HugeiconsIcon icon={Search01Icon} />
                </InputGroupAddon>
                <InputGroupInput
                  value={search}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    setDetailPage(1)
                  }}
                  placeholder='搜索渠道、分组、模型或原因'
                  aria-label='搜索执行明细'
                />
              </InputGroup>
              <Select
                items={groupOptions}
                value={groupFilter}
                onValueChange={(value) => {
                  setGroupFilter(value ?? 'all')
                  setDetailPage(1)
                }}
              >
                <SelectTrigger className='w-36' aria-label='按分组筛选'>
                  <SelectValue placeholder='全部分组' />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='all'>全部分组</SelectItem>
                    {groups.map((group) => (
                      <SelectItem key={group} value={group}>
                        {group}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Select
                items={modelOptions}
                value={modelFilter}
                onValueChange={(value) => {
                  setModelFilter(value ?? 'all')
                  setDetailPage(1)
                }}
              >
                <SelectTrigger className='w-36' aria-label='按模型筛选'>
                  <SelectValue placeholder='全部模型' />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='all'>全部模型</SelectItem>
                    {models.map((model) => (
                      <SelectItem key={model} value={model}>
                        {model}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Select
                items={ACTION_FILTER_OPTIONS}
                value={actionFilter}
                onValueChange={(value) => {
                  setActionFilter(value ?? 'all')
                  setDetailPage(1)
                }}
              >
                <SelectTrigger className='w-28' aria-label='按结果筛选'>
                  <SelectValue placeholder='全部结果' />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='all'>全部结果</SelectItem>
                    <SelectItem value='updated'>已调整</SelectItem>
                    <SelectItem value='unchanged'>保持</SelectItem>
                    <SelectItem value='skipped'>已跳过</SelectItem>
                    <SelectItem value='failed'>失败</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              {search ||
              groupFilter !== 'all' ||
              modelFilter !== 'all' ||
              actionFilter !== 'all' ? (
                <Button variant='ghost' size='sm' onClick={resetFilters}>
                  清除筛选
                </Button>
              ) : null}
              <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
                当前页 {adjustments.length} 条 · 共 {detailTotal} 条
              </span>
            </div>
            <ChannelMonitorSmartScheduleExecutionAdjustments>
              {detailBody}
            </ChannelMonitorSmartScheduleExecutionAdjustments>
            {detailTotal > 0 ? (
              <div className='flex shrink-0 items-center justify-between gap-3 border-t px-3 py-2'>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  明细第 {detailPage} / {detailTotalPages} 页
                </span>
                <div className='flex items-center gap-1'>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label='上一页明细'
                    title='上一页明细'
                    onClick={() =>
                      setDetailPage((current) => Math.max(1, current - 1))
                    }
                    disabled={detailPage <= 1 || detailQuery.isFetching}
                  >
                    <HugeiconsIcon icon={ArrowLeft01Icon} />
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label='下一页明细'
                    title='下一页明细'
                    onClick={() =>
                      setDetailPage((current) =>
                        Math.min(detailTotalPages, current + 1)
                      )
                    }
                    disabled={
                      detailPage >= detailTotalPages || detailQuery.isFetching
                    }
                  >
                    <HugeiconsIcon icon={ArrowRight01Icon} />
                  </Button>
                </div>
              </div>
            ) : null}
          </>
        )}
      </ChannelMonitorSmartScheduleExecutionLayout>
    )
  }

  return (
    <div
      className='grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] gap-3 overflow-hidden'
      data-smart-schedule-execution-panel
    >
      <div
        className='min-h-0 overflow-hidden rounded-lg border'
        aria-busy={query.isFetching || detailQuery.isFetching}
      >
        {body}
      </div>
      {total > 0 ? (
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <span className='text-muted-foreground text-xs tabular-nums'>
            共 {total} 个执行批次
          </span>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='icon-sm'
              aria-label='上一页'
              title='上一页'
              onClick={() => changeTaskPage(Math.max(1, page - 1))}
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
              onClick={() => changeTaskPage(Math.min(totalPages, page + 1))}
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
            {query.isFetching ? <Spinner /> : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}

type ChannelMonitorSmartScheduleExecutionDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChannelMonitorSmartScheduleExecutionDialog(
  props: ChannelMonitorSmartScheduleExecutionDialogProps
) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'grid-rows-[auto_minmax(0,1fr)] sm:max-w-6xl'
        )}
      >
        <DialogHeader className='pr-10'>
          <DialogTitle>智能调度执行记录</DialogTitle>
          <DialogDescription>
            按执行批次查看评分输入、计算结果、优先级与权重变化，以及每次调整的原因。
          </DialogDescription>
        </DialogHeader>
        <ChannelMonitorSmartScheduleExecutionPanel active={props.open} />
      </DialogContent>
    </Dialog>
  )
}
