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
  CheckmarkCircle02Icon,
  FingerPrintScanIcon,
  InformationCircleIcon,
  PauseIcon,
  PlayIcon,
  Refresh01Icon,
  Search01Icon,
  Settings02Icon,
  WifiOff01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { QueryClient } from '@tanstack/react-query'
import { useMemo, useState, type ReactNode } from 'react'

import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { orderGroupNames } from '@/lib/group-order'

import {
  CHANNEL_MODEL_DETECTION_STATUS_FILTERS,
  channelModelDetectionPresetLabel,
  filterChannelModelDetectionChannels,
  formatChannelModelDetectionRelativeTime,
} from '../lib/model-detection'
import { orderChannelsByReferenceOrder } from '../lib/sort'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionFilters,
  ChannelModelDetectionHealth,
  ChannelModelDetectionOverview,
  ChannelModelDetectionStatusFilter,
} from '../types-model-detection'
import { ChannelModelDetectionCard } from './channel-model-detection-card'

const DEFAULT_FILTERS: ChannelModelDetectionFilters = {
  status: 'all',
  group: '',
  model: '',
  search: '',
  onlyEnabled: true,
}

export type ChannelModelDetectionViewProps = {
  channelOrder: readonly number[]
  channelQueryClient?: QueryClient
  groupOrder?: readonly string[]
  overview?: ChannelModelDetectionOverview
  loading?: boolean
  refreshing?: boolean
  error?: string | null
  actionPendingChannelId?: number | null
  actionPendingAll?: boolean
  bulkActionPending?: 'run' | 'enable' | 'pause' | null
  bulkActionDisabled?: boolean
  filters?: ChannelModelDetectionFilters
  onFiltersChange?: (filters: ChannelModelDetectionFilters) => void
  onRefresh?: () => void
  onTestDetector?: () => void
  onOpenSettings?: () => void
  onOpenHistory?: (channel: ChannelModelDetectionChannel) => void
  onOpenConfig?: (channel: ChannelModelDetectionChannel) => void
  onOpenManualRun?: (channel: ChannelModelDetectionChannel) => void
  onCancelRun?: (channel: ChannelModelDetectionChannel) => void
  onToggleSchedule?: (channel: ChannelModelDetectionChannel) => void
  onChannelStatusChanged?: () => void | Promise<void>
  onActionComplete?: () => void | Promise<void>
  onRunEnabled?: () => void
  onEnableAll?: () => void
  onPauseAll?: () => void
  settingsSurface?: ReactNode
  channelSurface?: ReactNode
  historySurface?: ReactNode
}

function noop() {}

function LoadingState() {
  return (
    <div className='flex flex-col gap-4' aria-label='正在加载模型检测数据'>
      <Skeleton className='h-16 w-full rounded-lg' />
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className='h-[28rem] w-full rounded-lg' />
        ))}
      </div>
    </div>
  )
}

function DetectorStatus(
  props: ChannelModelDetectionViewProps & {
    overview: ChannelModelDetectionOverview
  }
) {
  const detector = props.overview.detector
  let icon = CheckmarkCircle02Icon
  let title = detector.busy ? '官方检测器运行中' : '官方检测器可用'
  let description = detector.last_checked_at
    ? `最近检查 ${formatChannelModelDetectionRelativeTime(detector.last_checked_at, props.overview.server_now)}`
    : '尚未完成连接检查'
  let destructive = false

  if (detector.state === 'degraded') {
    icon = Alert02Icon
    title = '官方检测器部分能力不可用'
    description = detector.compatibility_message || '部分档位可能暂不可用'
  } else if (detector.state === 'offline') {
    icon = WifiOff01Icon
    title = '官方检测器离线'
    description = detector.last_error || '无法连接独立部署的官方检测器'
    destructive = true
  } else if (detector.state === 'incompatible') {
    icon = Alert02Icon
    title = '官方检测器接口不兼容'
    description =
      detector.compatibility_message || '新任务已暂停，历史记录仍可查看'
    destructive = true
  } else if (detector.state === 'unconfigured') {
    icon = Settings02Icon
    title = '尚未配置官方检测器地址'
    description = '配置独立部署的检测器地址后才能开始检测'
  } else if (detector.state === 'unknown') {
    icon = InformationCircleIcon
    title = '官方检测器尚未检查'
    description = '已保存检测器地址，尚未取得健康检查结果'
  }

  return (
    <Alert
      variant={destructive ? 'destructive' : 'default'}
      data-detector-state={detector.state}
    >
      <HugeiconsIcon icon={icon} aria-hidden='true' />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        {description}
        {detector.detector_url_masked && ` · ${detector.detector_url_masked}`}
        {detector.deployment_id && ` · 部署 ${detector.deployment_id}`}
      </AlertDescription>
      {(props.onTestDetector || props.onOpenSettings) && (
        <AlertAction className='flex gap-1'>
          {props.onTestDetector && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    onClick={props.onTestDetector}
                    aria-label='重新检查官方检测器'
                  />
                }
              >
                <HugeiconsIcon icon={Refresh01Icon} />
              </TooltipTrigger>
              <TooltipContent>重新检查官方检测器</TooltipContent>
            </Tooltip>
          )}
          {props.onOpenSettings && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    onClick={props.onOpenSettings}
                    aria-label='打开模型检测统一设置'
                  />
                }
              >
                <HugeiconsIcon icon={Settings02Icon} />
              </TooltipTrigger>
              <TooltipContent>打开模型检测统一设置</TooltipContent>
            </Tooltip>
          )}
        </AlertAction>
      )}
    </Alert>
  )
}

function statusCounts(
  channels: ChannelModelDetectionChannel[],
  summary: Record<ChannelModelDetectionHealth, number>
) {
  return {
    all: channels.length,
    issue: summary.unhealthy + summary.detector_unavailable,
    attention: summary.attention + summary.stale,
    running: summary.running,
    healthy: summary.healthy,
    paused: summary.paused,
    unconfigured: summary.unconfigured,
  } satisfies Record<ChannelModelDetectionStatusFilter, number>
}

export function ChannelModelDetectionView(
  props: ChannelModelDetectionViewProps
) {
  const [localFilters, setLocalFilters] = useState(DEFAULT_FILTERS)
  const filters = props.filters ?? localFilters
  const overview = props.overview

  const visibleChannels = useMemo(() => {
    if (!overview) return []
    return orderChannelsByReferenceOrder(
      filterChannelModelDetectionChannels(overview.channels, filters),
      props.channelOrder
    )
  }, [filters, overview, props.channelOrder])

  if (props.loading && !overview) return <LoadingState />

  if (props.error && !overview) {
    return (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>模型检测数据加载失败</EmptyTitle>
          <EmptyDescription>{props.error}</EmptyDescription>
        </EmptyHeader>
        {props.onRefresh && (
          <Button type='button' variant='outline' onClick={props.onRefresh}>
            重新加载
          </Button>
        )}
      </Empty>
    )
  }

  if (!overview) {
    return (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={FingerPrintScanIcon} aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>暂无模型检测数据</EmptyTitle>
          <EmptyDescription>等待模型检测总览数据</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const setFilters = (next: ChannelModelDetectionFilters) => {
    if (!props.filters) setLocalFilters(next)
    props.onFiltersChange?.(next)
  }
  const counts = statusCounts(overview.channels, overview.summary)
  const groupOptions = [
    { value: null, label: '全部分组' },
    ...orderGroupNames(overview.groups, props.groupOrder).map((group) => ({
      value: group,
      label: group,
    })),
  ]
  const availableModels = filters.group
    ? (overview.models_by_group[filters.group] ?? [])
    : overview.models
  const modelOptions = [
    { value: null, label: '全部模型' },
    ...availableModels.map((model) => ({ value: model, label: model })),
  ]
  const hasActiveRun = overview.channels.some((channel) => channel.active_run)
  const scheduledChannels = overview.channels.filter(
    (channel) => channel.config?.schedule_enabled === true
  )
  const runnableScheduledChannelCount = scheduledChannels.filter(
    (channel) => !channel.active_run
  ).length
  const detectorBlocked =
    overview.detector.state === 'offline' ||
    overview.detector.state === 'incompatible' ||
    overview.detector.state === 'unconfigured'
  const pausedChannelCount = overview.channels.filter(
    (channel) =>
      channel.config?.schedule_enabled === false &&
      channel.targets.some((target) => target.enabled)
  ).length
  let channelGridContent: ReactNode
  if (overview.channels.length === 0) {
    channelGridContent = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={FingerPrintScanIcon} aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>暂无渠道</EmptyTitle>
          <EmptyDescription>
            创建渠道后会在这里显示模型检测卡片
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else if (visibleChannels.length === 0) {
    channelGridContent = (
      <Empty className='min-h-64 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Search01Icon} aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>没有匹配的渠道</EmptyTitle>
          <EmptyDescription>调整筛选条件或搜索内容</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    channelGridContent = (
      <div
        className='grid min-w-0 grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'
        data-slot='model-detection-card-grid'
      >
        {visibleChannels.map((channel) => (
          <ChannelModelDetectionCard
            key={channel.id}
            channel={channel}
            channelQueryClient={props.channelQueryClient}
            detectorState={overview.detector.state}
            scheduledPreset={overview.settings.scheduled_preset}
            scheduleEnabled={overview.settings.schedule_enabled}
            displayValue={overview.settings.display_value}
            displayUnit={overview.settings.display_unit}
            nextBatchAt={overview.settings.next_batch_at}
            serverNow={overview.server_now}
            actionPending={
              props.actionPendingAll ||
              props.actionPendingChannelId === channel.id
            }
            onOpenHistory={props.onOpenHistory ?? noop}
            onOpenConfig={props.onOpenConfig ?? noop}
            onOpenManualRun={props.onOpenManualRun ?? noop}
            onCancelRun={props.onCancelRun ?? noop}
            onToggleSchedule={props.onToggleSchedule ?? noop}
            onChannelStatusChanged={props.onChannelStatusChanged}
          />
        ))}
      </div>
    )
  }

  return (
    <div
      className='flex min-w-0 flex-col gap-4'
      data-slot='model-detection-view'
      data-has-active-run={hasActiveRun || undefined}
    >
      <DetectorStatus {...props} overview={overview} />

      <div className='flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <div className='text-muted-foreground min-w-0 text-sm'>
          <span className='text-foreground font-medium'>
            定时：
            {overview.settings.schedule_enabled
              ? channelModelDetectionPresetLabel(
                  overview.settings.scheduled_preset
                )
              : '已关闭'}
          </span>
          <span>
            {' '}
            · 每{' '}
            {overview.settings.interval_minutes % 60 === 0
              ? `${overview.settings.interval_minutes / 60} 小时`
              : `${overview.settings.interval_minutes} 分钟`}
          </span>
          <span>
            {' '}
            · 下批{' '}
            {formatChannelModelDetectionRelativeTime(
              overview.settings.next_batch_at,
              overview.server_now
            )}
          </span>
        </div>
        <div
          className='grid w-full shrink-0 grid-cols-2 gap-2 sm:flex sm:w-auto'
          data-slot='model-detection-bulk-actions'
        >
          {props.onRunEnabled && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='flex-1 sm:flex-none'
              onClick={props.onRunEnabled}
              disabled={
                runnableScheduledChannelCount === 0 ||
                detectorBlocked ||
                props.bulkActionPending != null ||
                props.bulkActionDisabled
              }
              aria-label='批量执行已启用模型检测'
            >
              {props.bulkActionPending === 'run' ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon
                  icon={FingerPrintScanIcon}
                  data-icon='inline-start'
                />
              )}
              执行已启用
            </Button>
          )}
          {props.onEnableAll && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='flex-1 sm:flex-none'
              onClick={props.onEnableAll}
              disabled={
                pausedChannelCount === 0 ||
                props.bulkActionPending != null ||
                props.bulkActionDisabled
              }
              aria-label='启用所有模型定时检测'
            >
              {props.bulkActionPending === 'enable' ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={PlayIcon} data-icon='inline-start' />
              )}
              启用所有
            </Button>
          )}
          {props.onPauseAll && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='flex-1 sm:flex-none'
              onClick={props.onPauseAll}
              disabled={
                scheduledChannels.length === 0 ||
                props.bulkActionPending != null ||
                props.bulkActionDisabled
              }
              aria-label='暂停所有模型定时检测'
            >
              {props.bulkActionPending === 'pause' ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={PauseIcon} data-icon='inline-start' />
              )}
              暂停所有
            </Button>
          )}
          {props.onOpenSettings && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='flex-1 sm:flex-none'
              onClick={props.onOpenSettings}
            >
              <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
              统一设置
            </Button>
          )}
        </div>
      </div>

      <div className='flex flex-col items-end gap-2'>
        <div
          className='no-scrollbar w-full overflow-x-auto pb-0.5'
          data-slot='model-detection-status-filters'
        >
          <ToggleGroup
            value={[filters.status]}
            onValueChange={(values) => {
              const selected = values.find(
                (value) => value !== filters.status
              ) as ChannelModelDetectionStatusFilter | undefined
              if (selected) setFilters({ ...filters, status: selected })
            }}
            variant='outline'
            size='sm'
            spacing={0}
            aria-label='按模型检测状态筛选渠道'
            className='ml-auto w-max justify-end'
          >
            {CHANNEL_MODEL_DETECTION_STATUS_FILTERS.map(([value, label]) => (
              <ToggleGroupItem key={value} value={value} className='shrink-0'>
                {label} {counts[value]}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <div
          className='flex w-full flex-col gap-2 sm:flex-row sm:flex-wrap sm:justify-end'
          data-slot='model-detection-filter-controls'
        >
          <div className='flex h-9 shrink-0 items-center gap-2 px-1'>
            <Checkbox
              id='model-detection-only-enabled'
              checked={filters.onlyEnabled}
              onCheckedChange={(checked) =>
                setFilters({ ...filters, onlyEnabled: checked === true })
              }
              aria-label='仅展示已启用的模型检测卡片'
            />
            <Label
              htmlFor='model-detection-only-enabled'
              className='text-muted-foreground cursor-pointer text-sm font-normal whitespace-nowrap'
            >
              仅展示已启用
            </Label>
          </div>
          <Select
            items={groupOptions}
            value={filters.group || null}
            onValueChange={(value) =>
              setFilters({ ...filters, group: value ?? '', model: '' })
            }
          >
            <SelectTrigger
              className='w-full min-w-0 sm:w-40'
              aria-label='选择模型检测分组'
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
            value={filters.model || null}
            onValueChange={(value) =>
              setFilters({ ...filters, model: value ?? '' })
            }
          >
            <SelectTrigger
              className='w-full min-w-0 sm:w-48'
              aria-label='选择模型检测请求模型'
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
          <InputGroup className='w-full min-w-0 sm:w-72'>
            <InputGroupAddon>
              <HugeiconsIcon icon={Search01Icon} />
            </InputGroupAddon>
            <InputGroupInput
              value={filters.search}
              onChange={(event) =>
                setFilters({ ...filters, search: event.target.value })
              }
              placeholder='搜索渠道、备注或 ID'
              aria-label='搜索模型检测渠道'
            />
          </InputGroup>
          {props.onRefresh && (
            <Button
              type='button'
              variant='outline'
              size='icon'
              onClick={props.onRefresh}
              disabled={props.refreshing}
              aria-label='刷新模型检测数据'
            >
              <HugeiconsIcon
                icon={Refresh01Icon}
                className={props.refreshing ? 'animate-spin' : undefined}
              />
            </Button>
          )}
        </div>
      </div>

      {props.error && (
        <Alert variant='destructive'>
          <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
          <AlertTitle>部分模型检测数据刷新失败</AlertTitle>
          <AlertDescription>{props.error}</AlertDescription>
        </Alert>
      )}

      {channelGridContent}

      {props.settingsSurface}
      {props.channelSurface}
      {props.historySurface}
    </div>
  )
}
