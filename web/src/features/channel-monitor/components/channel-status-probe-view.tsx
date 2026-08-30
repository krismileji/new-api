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
  PauseIcon,
  PlayIcon,
  Refresh01Icon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import {
  lazy,
  memo,
  Suspense,
  useCallback,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { channelsQueryKeys } from '@/features/channels/lib/channel-actions'
import { orderGroupNames } from '@/lib/group-order'

import {
  getChannelStatusProbeOverview,
  runChannelStatusProbe,
  updateChannelStatusProbeConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
} from '../lib/query-options'
import { orderChannelsByReferenceOrder } from '../lib/sort'
import {
  isChannelStatusProbeActive,
  isChannelStatusProbeIssue,
  matchesChannelStatusProbeGroup,
  matchesChannelStatusProbeSearch,
} from '../lib/status-probe'
import type {
  ChannelStatusProbeChannel,
  ChannelStatusProbeHealth,
} from '../types'
import { ChannelStatusProbeCard } from './channel-status-probe-card'

const LazyChannelStatusProbeConfigSheet = lazy(() =>
  import('./channel-status-probe-config-sheet').then((module) => ({
    default: module.ChannelStatusProbeConfigSheet,
  }))
)

const LazyChannelStatusProbeHistorySheet = lazy(() =>
  import('./channel-status-probe-history-sheet').then((module) => ({
    default: module.ChannelStatusProbeHistorySheet,
  }))
)

type StatusFilter =
  | 'all'
  | 'issue'
  | 'stale'
  | 'healthy'
  | 'paused'
  | 'unconfigured'

const STATUS_FILTER_OPTIONS: Array<[StatusFilter, string]> = [
  ['all', '全部'],
  ['issue', '异常'],
  ['stale', '过期'],
  ['healthy', '正常'],
  ['paused', '已暂停'],
  ['unconfigured', '未配置'],
]

const EMPTY_CHANNELS: ChannelStatusProbeChannel[] = []
const EMPTY_MODEL_NAMES: string[] = []

function matchesStatusFilter(
  health: ChannelStatusProbeHealth,
  filter: StatusFilter
) {
  if (filter === 'all') return true
  if (filter === 'issue') return isChannelStatusProbeIssue(health)
  return health === filter
}

export type ChannelStatusProbeViewProps = {
  channelOrder: readonly number[]
  groupOrder?: readonly string[]
  onActionComplete?: () => void | Promise<void>
}

export const ChannelStatusProbeView = memo(function ChannelStatusProbeView(
  props: ChannelStatusProbeViewProps
) {
  const queryClient = useQueryClient()
  const onActionComplete = props.onActionComplete
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [onlyEnabled, setOnlyEnabled] = useState(true)
  const [groupFilter, setGroupFilter] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [search, setSearch] = useState('')
  const [configChannelId, setConfigChannelId] = useState<number | null>(null)
  const [historyChannelId, setHistoryChannelId] = useState<number | null>(null)
  const [bulkAction, setBulkAction] = useState<'enable' | 'pause' | null>(null)
  const query = useQuery({
    queryKey: ['channel-monitor', 'status-probe', { model: modelFilter }],
    queryFn: () => getChannelStatusProbeOverview(modelFilter),
    placeholderData: keepPreviousData,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
    refetchInterval: (statusProbeQuery) =>
      getChannelMonitorActiveRefetchInterval(
        statusProbeQuery.state.data?.data.channels.some(
          isChannelStatusProbeActive
        ) ?? false
      ),
  })
  const channels = query.data?.data.channels ?? EMPTY_CHANNELS
  const enabledChannels = useMemo(
    () => channels.filter((channel) => channel.config?.enabled === true),
    [channels]
  )
  const pausedChannels = useMemo(
    () =>
      channels.filter(
        (channel) => channel.config != null && channel.config.enabled === false
      ),
    [channels]
  )
  const serverNow = query.data?.data.server_now ?? Math.floor(Date.now() / 1000)
  const groupOptions = useMemo<Array<{ value: string | null; label: string }>>(
    () => [
      { value: null, label: '选择分组' },
      ...orderGroupNames(query.data?.data.groups ?? [], props.groupOrder).map(
        (groupName) => ({
          value: groupName,
          label: groupName,
        })
      ),
    ],
    [props.groupOrder, query.data?.data.groups]
  )
  const groupModels = groupFilter
    ? (query.data?.data.models_by_group?.[groupFilter] ?? EMPTY_MODEL_NAMES)
    : EMPTY_MODEL_NAMES
  const modelOptions = useMemo<
    Array<{ value: string | null; label: string }>
  >(() => {
    if (!groupFilter) {
      return [{ value: null, label: '请先选择分组' }]
    }
    if (groupModels.length === 0) {
      return [{ value: null, label: '该分组暂无探测模型' }]
    }
    return [
      { value: null, label: '全部模型' },
      ...groupModels.map((modelName) => ({
        value: modelName,
        label: modelName,
      })),
    ]
  }, [groupFilter, groupModels])
  const filteredChannels = useMemo(() => {
    const filtered = channels.filter((channel) => {
      if (onlyEnabled && channel.config?.enabled !== true) {
        return false
      }
      if (!matchesStatusFilter(channel.health_status, statusFilter)) {
        return false
      }
      if (!matchesChannelStatusProbeGroup(channel, groupFilter)) {
        return false
      }
      return matchesChannelStatusProbeSearch(channel, search)
    })
    return orderChannelsByReferenceOrder(filtered, props.channelOrder)
  }, [
    channels,
    groupFilter,
    onlyEnabled,
    props.channelOrder,
    search,
    statusFilter,
  ])
  const summary = query.data?.data.summary
  const statusCounts: Record<StatusFilter, number> = {
    all: channels.length,
    issue:
      (summary?.unhealthy ?? 0) +
      (summary?.partial ?? 0) +
      (summary?.rate_limited ?? 0),
    stale: summary?.stale ?? 0,
    healthy: summary?.healthy ?? 0,
    paused: summary?.paused ?? 0,
    unconfigured: summary?.unconfigured ?? 0,
  }
  const configChannel = channels.find(
    (channel) => channel.id === configChannelId
  )
  const historyChannel = channels.find(
    (channel) => channel.id === historyChannelId
  )
  const openHistory = useCallback((channelId: number) => {
    setHistoryChannelId(channelId)
  }, [])
  const openConfig = useCallback((channelId: number) => {
    setConfigChannelId(channelId)
  }, [])
  const refreshChannelStatus = useCallback(
    () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() }),
        queryClient.invalidateQueries({
          queryKey: ['channel-monitor', 'status-probe'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['channel-monitor', 'model-detection'],
        }),
        onActionComplete?.() ?? Promise.resolve(),
      ]).then(() => undefined),
    [onActionComplete, queryClient]
  )
  const runMutation = useMutation({
    mutationFn: (channel: ChannelStatusProbeChannel) =>
      runChannelStatusProbe(channel.id),
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('已加入立即检测队列')
      void refreshChannelStatus()
    },
  })
  const toggleMutation = useMutation({
    mutationFn: (channel: ChannelStatusProbeChannel) => {
      if (!channel.config) throw new Error('请先配置状态探测')
      return updateChannelStatusProbeConfig({
        channelId: channel.id,
        enabled: !channel.config.enabled,
        models: channel.config.models,
        intervalSeconds: channel.config.interval_seconds,
        displayValue: channel.config.display_value,
        displayUnit: channel.config.display_unit,
        recordSample: channel.config.record_sample,
        revision: channel.config.revision,
      })
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(response.data.enabled ? '周期探测已恢复' : '周期探测已暂停')
      void refreshChannelStatus()
    },
  })
  const bulkMutation = useMutation({
    mutationFn: (variables: {
      channels: ChannelStatusProbeChannel[]
      enabled: boolean
    }) =>
      Promise.allSettled(
        variables.channels.map((channel) => {
          if (!channel.config) throw new Error('请先配置状态探测')
          return updateChannelStatusProbeConfig({
            channelId: channel.id,
            enabled: variables.enabled,
            models: channel.config.models,
            intervalSeconds: channel.config.interval_seconds,
            displayValue: channel.config.display_value,
            displayUnit: channel.config.display_unit,
            recordSample: channel.config.record_sample,
            revision: channel.config.revision,
          })
        })
      ),
    onSuccess: (results, variables) => {
      const updatedCount = results.filter(
        (result) => result.status === 'fulfilled'
      ).length
      const failedCount = results.length - updatedCount
      const actionLabel = variables.enabled ? '启用' : '暂停'
      setBulkAction(null)
      void refreshChannelStatus()
      if (failedCount === 0) {
        toast.success(`已${actionLabel} ${updatedCount} 个渠道的周期探测`)
      } else if (updatedCount === 0) {
        toast.error(
          `${actionLabel}失败，${failedCount} 个渠道未更新，请刷新后重试`
        )
      } else {
        toast.error(
          `已${actionLabel} ${updatedCount} 个渠道，${failedCount} 个渠道失败，请刷新后重试`
        )
      }
    },
  })
  let pendingChannelId: number | null | undefined = null
  if (runMutation.isPending) {
    pendingChannelId = runMutation.variables?.id
  } else if (toggleMutation.isPending) {
    pendingChannelId = toggleMutation.variables?.id
  }
  const allActionsPending = bulkMutation.isPending
  const bulkActionPending =
    query.isLoading ||
    query.isFetching ||
    runMutation.isPending ||
    toggleMutation.isPending ||
    bulkMutation.isPending
  const bulkActionEnabled = bulkAction === 'enable'
  const bulkActionChannels = bulkActionEnabled
    ? pausedChannels
    : enabledChannels
  let channelGridContent: ReactNode
  if (query.isLoading) {
    channelGridContent = (
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-[28rem] rounded-lg' />
        ))}
      </div>
    )
  } else if (query.isError) {
    channelGridContent = (
      <Empty className='min-h-80 border'>
        <EmptyHeader>
          <EmptyTitle>状态探测数据加载失败</EmptyTitle>
          <EmptyDescription>请检查服务状态后重试</EmptyDescription>
        </EmptyHeader>
        <Button type='button' variant='outline' onClick={() => query.refetch()}>
          <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
          重试
        </Button>
      </Empty>
    )
  } else if (filteredChannels.length === 0) {
    channelGridContent = (
      <Empty className='min-h-80 border'>
        <EmptyHeader>
          <EmptyTitle>没有匹配的渠道</EmptyTitle>
          <EmptyDescription>调整筛选条件或搜索内容</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    channelGridContent = (
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {filteredChannels.map((channel) => (
          <ChannelStatusProbeCard
            key={channel.id}
            channel={channel}
            channelQueryClient={queryClient}
            serverNow={serverNow}
            actionPending={allActionsPending || pendingChannelId === channel.id}
            onOpenHistory={openHistory}
            onOpenConfig={openConfig}
            onRun={runMutation.mutate}
            onToggleEnabled={toggleMutation.mutate}
            onChannelStatusChanged={refreshChannelStatus}
          />
        ))}
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-col items-end gap-2'>
        <div
          className='no-scrollbar w-full overflow-x-auto pb-0.5'
          data-slot='status-probe-status-filters'
        >
          <ToggleGroup
            value={[statusFilter]}
            onValueChange={(values) => {
              const selected = values.find(
                (value) => value !== statusFilter
              ) as StatusFilter | undefined
              if (selected) setStatusFilter(selected)
            }}
            variant='outline'
            size='sm'
            spacing={0}
            aria-label='按探测状态筛选渠道'
            className='ml-auto w-max justify-end'
          >
            {STATUS_FILTER_OPTIONS.map(([value, label]) => (
              <ToggleGroupItem key={value} value={value} className='shrink-0'>
                {label} {statusCounts[value]}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <div
          className='flex w-full flex-col gap-2 sm:flex-row sm:flex-wrap sm:justify-end'
          data-slot='status-probe-filter-controls'
        >
          <div className='flex h-9 shrink-0 items-center gap-2 px-1'>
            <Checkbox
              id='status-probe-only-enabled'
              checked={onlyEnabled}
              onCheckedChange={(checked) => setOnlyEnabled(checked === true)}
              aria-label='仅展示已启用的状态探测卡片'
            />
            <Label
              htmlFor='status-probe-only-enabled'
              className='text-muted-foreground cursor-pointer text-sm font-normal whitespace-nowrap'
            >
              仅展示已启用
            </Label>
          </div>
          <Select
            items={groupOptions}
            value={groupFilter || null}
            onValueChange={(value) => {
              setGroupFilter(value ?? '')
              setModelFilter('')
            }}
          >
            <SelectTrigger
              className='w-full sm:w-40'
              aria-label='选择状态探测分组'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {groupOptions.map((option) => (
                  <SelectItem key={option.value ?? 'all'} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            items={modelOptions}
            value={modelFilter || null}
            onValueChange={(value) => setModelFilter(value ?? '')}
            disabled={!groupFilter || groupModels.length === 0}
          >
            <SelectTrigger
              className='w-full sm:w-48'
              aria-label='选择状态探测模型'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {modelOptions.map((option) => (
                  <SelectItem key={option.value ?? 'all'} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <InputGroup className='w-full sm:w-72'>
            <InputGroupAddon>
              <HugeiconsIcon icon={Search01Icon} />
            </InputGroupAddon>
            <InputGroupInput
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder='搜索渠道、备注或 ID'
              aria-label='搜索状态探测渠道'
            />
          </InputGroup>
          <Button
            type='button'
            variant='outline'
            className='w-full sm:w-auto'
            onClick={() => setBulkAction('enable')}
            disabled={pausedChannels.length === 0 || bulkActionPending}
            aria-label='启用所有状态探测'
          >
            {bulkMutation.isPending && bulkMutation.variables.enabled ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={PlayIcon} data-icon='inline-start' />
            )}
            启用所有
          </Button>
          <Button
            type='button'
            variant='outline'
            className='w-full sm:w-auto'
            onClick={() => setBulkAction('pause')}
            disabled={enabledChannels.length === 0 || bulkActionPending}
            aria-label='暂停所有状态探测'
          >
            {bulkMutation.isPending && !bulkMutation.variables.enabled ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={PauseIcon} data-icon='inline-start' />
            )}
            暂停所有
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon'
            onClick={() => query.refetch()}
            disabled={query.isFetching}
            aria-label='刷新状态探测数据'
          >
            <HugeiconsIcon
              icon={Refresh01Icon}
              className={query.isFetching ? 'animate-spin' : undefined}
            />
          </Button>
        </div>
      </div>

      {channelGridContent}

      <AlertDialog
        open={bulkAction != null}
        onOpenChange={(open) => {
          if (!open && !bulkMutation.isPending) setBulkAction(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {bulkActionEnabled ? '启用全部周期探测？' : '暂停全部周期探测？'}
            </AlertDialogTitle>
            <AlertDialogDescription className='space-y-2'>
              <span className='block'>
                将{bulkActionEnabled ? '启用' : '暂停'}全部{' '}
                {bulkActionChannels.length} 个
                {bulkActionEnabled ? '已配置但暂停' : '已启用'}
                渠道的周期探测，不受当前筛选条件影响。
              </span>
              {!bulkActionEnabled && (
                <span className='block'>
                  当前正在运行或已手动排队的任务不会被取消。
                </span>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkMutation.isPending}>
              返回
            </AlertDialogCancel>
            <AlertDialogAction
              variant={bulkActionEnabled ? 'default' : 'destructive'}
              disabled={
                bulkActionChannels.length === 0 || bulkMutation.isPending
              }
              onClick={() =>
                bulkMutation.mutate({
                  channels: bulkActionChannels,
                  enabled: bulkActionEnabled,
                })
              }
            >
              {bulkMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon
                  icon={bulkActionEnabled ? PlayIcon : PauseIcon}
                  data-icon='inline-start'
                />
              )}
              {bulkActionEnabled ? '确认启用' : '确认暂停'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {configChannel && (
        <Suspense fallback={null}>
          <LazyChannelStatusProbeConfigSheet
            key={`${configChannel.id}:${configChannel.config?.revision ?? 0}`}
            channel={configChannel}
            open
            onSaved={refreshChannelStatus}
            onOpenChange={(open) => {
              if (!open) setConfigChannelId(null)
            }}
          />
        </Suspense>
      )}
      {historyChannel && (
        <Suspense fallback={null}>
          <LazyChannelStatusProbeHistorySheet
            key={historyChannel.id}
            channel={historyChannel}
            open
            actionPending={
              allActionsPending || pendingChannelId === historyChannel.id
            }
            onOpenChange={(open) => {
              if (!open) setHistoryChannelId(null)
            }}
            onOpenConfig={() => {
              setHistoryChannelId(null)
              setConfigChannelId(historyChannel.id)
            }}
            onRun={() => runMutation.mutate(historyChannel)}
          />
        </Suspense>
      )}
    </div>
  )
})
