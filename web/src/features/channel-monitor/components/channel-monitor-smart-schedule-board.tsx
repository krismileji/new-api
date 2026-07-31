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
  ArrowDown01Icon,
  Cancel01Icon,
  HistoryIcon,
  PinIcon,
  Refresh01Icon,
  Route01Icon,
  Settings02Icon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import { Switch } from '@/components/ui/switch'
import { compareChannelStatusesEnabledFirst } from '@/features/channels/lib/channel-status-order'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  runChannelMonitorSmartSchedule,
  updateChannelMonitorSmartScheduleRoutePrimary,
  updateChannelMonitorSmartScheduleRouteConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { formatMonitorRatio } from '../lib/format'
import { CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY } from '../lib/query-options'
import { compareChannelMonitorSmartScheduleModels } from '../lib/smart-schedule-model-order'
import { createChannelMonitorSmartSchedulePrimaryFormState } from '../lib/smart-schedule-primary'
import {
  channelMonitorSmartScheduleRouteKey,
  channelMonitorSmartScheduleRouteParticipates,
  compareChannelMonitorSmartScheduleGroupsByRatio,
  filterChannelMonitorSmartScheduleRoutes,
  getChannelMonitorSmartSchedulePoolStatus,
  getChannelMonitorSmartScheduleRouteDisplayStatus,
  isChannelMonitorSmartScheduleResultStale,
  placeChannelMonitorSmartScheduleRoutes,
  summarizeChannelMonitorSmartScheduleOverview,
  summarizeChannelMonitorSmartSchedulePools,
  type ChannelMonitorSmartSchedulePoolStatus,
  type ChannelMonitorSmartSchedulePoolSummary,
  type ChannelMonitorSmartScheduleRoutePlacement,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRouteResult,
} from '../types'
import { ChannelMonitorSmartScheduleClearDialog } from './channel-monitor-smart-schedule-clear-dialog'
import { ChannelMonitorSmartScheduleModelTestDialog } from './channel-monitor-smart-schedule-model-test-dialog'
import { ChannelMonitorSmartSchedulePoolLayout } from './channel-monitor-smart-schedule-pool-layout'
import {
  ChannelMonitorSmartSchedulePrimaryControls,
  ChannelMonitorSmartSchedulePrimaryStabilityField,
} from './channel-monitor-smart-schedule-primary-controls'
import { ChannelMonitorSmartScheduleRouteState } from './channel-monitor-smart-schedule-route-state'
import { ChannelMonitorSmartScheduleScoreDetails } from './channel-monitor-smart-schedule-score-details'

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
}

type SchedulePoolView = {
  summary: ChannelMonitorSmartSchedulePoolSummary
  routes: ChannelMonitorSmartScheduleRoute[]
}

type RouteRowProps = {
  className?: string
  route: ChannelMonitorSmartScheduleRoute
  channel: ChannelMonitorItem | undefined
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
  updatePending: boolean
  updateDisabled: boolean
  onParticipationChange: (
    route: ChannelMonitorSmartScheduleRoute,
    checked: boolean
  ) => void
  onClearProtection: (route: ChannelMonitorSmartScheduleRoute) => void
  onSetPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onClearPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
}

const EMPTY_ROUTES: readonly ChannelMonitorSmartScheduleRoute[] = []

function getPoolStatusVariant(status: ChannelMonitorSmartSchedulePoolStatus) {
  if (status === '稳定性降级' || status === '最近失败') return 'destructive'
  if (
    status === '稳定性试放' ||
    status === '探索采样' ||
    status === '部分可调度'
  ) {
    return 'warning'
  }
  if (status === '正常') return 'secondary'
  return 'outline'
}

function formatEstimatedShare(
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
) {
  if (placement?.estimatedShare != null) {
    return `${(placement.estimatedShare * 100).toFixed(1)}%`
  }
  if (placement?.role === 'first_backup' || placement?.role === 'standby') {
    return '0%'
  }
  return '-'
}

function formatSampleMode(
  policy: ChannelMonitorSmartScheduleGroupPolicy | undefined
) {
  if (!policy || policy.sample_mode === 'off') return '样本补充关闭'
  if (policy.sample_mode === 'traffic') {
    return `探索流量 ${policy.exploration_traffic_percent}%`
  }
  return `每 ${policy.probe_interval_minutes} 分钟文本探测`
}

function RouteMetric(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-[11px] leading-4'>
        {props.label}
      </div>
      <div className='truncate font-mono text-xs font-medium tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function ScheduleRouteStatus(props: {
  route: ChannelMonitorSmartScheduleRoute
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
  onClearProtection: () => void
}) {
  if (props.route.state.stability_state !== '') {
    return (
      <ChannelMonitorSmartScheduleRouteState
        route={props.route}
        onProtectedStatusClick={props.onClearProtection}
      />
    )
  }

  const status = getChannelMonitorSmartScheduleRouteDisplayStatus(
    props.route,
    props.placement
  )
  if (status === 'failed') {
    return <Badge variant='destructive'>调度失败</Badge>
  }
  if (status === 'exploring') return <Badge variant='warning'>探索采样</Badge>
  if (status === 'primary') return <Badge>主渠道</Badge>
  if (status === 'candidate') return <Badge variant='secondary'>同层候选</Badge>
  if (status === 'first_backup') {
    return <Badge variant='outline'>第一备用</Badge>
  }
  if (status === 'standby') return <Badge variant='outline'>后续备用</Badge>
  if (status === 'excluded') return <Badge variant='outline'>未参与</Badge>
  return <Badge variant='destructive'>不可调度</Badge>
}

function ScheduleRouteRow(props: RouteRowProps) {
  const remark = props.channel?.channel_remark || props.channel?.remark

  return (
    <div
      className={cn(
        'bg-card grid gap-3 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto]',
        props.className
      )}
    >
      <div className='min-w-0'>
        <div className='truncate font-medium'>{props.route.channel_name}</div>
        <div className='text-muted-foreground mt-0.5 flex min-w-0 items-center gap-2 text-xs'>
          <span className='shrink-0'>ID {props.route.channel_id}</span>
          <span aria-hidden='true'>·</span>
          <span className='truncate' title={remark || '暂无备注'}>
            {remark || '暂无备注'}
          </span>
        </div>
      </div>

      <div className='flex flex-wrap items-center gap-3 sm:justify-end'>
        <ScheduleRouteStatus
          route={props.route}
          placement={props.placement}
          onClearProtection={() => props.onClearProtection(props.route)}
        />
        <div className='flex items-center gap-2'>
          {props.updatePending ? <Spinner className='size-4' /> : null}
          <Switch
            checked={channelMonitorSmartScheduleRouteParticipates(props.route)}
            disabled={props.updateDisabled}
            onCheckedChange={(checked) =>
              props.onParticipationChange(props.route, checked)
            }
            aria-label={`${props.route.channel_name} ${props.route.group} ${props.route.model} 参与智能调度`}
          />
          <span className='text-muted-foreground text-xs'>参与</span>
        </div>
        <ChannelMonitorSmartSchedulePrimaryControls
          route={props.route}
          disabled={props.updateDisabled}
          onEdit={props.onSetPrimary}
          onClear={props.onClearPrimary}
        />
      </div>

      <div className='border-border/70 grid grid-cols-2 gap-x-4 gap-y-2 border-t pt-2 sm:col-span-2 sm:grid-cols-5'>
        <RouteMetric
          label='成本倍率'
          value={formatMonitorRatio(props.channel?.cost_ratio)}
        />
        <RouteMetric
          label='最终得分'
          value={
            props.route.state.last_schedule_score == null
              ? '-'
              : `${(props.route.state.last_schedule_score * 100).toFixed(1)}%`
          }
        />
        <RouteMetric label='优先级' value={`P${props.route.priority}`} />
        <RouteMetric label='权重' value={`W${props.route.weight}`} />
        <RouteMetric
          label='预计流量'
          value={formatEstimatedShare(props.placement)}
        />
      </div>
      <ChannelMonitorSmartScheduleScoreDetails
        details={props.route.state.last_schedule_score_details}
        className='sm:col-span-2'
        snapshotLabel='最近一次调度快照'
      />
      {props.route.state.probe_last_time > 0 ? (
        <div className='border-border/70 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 border-t pt-2 text-xs sm:col-span-2'>
          <Badge
            variant={
              props.route.state.probe_last_success ? 'secondary' : 'destructive'
            }
          >
            {props.route.state.probe_last_success ? '探测成功' : '探测失败'}
          </Badge>
          <span className='text-muted-foreground'>
            最近探测 {formatTimestampToDate(props.route.state.probe_last_time)}
          </span>
          {props.route.state.probe_last_error ? (
            <span
              className='text-destructive min-w-0 truncate'
              title={props.route.state.probe_last_error}
            >
              {props.route.state.probe_last_error}
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function SchedulePoolCard(props: {
  pool: SchedulePoolView
  policy: ChannelMonitorSmartScheduleGroupPolicy | undefined
  wide: boolean
  channelsById: ReadonlyMap<number, ChannelMonitorItem>
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>
  updateRouteKey: string | null
  updateDisabled: boolean
  onParticipationChange: (
    route: ChannelMonitorSmartScheduleRoute,
    checked: boolean
  ) => void
  onClearProtection: (route: ChannelMonitorSmartScheduleRoute) => void
  onSetPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onClearPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onModelTest: () => void
}) {
  const primaryRoutes: ChannelMonitorSmartScheduleRoute[] = []
  const foldedRoutes: ChannelMonitorSmartScheduleRoute[] = []

  for (const route of props.pool.routes) {
    const key = channelMonitorSmartScheduleRouteKey(route)
    const displayStatus = getChannelMonitorSmartScheduleRouteDisplayStatus(
      route,
      props.placements.get(key)
    )
    if (
      displayStatus === 'first_backup' ||
      displayStatus === 'standby' ||
      displayStatus === 'unavailable' ||
      displayStatus === 'excluded'
    ) {
      foldedRoutes.push(route)
    } else {
      primaryRoutes.push(route)
    }
  }

  const status = getChannelMonitorSmartSchedulePoolStatus(props.pool.summary)
  const trafficRoutes = props.pool.routes
    .map((route) => ({
      route,
      share:
        props.placements.get(channelMonitorSmartScheduleRouteKey(route))
          ?.estimatedShare ?? null,
    }))
    .filter(
      (
        item
      ): item is { route: ChannelMonitorSmartScheduleRoute; share: number } =>
        item.share != null
    )
    .sort((first, second) => second.share - first.share)
  const visibleTrafficRoutes = trafficRoutes.slice(0, 3)
  const otherTrafficShare = trafficRoutes
    .slice(3)
    .reduce((total, item) => total + item.share, 0)
  const standbyCount = foldedRoutes.filter(
    (route) =>
      props.placements.get(channelMonitorSmartScheduleRouteKey(route))?.role ===
      'standby'
  ).length
  const firstBackupCount = foldedRoutes.filter(
    (route) =>
      props.placements.get(channelMonitorSmartScheduleRouteKey(route))?.role ===
      'first_backup'
  ).length
  const unavailableCount = foldedRoutes.filter(
    (route) =>
      props.placements.get(channelMonitorSmartScheduleRouteKey(route))?.role ===
      'unavailable'
  ).length
  const excludedCount =
    foldedRoutes.length - firstBackupCount - standbyCount - unavailableCount
  const finalPrimaryGridRowStartsAt =
    primaryRoutes.length - (primaryRoutes.length % 2 === 0 ? 2 : 1)

  const renderRoute = (
    route: ChannelMonitorSmartScheduleRoute,
    className?: string
  ) => {
    const key = channelMonitorSmartScheduleRouteKey(route)
    return (
      <ScheduleRouteRow
        key={key}
        className={className}
        route={route}
        channel={props.channelsById.get(route.channel_id)}
        placement={props.placements.get(key)}
        updatePending={props.updateRouteKey === key}
        updateDisabled={props.updateDisabled}
        onParticipationChange={props.onParticipationChange}
        onClearProtection={props.onClearProtection}
        onSetPrimary={props.onSetPrimary}
        onClearPrimary={props.onClearPrimary}
      />
    )
  }

  return (
    <Card size='sm' className='gap-0 self-start py-0'>
      <CardHeader className='border-b py-3'>
        <div className='min-w-0'>
          <CardTitle className='flex min-w-0 flex-wrap items-center gap-2'>
            <span className='truncate' title={props.pool.summary.model}>
              {props.pool.summary.model}
            </span>
          </CardTitle>
          <CardDescription className='mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs'>
            <span>
              可调度 {props.pool.summary.activeCount}/
              {props.pool.summary.participatingCount}
            </span>
            <span>
              {props.pool.summary.topPriority == null
                ? '无候选层'
                : `流入层 P${props.pool.summary.topPriority} · ${props.pool.summary.candidateCount} 条`}
            </span>
            <span>{formatSampleMode(props.policy)}</span>
          </CardDescription>
        </div>
        <CardAction className='flex items-center gap-2'>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            onClick={props.onModelTest}
            aria-label={`测试 ${props.pool.summary.group} ${props.pool.summary.model} 调度池模型`}
            title='模型测试'
          >
            <HugeiconsIcon icon={TestTubeIcon} />
          </Button>
          <Badge variant={getPoolStatusVariant(status)}>{status}</Badge>
        </CardAction>
      </CardHeader>

      <CardContent className='p-0'>
        {visibleTrafficRoutes.length > 0 ? (
          <div className='border-b px-4 py-3'>
            <div className='mb-2 flex items-center justify-between gap-3 text-xs'>
              <span className='font-medium'>预计流量分布</span>
              <span className='text-muted-foreground'>当前流入层</span>
            </div>
            <div className='flex flex-col gap-2'>
              {visibleTrafficRoutes.map((item) => {
                const percentage = item.share * 100
                return (
                  <div
                    key={channelMonitorSmartScheduleRouteKey(item.route)}
                    className='grid grid-cols-[minmax(0,8rem)_minmax(5rem,1fr)_3.5rem] items-center gap-2 text-xs'
                  >
                    <span className='truncate' title={item.route.channel_name}>
                      {item.route.channel_name}
                    </span>
                    <span className='bg-muted h-1.5 overflow-hidden rounded-full'>
                      <span
                        className='bg-primary block h-full rounded-full transition-[width] duration-200'
                        style={{ width: `${Math.max(2, percentage)}%` }}
                      />
                    </span>
                    <span className='text-right font-mono tabular-nums'>
                      {percentage.toFixed(1)}%
                    </span>
                  </div>
                )
              })}
              {otherTrafficShare > 0 ? (
                <div className='grid grid-cols-[minmax(0,8rem)_minmax(5rem,1fr)_3.5rem] items-center gap-2 text-xs'>
                  <span className='text-muted-foreground'>其他渠道</span>
                  <span className='bg-muted h-1.5 overflow-hidden rounded-full'>
                    <span
                      className='bg-muted-foreground/50 block h-full rounded-full'
                      style={{
                        width: `${Math.max(2, otherTrafficShare * 100)}%`,
                      }}
                    />
                  </span>
                  <span className='text-right font-mono tabular-nums'>
                    {(otherTrafficShare * 100).toFixed(1)}%
                  </span>
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {primaryRoutes.length > 0 ? (
          <div
            className={cn(
              'divide-border divide-y',
              props.wide && '2xl:grid 2xl:grid-cols-2 2xl:divide-y-0'
            )}
          >
            {primaryRoutes.map((route, index) => {
              return renderRoute(
                route,
                cn(
                  props.wide &&
                    index < finalPrimaryGridRowStartsAt &&
                    '2xl:border-b',
                  props.wide &&
                    index % 2 === 0 &&
                    index + 1 < primaryRoutes.length &&
                    '2xl:border-r'
                )
              )
            })}
          </div>
        ) : (
          <div className='text-muted-foreground px-4 py-4 text-sm'>
            当前没有主路由或候选路由
          </div>
        )}

        {foldedRoutes.length > 0 ? (
          <Collapsible className='group/collapsible border-t'>
            <CollapsibleTrigger className='hover:bg-muted/40 focus-visible:ring-ring/50 flex w-full items-center justify-between gap-3 px-4 py-2.5 text-left text-sm transition-colors outline-none focus-visible:ring-3'>
              <span>
                备用与未参与路由
                <span className='text-muted-foreground ml-2 text-xs'>
                  第一备用 {firstBackupCount} · 后续备用 {standbyCount} ·
                  不可调度 {unavailableCount} · 未参与 {excludedCount}
                </span>
              </span>
              <HugeiconsIcon
                icon={ArrowDown01Icon}
                className='text-muted-foreground size-4 shrink-0 transition-transform group-data-[panel-open]/collapsible:rotate-180'
                aria-hidden='true'
              />
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className='divide-border divide-y border-t'>
                {foldedRoutes.map((route) => renderRoute(route))}
              </div>
            </CollapsibleContent>
          </Collapsible>
        ) : null}
      </CardContent>
    </Card>
  )
}

export function ChannelMonitorSmartScheduleBoard(
  props: ChannelMonitorSmartScheduleBoardProps
) {
  const queryClient = useQueryClient()
  const [selectedGroup, setSelectedGroup] = useState('')
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const [primaryTarget, setPrimaryTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const [modelTestTarget, setModelTestTarget] = useState<{
    group: string
    model: string
  } | null>(null)
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
  const pools = useMemo(() => {
    const routesByPool = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
    for (const route of routes) {
      const key = `${route.group}\u0000${route.model}`
      const poolRoutes = routesByPool.get(key)
      if (poolRoutes) poolRoutes.push(route)
      else routesByPool.set(key, [route])
    }
    return poolSummaries
      .map<SchedulePoolView>((poolSummary) => ({
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
        pool.summary.explorationCount === 0 &&
        pool.summary.failedCount === 0
      ) {
        continue
      }
      counts.set(pool.summary.group, (counts.get(pool.summary.group) ?? 0) + 1)
    }
    return counts
  }, [pools])
  const firstDegradedGroup = pools.find(
    (pool) => pool.summary.degradedCount > 0
  )?.summary.group
  const firstFailedGroup = pools.find((pool) => pool.summary.failedCount > 0)
    ?.summary.group
  const firstProbingGroup = pools.find((pool) => pool.summary.probingCount > 0)
    ?.summary.group
  const firstExplorationGroup = pools.find(
    (pool) => pool.summary.explorationCount > 0
  )?.summary.group
  const visibleGroup = groups.includes(selectedGroup)
    ? selectedGroup
    : (groups[0] ?? '')
  const visiblePools = pools.filter(
    (pool) => pool.summary.group === visibleGroup
  )

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
    mutationFn: updateChannelMonitorSmartScheduleRoutePrimary,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.duration_minutes > 0
          ? `主渠道已固定 ${response.data.duration_minutes} 分钟`
          : '已解除主渠道固定'
      )
      setPrimaryTarget(null)
    },
    onSettled: () => {
      invalidateSchedule()
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor-smart-schedule-executions'],
      })
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor-task-history'],
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
        queryKey: ['channel-monitor-task-history'],
      })
    },
  })
  const updateRouteKey =
    updateMutation.isPending && updateMutation.variables
      ? channelMonitorSmartScheduleRouteKey({
          channel_id: updateMutation.variables.channelId,
          group: updateMutation.variables.group,
          model: updateMutation.variables.model,
        })
      : null
  const stale = isChannelMonitorSmartScheduleResultStale(
    props.result?.generated_at ?? 0,
    props.intervalMinutes
  )

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

  if (props.isError) {
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
            执行记录
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

      {props.result?.enabled &&
      (summary.degradedCount > 0 ||
        summary.failedCount > 0 ||
        summary.probingCount > 0 ||
        summary.explorationCount > 0) ? (
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
                if (firstDegradedGroup) setSelectedGroup(firstDegradedGroup)
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
                if (firstFailedGroup) setSelectedGroup(firstFailedGroup)
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
                if (firstProbingGroup) setSelectedGroup(firstProbingGroup)
              }}
            >
              稳定性试放 {summary.probingCount}
            </Badge>
          ) : null}
          {summary.explorationCount > 0 ? (
            <Badge
              render={<button type='button' />}
              variant='warning'
              className='cursor-pointer'
              onClick={() => {
                if (firstExplorationGroup) {
                  setSelectedGroup(firstExplorationGroup)
                }
              }}
            >
              探索采样 {summary.explorationCount}
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
        <div className='grid items-start gap-4 xl:grid-cols-[14rem_minmax(0,1fr)]'>
          <nav
            className='flex gap-2 overflow-x-auto pb-1 xl:sticky xl:top-4 xl:flex-col xl:overflow-visible xl:pb-0'
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
                  onClick={() => setSelectedGroup(group)}
                >
                  <span className='min-w-0 flex-1 text-left'>
                    <span className='block truncate font-medium' title={group}>
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

          <ChannelMonitorSmartSchedulePoolLayout>
            {visiblePools.map((pool) => (
              <SchedulePoolCard
                key={`${pool.summary.group}\u0000${pool.summary.model}`}
                pool={pool}
                policy={policyByGroup.get(pool.summary.group)}
                wide={visiblePools.length === 1}
                channelsById={channelsById}
                placements={placements}
                updateRouteKey={updateRouteKey}
                updateDisabled={
                  updateMutation.isPending || primaryMutation.isPending
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
                    channelId: route.channel_id,
                    group: route.group,
                    model: route.model,
                    durationMinutes: 0,
                    allowStabilityDegrade: false,
                  })
                }
                onModelTest={() =>
                  setModelTestTarget({
                    group: pool.summary.group,
                    model: pool.summary.model,
                  })
                }
              />
            ))}
          </ChannelMonitorSmartSchedulePoolLayout>
        </div>
      ) : null}

      <ChannelMonitorSmartScheduleClearDialog
        route={clearTarget}
        onOpenChange={(open) => {
          if (!open) setClearTarget(null)
        }}
      />
      {modelTestTarget ? (
        <ChannelMonitorSmartScheduleModelTestDialog
          open
          group={modelTestTarget.group}
          model={modelTestTarget.model}
          onOpenChange={(open) => {
            if (!open) setModelTestTarget(null)
          }}
        />
      ) : null}
      {primaryTarget ? (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setPrimaryTarget(null)
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
              <Button variant='outline' onClick={() => setPrimaryTarget(null)}>
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
                onClick={() =>
                  primaryMutation.mutate({
                    channelId: primaryTarget.channel_id,
                    group: primaryTarget.group,
                    model: primaryTarget.model,
                    durationMinutes: Number(primaryDuration),
                    allowStabilityDegrade: allowPrimaryStabilityDegrade,
                  })
                }
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
    </div>
  )
}
