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
  Activity01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ArrowUp01Icon,
  Cancel01Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  InformationCircleIcon,
  HistoryIcon,
  PinIcon,
  Refresh01Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
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
  InputGroupButton,
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
import { orderGroupNames } from '@/lib/group-order'
import { cn } from '@/lib/utils'

import {
  getChannelMonitorSmartScheduleExecutionDetails,
  getChannelMonitorTasks,
} from '../api'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
} from '../lib/query-options'
import {
  formatChannelMonitorSmartScheduleFailureStage,
  loadChannelMonitorSmartScheduleExecutionSelection,
  orderChannelMonitorSmartScheduleAdjustmentsByRoutingPolicy,
  orderChannelMonitorSmartScheduleModels,
  orderChannelMonitorTasksByExecutionTime,
  saveChannelMonitorSmartScheduleExecutionSelection,
} from '../lib/smart-schedule-execution'
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

function adjustmentIcon(action: ChannelMonitorTaskAdjustment['action']) {
  if (action === 'failed') return Cancel01Icon
  if (action === 'updated') return ArrowUp01Icon
  if (action === 'unchanged') return CheckmarkCircle02Icon
  return Clock01Icon
}

function taskStatusTone(status: ChannelMonitorTaskStatus) {
  if (status === 'failed') return 'bg-destructive'
  if (status === 'running') return 'bg-warning'
  if (status === 'succeeded') return 'bg-success'
  return 'bg-muted-foreground/50'
}

function adjustmentAccent(action: ChannelMonitorTaskAdjustment['action']) {
  if (action === 'failed') return 'border-l-destructive'
  if (action === 'updated') return 'border-l-primary'
  return 'border-l-muted-foreground/40'
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
  const actionIcon = adjustmentIcon(adjustment.action)
  const actionAccent = adjustmentAccent(adjustment.action)
  return (
    <li
      className={cn(
        'grid min-w-0 gap-4 border-b border-l-4 bg-background px-4 py-4 transition-colors last:border-b-0 hover:bg-muted/20 sm:px-5',
        actionAccent
      )}
      data-adjustment-action={adjustment.action}
    >
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='flex min-w-0 items-start gap-3'>
          <span className='bg-muted/60 text-muted-foreground mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={actionIcon} size={18} aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <div
                className='truncate font-medium'
                title={adjustment.channel_name}
              >
                {adjustment.channel_name || `渠道 ${adjustment.channel_id}`}
              </div>
              <Badge variant={actionVariant(adjustment.action)}>
                {ACTION_LABELS[adjustment.action]}
              </Badge>
              {failureStage ? (
                <Badge variant='destructive'>失败阶段：{failureStage}</Badge>
              ) : null}
            </div>
            <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-2 text-xs'>
              <span>{adjustment.group || '未分组'}</span>
              <span aria-hidden='true'>/</span>
              <span>{adjustment.model || '未指定模型'}</span>
              <span aria-hidden='true'>·</span>
              <span>ID {adjustment.channel_id}</span>
            </div>
          </div>
        </div>
        <span className='text-muted-foreground hidden shrink-0 text-[11px] sm:block'>
          路由变化
        </span>
      </div>

      <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
        <div className='bg-muted/35 rounded-md px-3 py-2'>
          <span className='text-muted-foreground block text-[11px]'>
            综合评分
          </span>
          <strong className='mt-1 block text-sm tabular-nums'>
            {adjustment.score == null
              ? '-'
              : `${(adjustment.score * 100).toFixed(2)} 分`}
          </strong>
        </div>
        <div className='bg-muted/35 rounded-md px-3 py-2'>
          <span className='text-muted-foreground block text-[11px]'>
            优先级
          </span>
          <strong className='mt-1 block text-sm tabular-nums'>
            {adjustment.old_priority}
            <span className='text-muted-foreground mx-1 font-normal'>→</span>
            {adjustment.new_priority}
          </strong>
        </div>
        <div className='bg-muted/35 rounded-md px-3 py-2'>
          <span className='text-muted-foreground block text-[11px]'>权重</span>
          <strong className='mt-1 block text-sm tabular-nums'>
            {adjustment.old_weight}
            <span className='text-muted-foreground mx-1 font-normal'>→</span>
            {adjustment.new_weight}
          </strong>
        </div>
      </div>

      <div className='border-muted-foreground/15 bg-muted/15 rounded-md border px-3 py-2.5 text-xs'>
        <span className='text-foreground font-medium'>调度理由</span>
        <p className='text-muted-foreground mt-1 break-words'>
          {adjustment.reason || '未记录原因'}
        </p>
      </div>

      {hasPreviousEffectiveResult ? (
        <div className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
          <HugeiconsIcon
            icon={InformationCircleIcon}
            size={14}
            aria-hidden='true'
          />
          <span>
            上一轮生效：P{adjustment.previous_effective_priority ?? 0} / W
            {adjustment.previous_effective_weight ?? 0}
          </span>
          <span>
            {formatTimestampToDate(adjustment.previous_effective_time ?? 0)}
          </span>
          {adjustment.action === 'failed' ? (
            <span className='text-destructive'>本轮失败未覆盖，继续沿用</span>
          ) : null}
        </div>
      ) : null}

      <ChannelMonitorSmartScheduleScoreDetails
        details={adjustment.score_details}
        snapshotLabel='本次执行快照'
        channelNameById={props.channelNameById}
      />
      {adjustment.manual_primary || adjustment.manual_primary_until ? (
        <div className='flex flex-wrap items-center gap-2 text-xs'>
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
  position: number
  onSelect: () => void
}) {
  const statusTone = taskStatusTone(props.task.status)
  const result = props.task.result
  return (
    <button
      type='button'
      className={cn(
        'relative w-full border-b px-4 py-3.5 text-left transition-colors hover:bg-muted/60',
        props.selected &&
          'bg-primary/[0.06] shadow-[inset_3px_0_0_var(--primary)]'
      )}
      aria-pressed={props.selected}
      data-task-status={props.task.status}
      onClick={props.onSelect}
    >
      <div className='flex items-start gap-3'>
        <span className='relative mt-1.5 flex shrink-0 justify-center'>
          <span className={cn('size-2.5 rounded-full', statusTone)} />
        </span>
        <span className='min-w-0 flex-1'>
          <span className='flex items-center justify-between gap-2'>
            <span className='font-medium'>第 {props.position} 批</span>
            <Badge variant={statusVariant(props.task.status)}>
              {STATUS_LABELS[props.task.status]}
            </Badge>
          </span>
          <span className='text-muted-foreground mt-1 block text-xs tabular-nums'>
            {formatTimestampToDate(props.task.created_at)}
          </span>
          <span className='mt-2 flex flex-wrap gap-x-2 gap-y-1 text-xs tabular-nums'>
            <span className='text-primary'>{result?.updated ?? 0} 已调整</span>
            <span className='text-muted-foreground'>
              {(result?.unchanged ?? 0) + (result?.skipped ?? 0)} 保持
            </span>
            <span
              className={
                result?.failed ? 'text-destructive' : 'text-muted-foreground'
              }
            >
              {result?.failed ?? 0} 失败
            </span>
          </span>
        </span>
      </div>
      <div className='text-muted-foreground mt-2 pl-[1.375rem] text-[11px]'>
        <span>耗时 {formatDuration(props.task)}</span>
        <span className='mx-1.5' aria-hidden='true'>
          ·
        </span>
        <span className='font-mono'>{props.task.task_id.slice(0, 12)}</span>
      </div>
    </button>
  )
}

type ChannelMonitorSmartScheduleExecutionPanelProps = {
  active: boolean
  groupOrder?: readonly string[]
  modelsByGroup?: ReadonlyMap<string, readonly string[]>
  selection?: ChannelMonitorSmartScheduleExecutionSelection
  onSelectionChange?: (
    selection: ChannelMonitorSmartScheduleExecutionSelection
  ) => void
}

export type ChannelMonitorSmartScheduleExecutionSelection = {
  group: string
  model: string
}

export function ChannelMonitorSmartScheduleExecutionPanel(
  props: ChannelMonitorSmartScheduleExecutionPanelProps
) {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [selection, setSelection] = useState(
    () => props.selection ?? loadChannelMonitorSmartScheduleExecutionSelection()
  )
  const [groupFilter, setGroupFilter] = useState(() => selection.group)
  const [modelFilter, setModelFilter] = useState(() => selection.model)
  const [actionFilter, setActionFilter] = useState('all')
  const [detailPage, setDetailPage] = useState(1)
  const query = useQuery({
    queryKey: [...CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY, page],
    queryFn: () => getChannelMonitorTasks(page, PAGE_SIZE, 'schedule'),
    enabled: props.active,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
  })
  const tasks = useMemo(
    () => orderChannelMonitorTasksByExecutionTime(query.data?.data.items ?? []),
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
          group: groupFilter || undefined,
          model:
            modelFilter === 'all' || !modelFilter ? undefined : modelFilter,
          action: actionFilter === 'all' ? undefined : actionFilter,
        }
      ),
    enabled:
      props.active &&
      selectedTask != null &&
      !isActiveChannelMonitorTask(selectedTask),
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === selectedTask?.task_id
        ? keepPreviousData(previousData)
        : undefined,
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
  const refreshExecutionData = async () => {
    const detailRefresh =
      selectedTask != null && !isActiveChannelMonitorTask(selectedTask)
        ? detailQuery.refetch()
        : Promise.resolve()
    await Promise.all([query.refetch(), detailRefresh])
  }
  const detailResult = detailQuery.data?.data
  const adjustments = useMemo(
    () =>
      orderChannelMonitorSmartScheduleAdjustmentsByRoutingPolicy(
        detailResult?.items ?? []
      ),
    [detailResult?.items]
  )
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
    () => orderGroupNames(detailResult?.groups ?? [], props.groupOrder),
    [detailResult?.groups, props.groupOrder]
  )
  const historicalModelsByGroup = detailResult?.models_by_group
  const models = useMemo(
    () =>
      orderChannelMonitorSmartScheduleModels(
        historicalModelsByGroup
          ? (historicalModelsByGroup[groupFilter] ?? [])
          : (detailResult?.models ?? []),
        props.modelsByGroup?.get(groupFilter) ?? []
      ),
    [
      detailResult?.models,
      groupFilter,
      historicalModelsByGroup,
      props.modelsByGroup,
    ]
  )
  const groupOptions = useMemo(
    () => groups.map((group) => ({ value: group, label: group })),
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
  const externalGroup = props.selection?.group
  const externalModel = props.selection?.model

  useEffect(() => {
    if (externalGroup === undefined || externalModel === undefined) return
    setSelection({ group: externalGroup, model: externalModel })
    setGroupFilter(externalGroup)
    setModelFilter(externalModel)
  }, [externalGroup, externalModel])

  useEffect(() => {
    if (groups.length === 0) return
    let nextGroup = groups[0]
    if (selection.group && groups.includes(selection.group)) {
      nextGroup = selection.group
    } else if (groups.includes(groupFilter)) {
      nextGroup = groupFilter
    }
    if (nextGroup === groupFilter) return
    const availableModels = historicalModelsByGroup
      ? (historicalModelsByGroup[nextGroup] ?? [])
      : (detailResult?.models ?? [])
    const nextModels = orderChannelMonitorSmartScheduleModels(
      availableModels,
      props.modelsByGroup?.get(nextGroup) ?? []
    )
    let nextModel = nextModels[0] ?? ''
    if (
      selection.group === nextGroup &&
      (selection.model === 'all' || nextModels.includes(selection.model))
    ) {
      nextModel = selection.model
    }
    setGroupFilter(nextGroup)
    setModelFilter(nextModel)
    setDetailPage(1)
  }, [
    detailResult?.models,
    groupFilter,
    groups,
    historicalModelsByGroup,
    props.modelsByGroup,
    selection.group,
    selection.model,
  ])

  useEffect(() => {
    if (models.length === 0) return
    let nextModel = models[0]
    if (
      selection.group === groupFilter &&
      (selection.model === 'all' || models.includes(selection.model))
    ) {
      nextModel = selection.model
    } else if (modelFilter === 'all' || models.includes(modelFilter)) {
      nextModel = modelFilter
    }
    if (nextModel === modelFilter) return
    setModelFilter(nextModel)
    setDetailPage(1)
  }, [groupFilter, modelFilter, models, selection.group, selection.model])

  useEffect(() => {
    if (tasks.length === 0) {
      setSelectedTaskId(null)
      return
    }
    if (!tasks.some((task) => task.task_id === selectedTaskId)) {
      setSelectedTaskId(tasks[0].task_id)
      setSearch('')
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
    setActionFilter('all')
    setDetailPage(1)
  }
  const saveSelection = (
    nextSelection: ChannelMonitorSmartScheduleExecutionSelection
  ) => {
    setSelection(nextSelection)
    saveChannelMonitorSmartScheduleExecutionSelection(nextSelection)
    props.onSelectionChange?.(nextSelection)
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
  const selectedTaskPosition = selectedTask
    ? (page - 1) * PAGE_SIZE +
      Math.max(
        0,
        tasks.findIndex((task) => task.task_id === selectedTask.task_id)
      ) +
      1
    : 0
  const detailTotal = detailResult?.total ?? 0
  const detailTotalPages = Math.max(
    1,
    Math.ceil(detailTotal / DETAIL_PAGE_SIZE)
  )

  useEffect(() => {
    if (!detailResult || detailPage <= detailTotalPages) return
    setDetailPage(detailTotalPages)
  }, [detailPage, detailResult, detailTotalPages])

  const detailPending = detailQuery.isLoading || detailQuery.isPlaceholderData
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
  } else if (detailPending) {
    detailBody = (
      <div className='space-y-3 p-4' data-schedule-execution-detail-loading>
        <Skeleton className='h-28 w-full' />
        <Skeleton className='h-28 w-full' />
        <div className='text-muted-foreground flex items-center justify-center gap-2 text-xs'>
          <Spinner aria-label='执行明细加载中' />
          <span>正在加载执行明细</span>
        </div>
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
      <ol className='bg-background'>
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
            <div className='bg-muted/20 border-b px-4 py-3'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                <span className='bg-primary/10 text-primary flex size-7 items-center justify-center rounded-md'>
                  <HugeiconsIcon
                    icon={Activity01Icon}
                    size={16}
                    aria-hidden='true'
                  />
                </span>
                <span>执行时间线</span>
                <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
                  {total} 批
                </span>
              </div>
              <p className='text-muted-foreground mt-1 pl-9 text-[11px]'>
                选择一批查看完整调度结果
              </p>
            </div>
            {tasks.map((task, index) => (
              <TaskListItem
                key={task.task_id}
                task={task}
                position={(page - 1) * PAGE_SIZE + index + 1}
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
            <div className='bg-muted/[0.08] border-b p-4 sm:p-5'>
              <div className='flex flex-wrap items-start justify-between gap-4'>
                <div className='flex min-w-0 items-start gap-3'>
                  <span className='bg-primary/10 text-primary mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-xl'>
                    <HugeiconsIcon
                      icon={Activity01Icon}
                      size={20}
                      aria-hidden='true'
                    />
                  </span>
                  <div className='min-w-0'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
                        第 {selectedTaskPosition} 批执行
                      </span>
                      <Badge variant={statusVariant(selectedTask.status)}>
                        {STATUS_LABELS[selectedTask.status]}
                      </Badge>
                      {result?.force_reset ? (
                        <Badge variant='outline'>强制重算</Badge>
                      ) : null}
                    </div>
                    <h3 className='mt-1 truncate text-base font-semibold'>
                      智能调度执行详情
                    </h3>
                    <p className='text-muted-foreground mt-1 flex flex-wrap gap-x-2 text-xs'>
                      <span>
                        {formatTimestampToDate(selectedTask.created_at)}
                      </span>
                      <span aria-hidden='true'>·</span>
                      <span>耗时 {formatDuration(selectedTask)}</span>
                      <span aria-hidden='true'>·</span>
                      <span className='font-mono'>{selectedTask.task_id}</span>
                    </p>
                  </div>
                </div>
                <div className='border-primary/20 bg-primary/[0.06] text-primary rounded-lg border px-3 py-2 text-right text-xs tabular-nums'>
                  <span className='text-primary/70 block text-[11px]'>
                    本批执行结论
                  </span>
                  <strong className='mt-1 block'>
                    {taskSummary(selectedTask)}
                  </strong>
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
              <div className='mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4'>
                <div className='border-border/70 bg-background rounded-lg border px-3 py-2.5'>
                  <span className='text-muted-foreground block text-[11px]'>
                    总路由
                  </span>
                  <strong className='mt-1 block text-lg tabular-nums'>
                    {result?.total ?? 0}
                  </strong>
                </div>
                <div className='border-primary/20 bg-primary/[0.04] rounded-lg border px-3 py-2.5'>
                  <span className='text-muted-foreground block text-[11px]'>
                    已调整
                  </span>
                  <strong className='text-primary mt-1 block text-lg tabular-nums'>
                    {result?.updated ?? 0}
                  </strong>
                </div>
                <div className='border-border/70 bg-background rounded-lg border px-3 py-2.5'>
                  <span className='text-muted-foreground block text-[11px]'>
                    保持 / 跳过
                  </span>
                  <strong className='mt-1 block text-lg tabular-nums'>
                    {(result?.unchanged ?? 0) + (result?.skipped ?? 0)}
                  </strong>
                </div>
                <div
                  className={cn(
                    'rounded-lg border px-3 py-2.5',
                    result?.failed
                      ? 'border-destructive/30 bg-destructive/[0.05]'
                      : 'border-border/70 bg-background'
                  )}
                >
                  <span className='text-muted-foreground block text-[11px]'>
                    失败
                  </span>
                  <strong
                    className={cn(
                      'mt-1 block text-lg tabular-nums',
                      result?.failed && 'text-destructive'
                    )}
                  >
                    {result?.failed ?? 0}
                  </strong>
                </div>
              </div>
              {result ? (
                <div className='text-muted-foreground mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
                  <span className='text-foreground font-medium'>执行参数</span>
                  <span>
                    {(result.group_policy_count ??
                    result.group_policies?.length)
                      ? `按 ${result.group_policy_count ?? result.group_policies?.length} 个分组策略执行`
                      : '未记录分组策略'}
                  </span>
                  <span aria-hidden='true'>·</span>
                  <span>
                    性能窗口 {result.performance_window_minutes ?? 0} 分钟
                  </span>
                  <span aria-hidden='true'>·</span>
                  <span>
                    稳定性窗口 {result.stability_window_minutes ?? 0} 分钟
                  </span>
                </div>
              ) : null}
            </div>
            <div
              className='bg-background flex flex-col gap-2 border-b p-3 sm:flex-row sm:flex-wrap sm:items-center sm:px-5 xl:flex-nowrap'
              role='group'
              aria-label='筛选明细'
              data-schedule-execution-filters
            >
              <InputGroup className='min-w-0 flex-1 ring-inset sm:min-w-48'>
                <InputGroupAddon>
                  <HugeiconsIcon icon={Search01Icon} size={16} />
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
                {search || actionFilter !== 'all' ? (
                  <InputGroupAddon align='inline-end'>
                    <InputGroupButton
                      size='icon-xs'
                      onClick={resetFilters}
                      aria-label='清除筛选'
                      title='清除筛选'
                    >
                      <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
                      <span className='sr-only'>清除筛选</span>
                    </InputGroupButton>
                  </InputGroupAddon>
                ) : null}
              </InputGroup>
              <Select
                items={groupOptions}
                value={groupFilter}
                onValueChange={(value) => {
                  if (value === null) return
                  const availableModels = historicalModelsByGroup
                    ? (historicalModelsByGroup[value] ?? [])
                    : (detailResult?.models ?? [])
                  const nextModels = orderChannelMonitorSmartScheduleModels(
                    availableModels,
                    props.modelsByGroup?.get(value) ?? []
                  )
                  const nextModel = nextModels[0] ?? ''
                  setGroupFilter(value)
                  setModelFilter(nextModel)
                  saveSelection({
                    group: value,
                    model: nextModel,
                  })
                  setDetailPage(1)
                }}
              >
                <SelectTrigger
                  className='w-full min-w-0 sm:w-36 sm:shrink-0'
                  aria-label='按分组筛选'
                  title={groupFilter || undefined}
                >
                  <SelectValue placeholder='选择分组' />
                </SelectTrigger>
                <SelectContent
                  align='start'
                  alignItemWithTrigger={false}
                  className='w-max max-w-[min(24rem,calc(100vw-2rem))] min-w-[var(--anchor-width)]'
                >
                  <SelectGroup>
                    {groups.map((group) => (
                      <SelectItem
                        key={group}
                        value={group}
                        title={group}
                        className='[&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:truncate'
                      >
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
                  if (value === null) return
                  setModelFilter(value)
                  saveSelection({
                    group: groupFilter,
                    model: value,
                  })
                  setDetailPage(1)
                }}
              >
                <SelectTrigger
                  className='w-full min-w-0 sm:w-56 sm:shrink-0'
                  aria-label='按模型筛选'
                  title={
                    modelFilter === 'all'
                      ? '全部模型'
                      : modelFilter || undefined
                  }
                >
                  <SelectValue
                    className='min-w-0 truncate'
                    placeholder='全部模型'
                  />
                </SelectTrigger>
                <SelectContent
                  align='start'
                  alignItemWithTrigger={false}
                  className='w-max max-w-[min(24rem,calc(100vw-2rem))] min-w-[var(--anchor-width)]'
                >
                  <SelectGroup>
                    <SelectItem value='all'>全部模型</SelectItem>
                    {models.map((model) => (
                      <SelectItem
                        key={model}
                        value={model}
                        title={model}
                        className='[&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:truncate'
                      >
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
                <SelectTrigger
                  className='w-full sm:w-28 sm:shrink-0'
                  aria-label='按结果筛选'
                >
                  <SelectValue placeholder='全部结果' />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='all'>全部结果</SelectItem>
                    <SelectItem value='updated'>已调整</SelectItem>
                    <SelectItem value='unchanged'>保持</SelectItem>
                    <SelectItem value='skipped'>已跳过</SelectItem>
                    <SelectItem value='failed'>失败</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <span className='text-muted-foreground self-end text-xs whitespace-nowrap tabular-nums sm:ml-auto sm:self-auto xl:ml-0'>
                {detailQuery.isFetching
                  ? '正在更新'
                  : `${adjustments.length} / ${detailTotal} 条`}
              </span>
            </div>
            <div
              className='bg-muted/20 text-muted-foreground flex items-start gap-2 border-b px-4 py-2.5 text-xs sm:px-5'
              data-routing-semantics
            >
              <HugeiconsIcon
                icon={InformationCircleIcon}
                className='mt-0.5 shrink-0'
                size={14}
                aria-hidden='true'
              />
              <span>
                优先级高的路由层先参与调度；同优先级内按权重随机选择，权重越高命中概率越高。
              </span>
            </div>
            <ChannelMonitorSmartScheduleExecutionAdjustments>
              {detailBody}
            </ChannelMonitorSmartScheduleExecutionAdjustments>
            {detailTotal > 0 && !detailQuery.isPlaceholderData ? (
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
              onClick={() => void refreshExecutionData()}
              disabled={query.isFetching || detailQuery.isFetching}
            >
              <HugeiconsIcon
                icon={Refresh01Icon}
                className={cn(
                  (query.isFetching || detailQuery.isFetching) && 'animate-spin'
                )}
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
  groupOrder?: readonly string[]
  modelsByGroup?: ReadonlyMap<string, readonly string[]>
  selection?: ChannelMonitorSmartScheduleExecutionSelection
  onSelectionChange?: (
    selection: ChannelMonitorSmartScheduleExecutionSelection
  ) => void
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
        <ChannelMonitorSmartScheduleExecutionPanel
          active={props.open}
          groupOrder={props.groupOrder}
          modelsByGroup={props.modelsByGroup}
          selection={props.selection}
          onSelectionChange={props.onSelectionChange}
        />
      </DialogContent>
    </Dialog>
  )
}
