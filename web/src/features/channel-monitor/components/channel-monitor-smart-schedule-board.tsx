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
  Cancel01Icon,
  HistoryIcon,
  PinIcon,
  Refresh01Icon,
  Route01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { compareChannelStatusesEnabledFirst } from '@/features/channels/lib/channel-status-order'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  type ChannelMonitorSmartSchedulePrimaryUpdateRequest,
  ChannelMonitorSmartScheduleStabilityConfirmationRequiredError,
  runChannelMonitorSmartSchedule,
  updateChannelMonitorSmartScheduleGroupPause,
  updateChannelMonitorSmartScheduleManualRouting,
  updateChannelMonitorSmartScheduleRoutePrimary,
  updateChannelMonitorSmartScheduleRouteConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { formatMonitorRatio } from '../lib/format'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
  CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
  CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
} from '../lib/query-options'
import { compareChannelMonitorSmartScheduleModels } from '../lib/smart-schedule-model-order'
import {
  channelMonitorSmartSchedulePrimaryRequiresConfirmation,
  createChannelMonitorSmartSchedulePrimaryFormState,
} from '../lib/smart-schedule-primary'
import {
  channelMonitorSmartScheduleRouteKey,
  compareChannelMonitorSmartScheduleGroupsByRatio,
  filterChannelMonitorSmartScheduleRoutes,
  isChannelMonitorSmartScheduleResultStale,
  placeChannelMonitorSmartScheduleRoutes,
  summarizeChannelMonitorSmartScheduleOverview,
  summarizeChannelMonitorSmartSchedulePools,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteResult,
  ChannelMonitorSmartScheduleRouteStability,
  ChannelMonitorSmartScheduleSampleItem,
} from '../types'
import { ChannelMonitorSmartScheduleClearDialog } from './channel-monitor-smart-schedule-clear-dialog'
import {
  ChannelMonitorSmartSchedulePool,
  type ChannelMonitorSmartSchedulePoolView,
} from './channel-monitor-smart-schedule-pool'
import { ChannelMonitorSmartSchedulePrimaryConfirmDialog } from './channel-monitor-smart-schedule-primary-confirm-dialog'
import { ChannelMonitorSmartSchedulePrimaryStabilityField } from './channel-monitor-smart-schedule-primary-controls'

type ChannelMonitorSmartScheduleBoardProps = {
  active: boolean
  result: ChannelMonitorSmartScheduleRouteResult | undefined
  channels: readonly ChannelMonitorItem[]
  groupPolicies: readonly ChannelMonitorSmartScheduleGroupPolicy[]
  groupRatios: Readonly<Record<string, number>>
  intervalMinutes: number
  isLoading: boolean
  isError: boolean
  onOpenSettings: () => void
  onOpenHistory: () => void
  selection?: {
    group: string
    model: string
  }
  onSelectionChange?: (selection: { group: string; model: string }) => void
}

type PrimaryMutationVariables = {
  request: ChannelMonitorSmartSchedulePrimaryUpdateRequest
  route: ChannelMonitorSmartScheduleRoute
}

const EMPTY_ROUTES: readonly ChannelMonitorSmartScheduleRoute[] = []

function channelMonitorSmartScheduleSampleKey(
  channelId: number,
  model: string
) {
  return `${channelId}\u0000${model}`
}

export function ChannelMonitorSmartScheduleBoard(
  props: ChannelMonitorSmartScheduleBoardProps
) {
  const queryClient = useQueryClient()
  const [selectedGroup, setSelectedGroup] = useState('')
  const [selectedModel, setSelectedModel] = useState('')
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const [primaryTarget, setPrimaryTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const [primaryConfirmation, setPrimaryConfirmation] =
    useState<PrimaryMutationVariables | null>(null)
  const [primaryDuration, setPrimaryDuration] = useState('60')
  const [allowPrimaryStabilityDegrade, setAllowPrimaryStabilityDegrade] =
    useState(true)
  const routes = useMemo(
    () =>
      filterChannelMonitorSmartScheduleRoutes(
        props.result?.routes ?? EMPTY_ROUTES,
        props.result?.enabled === true,
        props.groupPolicies
      ),
    [props.groupPolicies, props.result?.enabled, props.result?.routes]
  )
  const placements = useMemo(
    () => placeChannelMonitorSmartScheduleRoutes(routes),
    [routes]
  )
  const summary = useMemo(
    () => summarizeChannelMonitorSmartScheduleOverview(routes),
    [routes]
  )
  const groups = useMemo(
    () =>
      [...new Set(routes.map((route) => route.group))].sort((first, second) =>
        compareChannelMonitorSmartScheduleGroupsByRatio(
          first,
          second,
          props.groupRatios
        )
      ),
    [props.groupRatios, routes]
  )
  const poolSummaries = useMemo(
    () => summarizeChannelMonitorSmartSchedulePools(routes, props.groupRatios),
    [props.groupRatios, routes]
  )
  const policyByGroup = useMemo(
    () => new Map(props.groupPolicies.map((policy) => [policy.group, policy])),
    [props.groupPolicies]
  )
  const channelsById = useMemo(
    () => new Map(props.channels.map((channel) => [channel.id, channel])),
    [props.channels]
  )
  const performanceByRoute = useMemo(
    () =>
      new Map<string, ChannelMonitorSmartScheduleRoutePerformance>(
        (props.result?.performance_items ?? []).map((item) => [
          channelMonitorSmartScheduleRouteKey(item),
          item,
        ])
      ),
    [props.result?.performance_items]
  )
  const businessPerformanceByRoute = useMemo(
    () =>
      new Map<string, ChannelMonitorSmartScheduleRoutePerformance>(
        (props.result?.business_performance_items ?? []).map((item) => [
          channelMonitorSmartScheduleRouteKey(item),
          item,
        ])
      ),
    [props.result?.business_performance_items]
  )
  const samplesByModel = useMemo(
    () =>
      new Map<string, ChannelMonitorSmartScheduleSampleItem>(
        (props.result?.sample_items ?? []).map((item) => [
          channelMonitorSmartScheduleSampleKey(item.channel_id, item.model),
          item,
        ])
      ),
    [props.result?.sample_items]
  )
  const stabilityByRoute = useMemo(
    () =>
      new Map<string, ChannelMonitorSmartScheduleRouteStability>(
        (props.result?.stability_items ?? []).map((item) => [
          channelMonitorSmartScheduleRouteKey(item),
          item,
        ])
      ),
    [props.result?.stability_items]
  )
  const pools = useMemo(() => {
    const routesByPool = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
    for (const route of routes) {
      const key = `${route.group}\u0000${route.model}`
      const poolRoutes = routesByPool.get(key)
      if (poolRoutes) poolRoutes.push(route)
      else routesByPool.set(key, [route])
    }
    return poolSummaries
      .map<ChannelMonitorSmartSchedulePoolView>((poolSummary) => ({
        summary: poolSummary,
        routes: (
          routesByPool.get(`${poolSummary.group}\u0000${poolSummary.model}`) ??
          []
        ).sort((first, second) => {
          const statusOrder = compareChannelStatusesEnabledFirst(
            first.channel_status,
            second.channel_status
          )
          if (statusOrder !== 0) return statusOrder

          const firstRatio = channelsById.get(first.channel_id)?.cost_ratio
          const secondRatio = channelsById.get(second.channel_id)?.cost_ratio
          if (firstRatio == null && secondRatio != null) return 1
          if (firstRatio != null && secondRatio == null) return -1
          if (firstRatio != null && secondRatio != null) {
            const ratioOrder = firstRatio - secondRatio
            if (ratioOrder !== 0) return ratioOrder
          }
          const nameOrder = first.channel_name.localeCompare(
            second.channel_name
          )
          return nameOrder || first.channel_id - second.channel_id
        }),
      }))
      .sort((first, second) => {
        const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
          first.summary.group,
          second.summary.group,
          props.groupRatios
        )
        if (groupOrder !== 0) return groupOrder
        return compareChannelMonitorSmartScheduleModels(
          first.summary.model,
          second.summary.model,
          policyByGroup.get(first.summary.group)?.model_order
        )
      })
  }, [channelsById, policyByGroup, poolSummaries, props.groupRatios, routes])
  const poolCountByGroup = useMemo(() => {
    const counts = new Map<string, number>()
    for (const pool of pools) {
      counts.set(pool.summary.group, (counts.get(pool.summary.group) ?? 0) + 1)
    }
    return counts
  }, [pools])
  const warningCountByGroup = useMemo(() => {
    const counts = new Map<string, number>()
    for (const pool of pools) {
      if (
        pool.summary.degradedCount === 0 &&
        pool.summary.probingCount === 0 &&
        pool.summary.insufficientSampleCount === 0 &&
        pool.summary.failedCount === 0 &&
        pool.summary.pausedCount === 0
      ) {
        continue
      }
      counts.set(pool.summary.group, (counts.get(pool.summary.group) ?? 0) + 1)
    }
    return counts
  }, [pools])
  const firstDegradedPool = pools.find((pool) => pool.summary.degradedCount > 0)
  const firstFailedPool = pools.find((pool) => pool.summary.failedCount > 0)
  const firstProbingPool = pools.find((pool) => pool.summary.probingCount > 0)
  const firstInsufficientSamplePool = pools.find(
    (pool) => pool.summary.insufficientSampleCount > 0
  )
  const firstPausedPool = pools.find((pool) => pool.summary.pausedCount > 0)
  const selectedGroupValue = props.selection?.group ?? selectedGroup
  const selectedModelValue = props.selection?.model ?? selectedModel
  const visibleGroup = groups.includes(selectedGroupValue)
    ? selectedGroupValue
    : (groups[0] ?? '')
  const visiblePools = useMemo(
    () => pools.filter((pool) => pool.summary.group === visibleGroup),
    [pools, visibleGroup]
  )
  const visibleModels = useMemo(
    () => visiblePools.map((pool) => pool.summary.model),
    [visiblePools]
  )
  const visibleModel = visibleModels.includes(selectedModelValue)
    ? selectedModelValue
    : (visibleModels[0] ?? '')
  const visiblePool =
    visiblePools.find((pool) => pool.summary.model === visibleModel) ?? null

  const updateSelection = (group: string, model: string) => {
    setSelectedGroup(group)
    setSelectedModel(model)
    props.onSelectionChange?.({ group, model })
  }
  const selectGroup = (group: string) => {
    const firstModel = pools.find((pool) => pool.summary.group === group)
    updateSelection(group, firstModel?.summary.model ?? '')
  }
  const selectPool = (pool: ChannelMonitorSmartSchedulePoolView) => {
    updateSelection(pool.summary.group, pool.summary.model)
  }
  const closePrimaryDialog = () => {
    setPrimaryConfirmation(null)
    setPrimaryTarget(null)
  }

  const invalidateSchedule = () => {
    queryClient.invalidateQueries({
      queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
    })
    queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
  }
  const updateMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleRouteConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => toast.success('路由调度设置已保存'),
    onSettled: invalidateSchedule,
  })
  const primaryMutation = useMutation({
    mutationFn: ({ request }: PrimaryMutationVariables) =>
      updateChannelMonitorSmartScheduleRoutePrimary(request),
    onError: (error, variables) => {
      if (
        error instanceof
          ChannelMonitorSmartScheduleStabilityConfirmationRequiredError &&
        variables.request.confirmStabilityOverride !== true
      ) {
        setPrimaryConfirmation(variables)
        return
      }
      handleChannelMonitorMutationError(error)
    },
    onSuccess: (response) => {
      let successMessage = '已解除主渠道固定'
      if (response.data.duration_minutes > 0) {
        successMessage = `主渠道已固定 ${response.data.duration_minutes} 分钟`
      }
      if (response.data.stability_protection_cleared) {
        successMessage = `已解除稳定性保护，主渠道固定 ${response.data.duration_minutes} 分钟`
      }
      toast.success(successMessage)
      closePrimaryDialog()
    },
    onSettled: () => {
      invalidateSchedule()
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
    },
  })
  const manualRoutingMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleManualRouting,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => toast.success('人工优先级和权重已保存'),
    onSettled: invalidateSchedule,
  })
  const groupPauseMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleGroupPause,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.duration_minutes > 0
          ? `已暂停“${response.data.group} / ${response.data.model}”路由流量 ${response.data.duration_minutes} 分钟`
          : `已恢复“${response.data.group} / ${response.data.model}”路由流量`
      )
    },
    onSettled: () => {
      invalidateSchedule()
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
    },
  })
  const runMutation = useMutation({
    mutationFn: runChannelMonitorSmartSchedule,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.created
          ? '智能调度任务已创建'
          : '已有智能调度任务正在运行'
      )
    },
    onSettled: () => {
      invalidateSchedule()
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY,
      })
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
      })
    },
  })
  const submitPrimary = () => {
    if (!primaryTarget) return
    const request: ChannelMonitorSmartSchedulePrimaryUpdateRequest = {
      channelId: primaryTarget.channel_id,
      group: primaryTarget.group,
      model: primaryTarget.model,
      durationMinutes: Number(primaryDuration),
      allowStabilityDegrade: allowPrimaryStabilityDegrade,
    }
    if (
      channelMonitorSmartSchedulePrimaryRequiresConfirmation(
        primaryTarget,
        Date.now() / 1000
      )
    ) {
      setPrimaryConfirmation({ request, route: primaryTarget })
      return
    }
    primaryMutation.mutate({ request, route: primaryTarget })
  }
  const updateRouteKey =
    updateMutation.isPending && updateMutation.variables
      ? channelMonitorSmartScheduleRouteKey({
          channel_id: updateMutation.variables.channelId,
          group: updateMutation.variables.group,
          model: updateMutation.variables.model,
        })
      : null
  const manualRoutingKey =
    manualRoutingMutation.isPending && manualRoutingMutation.variables
      ? channelMonitorSmartScheduleRouteKey({
          channel_id: manualRoutingMutation.variables.channelId,
          group: manualRoutingMutation.variables.group,
          model: manualRoutingMutation.variables.model,
        })
      : null
  const groupPauseKey =
    groupPauseMutation.isPending && groupPauseMutation.variables
      ? channelMonitorSmartScheduleRouteKey({
          channel_id: groupPauseMutation.variables.channelId,
          group: groupPauseMutation.variables.group,
          model: groupPauseMutation.variables.model,
        })
      : null
  const stale = isChannelMonitorSmartScheduleResultStale(
    props.result?.generated_at ?? 0,
    props.intervalMinutes
  )
  const metricCoverage = props.result?.metric_coverage
  const incompleteMetricWindows: string[] = []
  if (metricCoverage?.aggregation_enabled) {
    if (!metricCoverage.performance_window_complete) {
      incompleteMetricWindows.push('性能窗口覆盖不足')
    }
    if (!metricCoverage.stability_window_complete) {
      incompleteMetricWindows.push('稳定性窗口覆盖不足')
    }
  }

  if (props.isLoading) {
    return (
      <div className='flex flex-col gap-4'>
        <Skeleton className='h-20 w-full' />
        <Skeleton className='h-10 w-full' />
        <div className='grid gap-4 lg:grid-cols-2'>
          <Skeleton className='h-72 w-full' />
          <Skeleton className='h-72 w-full' />
        </div>
      </div>
    )
  }

  if (props.isError && !props.result) {
    return (
      <Empty className='min-h-72'>
        <EmptyHeader>
          <EmptyTitle>智能调度加载失败</EmptyTitle>
          <EmptyDescription>请刷新页面后重试</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <section
        className='border-border bg-muted/25 flex flex-col gap-3 border-y px-4 py-3 xl:flex-row xl:items-center xl:justify-between'
        aria-label='智能调度运行状态'
      >
        <div className='flex min-w-0 flex-1 flex-wrap items-center gap-x-5 gap-y-2'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='bg-background text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-md border'>
              <HugeiconsIcon icon={Route01Icon} aria-hidden='true' />
            </span>
            <div>
              <div className='flex flex-wrap items-center gap-2 font-medium'>
                运行状态
                <Badge
                  variant={props.result?.enabled ? 'secondary' : 'outline'}
                >
                  {props.result?.enabled ? '已启用' : '已禁用'}
                </Badge>
                {props.isError && props.result ? (
                  <Badge variant='destructive'>刷新失败，显示上次结果</Badge>
                ) : null}
                {stale ? <Badge variant='warning'>数据可能已过期</Badge> : null}
              </div>
              <div className='text-muted-foreground mt-0.5 text-xs'>
                {props.result?.generated_at
                  ? `更新于 ${formatTimestampToDate(props.result.generated_at)} · 每 ${props.intervalMinutes} 分钟调度`
                  : `每 ${props.intervalMinutes} 分钟调度`}
              </div>
            </div>
          </div>

          <div className='flex flex-wrap items-center gap-x-5 gap-y-2 text-sm'>
            <span>
              <span className='text-muted-foreground'>调度池 </span>
              <strong className='font-mono tabular-nums'>
                {summary.poolCount}
              </strong>
            </span>
            <span>
              <span className='text-muted-foreground'>参与路由 </span>
              <strong className='font-mono tabular-nums'>
                {summary.participatingCount}/{summary.routeCount}
              </strong>
            </span>
            <span>
              <span className='text-muted-foreground'>当前可调度 </span>
              <strong className='font-mono tabular-nums'>
                {summary.activeCount}
              </strong>
            </span>
          </div>
        </div>

        <div className='flex shrink-0 flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onOpenHistory}
          >
            <HugeiconsIcon icon={HistoryIcon} data-icon='inline-start' />
            智能调度记录
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onOpenSettings}
          >
            <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
            调度设置
          </Button>
          <Button
            type='button'
            size='sm'
            disabled={
              !props.active ||
              !props.result?.enabled ||
              props.isError ||
              runMutation.isPending
            }
            onClick={() => runMutation.mutate()}
          >
            {runMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            )}
            立即调度
          </Button>
        </div>
      </section>

      {metricCoverage?.aggregation_enabled &&
      incompleteMetricWindows.length > 0 ? (
        <Alert className='mx-4 w-auto'>
          <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
          <AlertTitle>调度窗口数据尚未覆盖完整</AlertTitle>
          <AlertDescription>
            {incompleteMetricWindows.join('、')}。当前分钟汇总覆盖从{' '}
            {metricCoverage.aggregated_from > 0
              ? formatTimestampToDate(metricCoverage.aggregated_from)
              : '尚未建立'}{' '}
            到{' '}
            {metricCoverage.aggregated_through > 0
              ? formatTimestampToDate(metricCoverage.aggregated_through)
              : '尚未建立'}
            ，后台正在分批补齐分钟汇总
            {!metricCoverage.configured_retention_sufficient
              ? '；保留配置短于最长调度窗口，系统会优先保留调度所需分钟汇总'
              : ''}
            。
          </AlertDescription>
        </Alert>
      ) : null}

      {props.result?.enabled &&
      (summary.degradedCount > 0 ||
        summary.failedCount > 0 ||
        summary.probingCount > 0 ||
        summary.insufficientSampleCount > 0 ||
        summary.pausedCount > 0) ? (
        <section
          className='border-border flex flex-wrap items-center gap-2 border-b px-4 pb-3'
          aria-label='当前调度状态'
        >
          <span className='text-muted-foreground mr-1 text-xs font-medium'>
            当前调度状态
          </span>
          {summary.degradedCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='destructive'
              className='cursor-pointer'
              onClick={() => {
                if (firstDegradedPool) selectPool(firstDegradedPool)
              }}
            >
              稳定性降级 {summary.degradedCount}
            </Badge>
          ) : null}
          {summary.failedCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='destructive'
              className='cursor-pointer'
              onClick={() => {
                if (firstFailedPool) selectPool(firstFailedPool)
              }}
            >
              最近调度失败 {summary.failedCount}
            </Badge>
          ) : null}
          {summary.probingCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='warning'
              className='cursor-pointer'
              onClick={() => {
                if (firstProbingPool) selectPool(firstProbingPool)
              }}
            >
              稳定性试放 {summary.probingCount}
            </Badge>
          ) : null}
          {summary.insufficientSampleCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='warning'
              className='cursor-pointer'
              onClick={() => {
                if (firstInsufficientSamplePool) {
                  selectPool(firstInsufficientSamplePool)
                }
              }}
            >
              统一采样 {summary.insufficientSampleCount}
            </Badge>
          ) : null}
          {summary.pausedCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='warning'
              className='cursor-pointer'
              onClick={() => {
                if (firstPausedPool) selectPool(firstPausedPool)
              }}
            >
              流量已暂停 {summary.pausedCount}
            </Badge>
          ) : null}
        </section>
      ) : null}

      {!props.result?.enabled ? (
        <Empty className='min-h-72'>
          <EmptyHeader>
            <EmptyTitle>智能调度尚未启用</EmptyTitle>
            <EmptyDescription>
              启用后将按照分组和模型建立独立调度池
            </EmptyDescription>
          </EmptyHeader>
          <Button type='button' onClick={props.onOpenSettings}>
            <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
            打开调度设置
          </Button>
        </Empty>
      ) : null}

      {props.result?.enabled && groups.length === 0 ? (
        <Empty className='min-h-72'>
          <EmptyHeader>
            <EmptyTitle>暂无智能调度路由</EmptyTitle>
            <EmptyDescription>请先在调度设置中配置分组和模型</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : null}

      {props.result?.enabled && groups.length > 0 ? (
        <div className='grid items-start gap-4 xl:grid-cols-[16rem_minmax(0,1fr)]'>
          <aside className='grid min-w-0 gap-3 xl:sticky xl:top-4'>
            <div className='grid min-w-0 gap-2'>
              <div className='text-muted-foreground px-1 text-xs font-medium'>
                1. 选择分组
              </div>
              <nav
                className='flex gap-2 overflow-x-auto pb-1 xl:flex-col xl:overflow-visible xl:pb-0'
                aria-label='智能调度分组'
              >
                {groups.map((group) => {
                  const selected = group === visibleGroup
                  const warningCount = warningCountByGroup.get(group) ?? 0
                  return (
                    <button
                      key={group}
                      type='button'
                      className={cn(
                        'focus-visible:ring-ring/50 flex min-h-12 shrink-0 items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm outline-none transition-colors focus-visible:ring-3',
                        selected
                          ? 'border-foreground/20 bg-muted text-foreground'
                          : 'bg-background hover:bg-muted/50'
                      )}
                      aria-pressed={selected}
                      onClick={() => selectGroup(group)}
                    >
                      <span className='min-w-0 flex-1 text-left'>
                        <span
                          className='block truncate font-medium'
                          title={group}
                        >
                          {group}
                        </span>
                        <span
                          className={cn(
                            'block font-mono text-xs tabular-nums',
                            selected
                              ? 'text-foreground/70'
                              : 'text-muted-foreground'
                          )}
                        >
                          x{formatMonitorRatio(props.groupRatios[group] ?? 1)} ·{' '}
                          {poolCountByGroup.get(group) ?? 0} 池
                        </span>
                      </span>
                      {warningCount > 0 ? (
                        <span
                          className={cn(
                            'flex size-5 items-center justify-center rounded-full text-[11px] font-medium tabular-nums',
                            selected
                              ? 'bg-warning/15 text-warning'
                              : 'bg-warning/10 text-warning'
                          )}
                          aria-label={`${warningCount} 个调度池需要关注`}
                        >
                          {warningCount}
                        </span>
                      ) : null}
                    </button>
                  )
                })}
              </nav>
            </div>
          </aside>

          <div className='grid min-w-0 gap-4'>
            <section
              className='border-border grid min-w-0 gap-2 border-b pb-3'
              aria-label={`${visibleGroup} 分组下的模型选择`}
            >
              <div className='flex items-center justify-between gap-2 px-1'>
                <span className='min-w-0 truncate text-sm font-medium'>
                  选择模型
                  <span className='text-muted-foreground'>
                    {' '}
                    · {visibleGroup}
                  </span>
                </span>
                <span className='text-muted-foreground font-mono text-xs tabular-nums'>
                  {visibleModels.length} 个模型
                </span>
              </div>
              <nav
                className='flex gap-2 overflow-x-auto pb-1 xl:max-h-32 xl:flex-wrap xl:overflow-y-auto xl:pr-1 xl:pb-0'
                aria-label={`${visibleGroup} 分组下的智能调度模型`}
              >
                {visiblePools.map((pool) => {
                  const selected = pool.summary.model === visibleModel
                  const needsAttention =
                    pool.summary.degradedCount > 0 ||
                    pool.summary.probingCount > 0 ||
                    pool.summary.insufficientSampleCount > 0 ||
                    pool.summary.failedCount > 0 ||
                    pool.summary.pausedCount > 0
                  return (
                    <button
                      key={pool.summary.model}
                      type='button'
                      className={cn(
                        'focus-visible:ring-ring/50 flex min-h-12 min-w-48 shrink-0 items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm outline-none transition-colors focus-visible:ring-3',
                        selected
                          ? 'border-foreground/20 bg-muted text-foreground'
                          : 'bg-background hover:bg-muted/50'
                      )}
                      aria-pressed={selected}
                      onClick={() => selectPool(pool)}
                    >
                      <span className='min-w-0 flex-1 text-left'>
                        <span
                          className='block truncate font-medium'
                          title={pool.summary.model}
                        >
                          {pool.summary.model}
                        </span>
                        <span
                          className={cn(
                            'block text-xs tabular-nums',
                            selected
                              ? 'text-foreground/70'
                              : 'text-muted-foreground'
                          )}
                        >
                          可调度 {pool.summary.activeCount}/
                          {pool.summary.participatingCount}
                        </span>
                      </span>
                      {needsAttention ? (
                        <span
                          className='bg-warning size-2 shrink-0 rounded-full'
                          aria-label='需要关注'
                        />
                      ) : null}
                    </button>
                  )
                })}
              </nav>
            </section>

            {visiblePool ? (
              <ChannelMonitorSmartSchedulePool
                key={`${visiblePool.summary.group}\u0000${visiblePool.summary.model}`}
                pool={visiblePool}
                policy={policyByGroup.get(visiblePool.summary.group)}
                channelsById={channelsById}
                placements={placements}
                performanceByRoute={performanceByRoute}
                businessPerformanceByRoute={businessPerformanceByRoute}
                stabilityByRoute={stabilityByRoute}
                samplesByModel={samplesByModel}
                updateRouteKey={updateRouteKey}
                manualRoutingKey={manualRoutingKey}
                groupPauseKey={groupPauseKey}
                updateDisabled={
                  updateMutation.isPending ||
                  primaryMutation.isPending ||
                  manualRoutingMutation.isPending ||
                  groupPauseMutation.isPending
                }
                onParticipationChange={(route, checked) =>
                  updateMutation.mutate({
                    channelId: route.channel_id,
                    group: route.group,
                    model: route.model,
                    excluded: !checked,
                  })
                }
                onClearProtection={setClearTarget}
                onSetPrimary={(route) => {
                  const formState =
                    createChannelMonitorSmartSchedulePrimaryFormState(
                      route,
                      Date.now() / 1000
                    )
                  setPrimaryDuration(formState.durationMinutes)
                  setAllowPrimaryStabilityDegrade(
                    formState.allowStabilityDegrade
                  )
                  setPrimaryTarget(route)
                }}
                onClearPrimary={(route) =>
                  primaryMutation.mutate({
                    route,
                    request: {
                      channelId: route.channel_id,
                      group: route.group,
                      model: route.model,
                      durationMinutes: 0,
                      allowStabilityDegrade: false,
                    },
                  })
                }
                onSaveManualRouting={(route, priority, weight) =>
                  manualRoutingMutation.mutate({
                    channelId: route.channel_id,
                    group: route.group,
                    model: route.model,
                    priority,
                    weight,
                  })
                }
                onGroupPauseChange={(route, durationMinutes) =>
                  groupPauseMutation.mutate({
                    channelId: route.channel_id,
                    group: route.group,
                    model: route.model,
                    durationMinutes,
                  })
                }
              />
            ) : (
              <Empty className='min-h-72'>
                <EmptyHeader>
                  <EmptyTitle>当前分组暂无模型池</EmptyTitle>
                  <EmptyDescription>
                    请检查该分组的模型和渠道配置
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
          </div>
        </div>
      ) : null}

      <ChannelMonitorSmartScheduleClearDialog
        route={clearTarget}
        onOpenChange={(open) => {
          if (!open) setClearTarget(null)
        }}
      />
      {primaryTarget ? (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !primaryMutation.isPending) closePrimaryDialog()
          }}
        >
          <DialogContent className='sm:max-w-md'>
            <DialogHeader>
              <DialogTitle>
                {primaryTarget.state.manual_primary_until > 0
                  ? '重新设置固定时长'
                  : '固定主渠道'}
              </DialogTitle>
              <DialogDescription>
                {primaryTarget.channel_name}{' '}
                将在当前分组和模型中优先承接请求。固定期间仍会继续采集和计算评分；渠道禁用或退出参与仍会立即停止该路由接收请求。
                {primaryTarget.state.manual_primary_until > 0
                  ? ` 当前固定至 ${formatTimestampToDate(primaryTarget.state.manual_primary_until)}。`
                  : null}
              </DialogDescription>
            </DialogHeader>
            <FieldGroup className='gap-3'>
              <Field>
                <FieldLabel htmlFor='channel-monitor-manual-primary-duration'>
                  固定时长
                </FieldLabel>
                <div className='flex items-center gap-2'>
                  <Input
                    id='channel-monitor-manual-primary-duration'
                    type='number'
                    min={1}
                    max={525600}
                    value={primaryDuration}
                    onChange={(event) => setPrimaryDuration(event.target.value)}
                    aria-label='固定时长（分钟）'
                  />
                  <span className='text-muted-foreground text-sm'>分钟</span>
                </div>
              </Field>
              <ChannelMonitorSmartSchedulePrimaryStabilityField
                checked={allowPrimaryStabilityDegrade}
                onCheckedChange={setAllowPrimaryStabilityDegrade}
              />
            </FieldGroup>
            <DialogFooter>
              <Button
                variant='outline'
                disabled={primaryMutation.isPending}
                onClick={closePrimaryDialog}
              >
                <HugeiconsIcon icon={Cancel01Icon} data-icon='inline-start' />
                取消
              </Button>
              <Button
                disabled={
                  primaryMutation.isPending ||
                  !Number.isInteger(Number(primaryDuration)) ||
                  Number(primaryDuration) < 1 ||
                  Number(primaryDuration) > 525600
                }
                onClick={submitPrimary}
              >
                {primaryMutation.isPending ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon icon={PinIcon} data-icon='inline-start' />
                )}
                {primaryTarget.state.manual_primary_until > 0
                  ? '更新固定时长'
                  : '固定主渠道'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
      <ChannelMonitorSmartSchedulePrimaryConfirmDialog
        route={primaryConfirmation?.route ?? null}
        pending={primaryMutation.isPending}
        onOpenChange={(open) => {
          if (!open && !primaryMutation.isPending) {
            setPrimaryConfirmation(null)
          }
        }}
        onConfirm={() => {
          if (!primaryConfirmation) return
          primaryMutation.mutate({
            route: primaryConfirmation.route,
            request: {
              ...primaryConfirmation.request,
              confirmStabilityOverride: true,
            },
          })
        }}
      />
    </div>
  )
}
