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
  CancelCircleIcon,
  Layers01Icon,
  Route01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { IconBadge } from '@/components/ui/icon-badge'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'

import { updateChannelMonitorSmartScheduleRouteConfig } from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { formatMonitorRatio } from '../lib/format'
import { CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY } from '../lib/query-options'
import {
  channelMonitorSmartScheduleRouteKey,
  channelMonitorSmartScheduleRouteParticipates,
  compareChannelMonitorSmartScheduleGroupsByRatio,
  compareChannelMonitorSmartScheduleRoutesByPool,
  formatChannelMonitorSmartSchedulePriorityWeightRange,
  summarizeChannelMonitorSmartScheduleChannel,
  type ChannelMonitorSmartScheduleRoutePlacement,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
} from '../types'
import { ChannelMonitorSmartScheduleClearDialog } from './channel-monitor-smart-schedule-clear-dialog'
import { ChannelMonitorSmartScheduleRouteState } from './channel-monitor-smart-schedule-route-state'

type ChannelMonitorSmartScheduleChannelSheetProps = {
  channel: ChannelMonitorItem | null
  channelId: number
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  groupRatios: Readonly<Record<string, number>>
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>
  performanceItems: readonly ChannelMonitorSmartScheduleRoutePerformance[]
  stabilityItems: readonly ChannelMonitorSmartScheduleRouteStability[]
  stabilityMetricsAvailable: boolean
  rangeMinutes: number
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenChangeComplete?: (open: boolean) => void
}

const ROUTE_ROLE_LABEL: Record<
  ChannelMonitorSmartScheduleRoutePlacement['role'],
  string
> = {
  primary: '主候选',
  candidate: '候选',
  standby: '备用',
  excluded: '未参与',
  unavailable: '不可调度',
}

function getRoutePlacementDescription(
  route: ChannelMonitorSmartScheduleRoute,
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
) {
  if (!placement) return '暂无调度位置数据'
  if (placement.role === 'excluded') return '该路由已取消参与智能调度'
  if (placement.role === 'unavailable') return '渠道或分组模型路由当前不可用'
  if (placement.role === 'standby') {
    return `实际优先级 P${route.priority} 低于当前候选层 P${placement.topPriority ?? '-'}，请求重试时作为备用层`
  }
  const share = ((placement.estimatedShare ?? 0) * 100).toFixed(1)
  return `位于最高优先级 P${placement.topPriority ?? route.priority}，同层 ${placement.candidateCount} 条路由按权重分配，预计 ${share}%`
}

function formatPerformance(
  metric: ChannelMonitorSmartScheduleRoutePerformance | undefined
) {
  if (!metric) return '暂无样本'
  const values: string[] = []
  if (metric.average_first_token_ms != null) {
    values.push(`首字 ${Math.round(metric.average_first_token_ms)} ms`)
  }
  if (metric.average_tps != null) {
    values.push(`TPS ${metric.average_tps.toFixed(1)}`)
  }
  return values.length > 0 ? values.join(' · ') : '暂无样本'
}

function formatStability(
  metric: ChannelMonitorSmartScheduleRouteStability | undefined,
  available: boolean
) {
  if (!available) return '未启用统计'
  if (!metric || metric.sample_count === 0) return '暂无样本'
  const values = [
    `稳定性 ${metric.stability_score == null ? '-' : `${(metric.stability_score * 100).toFixed(1)}%`}`,
    `成功率 ${(metric.success_rate * 100).toFixed(1)}%`,
    `${metric.sample_count} 次`,
  ]
  if (metric.retry_failure_count > 0) {
    values.push(
      `重试失败 ${metric.retry_failure_count} 次 / 平均 ${Math.round(metric.average_retry_failure_duration_ms)} ms`
    )
  }
  if (metric.final_failure_count > 0) {
    values.push(`最终失败 ${metric.final_failure_count} 次`)
  }
  return values.join(' · ')
}

function formatProbeSummary(
  state: ChannelMonitorSmartScheduleRoute['state']
): string {
  const values = [
    `成功 ${state.probe_success_count}/${state.probe_sample_count} 次`,
  ]
  if (state.probe_average_first_token_ms != null) {
    values.push(
      `首字 ${Math.round(state.probe_average_first_token_ms)} ms（${state.probe_first_token_sample_count} 次）`
    )
  }
  if (state.probe_average_tps != null) {
    values.push(
      `TPS ${state.probe_average_tps.toFixed(1)}（${state.probe_tps_sample_count} 次）`
    )
  }
  if (state.probe_average_failure_duration_ms != null) {
    values.push(
      `失败耗时 ${Math.round(state.probe_average_failure_duration_ms)} ms（${state.probe_failure_duration_sample_count} 次）`
    )
  }
  return values.join(' · ')
}

export function ChannelMonitorSmartScheduleChannelSheet(
  props: ChannelMonitorSmartScheduleChannelSheetProps
) {
  const queryClient = useQueryClient()
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const channelName = props.channel?.name ?? `渠道 #${props.channelId}`
  const summary = useMemo(
    () =>
      summarizeChannelMonitorSmartScheduleChannel(
        props.routes,
        props.groupRatios
      ),
    [props.groupRatios, props.routes]
  )
  const performanceByRoute = useMemo(
    () =>
      new Map(
        props.performanceItems.map((metric) => [
          channelMonitorSmartScheduleRouteKey(metric),
          metric,
        ])
      ),
    [props.performanceItems]
  )
  const stabilityByRoute = useMemo(
    () =>
      new Map(
        props.stabilityItems.map((metric) => [
          channelMonitorSmartScheduleRouteKey(metric),
          metric,
        ])
      ),
    [props.stabilityItems]
  )
  const updateMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleRouteConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => toast.success('路由调度参与设置已保存'),
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
      })
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    },
  })
  const routesByGroup = useMemo(() => {
    const groups = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
    const sortedRoutes = [...props.routes].sort((first, second) =>
      compareChannelMonitorSmartScheduleRoutesByPool(
        first,
        second,
        props.placements,
        props.groupRatios
      )
    )
    for (const route of sortedRoutes) {
      const routes = groups.get(route.group)
      if (routes) routes.push(route)
      else groups.set(route.group, [route])
    }
    return [...groups.entries()]
      .sort((first, second) =>
        compareChannelMonitorSmartScheduleGroupsByRatio(
          first[0],
          second[0],
          props.groupRatios
        )
      )
      .map(([group, routes]) => ({
        group,
        routes,
        summary: summarizeChannelMonitorSmartScheduleChannel(
          routes,
          props.groupRatios
        )?.groups.at(0),
      }))
  }, [props.groupRatios, props.placements, props.routes])

  return (
    <>
      <Sheet
        open={props.open}
        onOpenChange={props.onOpenChange}
        onOpenChangeComplete={props.onOpenChangeComplete}
      >
        <SheetContent
          side='right'
          className={sideDrawerContentClassName('sm:max-w-5xl')}
        >
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle className='flex items-center gap-3'>
              <IconBadge tone='info' size='title'>
                <HugeiconsIcon icon={Route01Icon} aria-hidden='true' />
              </IconBadge>
              <span className='min-w-0'>
                智能调度详情
                <span className='text-muted-foreground ml-2 text-sm font-normal break-words'>
                  {channelName}
                </span>
              </span>
            </SheetTitle>
            <SheetDescription className='mt-1'>
              {summary
                ? `${summary.routeCount} 条路由 · ${summary.participatingCount} 条参与 · 统计近 ${props.rangeMinutes} 分钟`
                : '当前渠道暂无可调度路由'}
            </SheetDescription>
          </SheetHeader>
          {!summary ? (
            <div className={sideDrawerFormClassName()}>
              <Empty className='min-h-64 border-0'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <HugeiconsIcon icon={Alert02Icon} />
                  </EmptyMedia>
                  <EmptyTitle>暂无智能调度路由</EmptyTitle>
                  <EmptyDescription>请先为渠道关联分组和模型</EmptyDescription>
                </EmptyHeader>
              </Empty>
            </div>
          ) : (
            <div className={sideDrawerFormClassName('gap-5')}>
              <div className='grid gap-5 lg:grid-cols-[13rem_minmax(0,1fr)] lg:items-start'>
                <aside
                  className='flex flex-col gap-3 lg:sticky lg:top-0'
                  aria-label='渠道调度摘要'
                >
                  <div className='border-border/60 bg-muted/20 rounded-lg border p-3'>
                    <p className='font-medium break-words'>{channelName}</p>
                    <p className='text-muted-foreground mt-0.5 text-xs'>
                      ID {props.channelId}
                    </p>
                    <dl className='mt-3 flex flex-col gap-2 text-xs'>
                      <div className='flex items-center justify-between gap-3'>
                        <dt className='text-muted-foreground'>状态</dt>
                        <dd className='font-medium'>
                          {props.channel?.status === CHANNEL_STATUS.ENABLED
                            ? '已启用'
                            : '已禁用'}
                        </dd>
                      </div>
                      <div className='flex items-center justify-between gap-3'>
                        <dt className='text-muted-foreground'>渠道默认</dt>
                        <dd className='font-mono font-medium tabular-nums'>
                          P{props.channel?.priority ?? '-'} / W
                          {props.channel?.weight ?? '-'}
                        </dd>
                      </div>
                    </dl>
                  </div>
                  <dl className='border-border/60 grid grid-cols-2 overflow-hidden rounded-lg border text-xs'>
                    <div className='border-border/60 flex flex-col gap-1 border-r border-b p-3'>
                      <dt className='text-muted-foreground'>参与路由</dt>
                      <dd className='font-mono font-semibold tabular-nums'>
                        {summary.participatingCount}/{summary.routeCount}
                      </dd>
                    </div>
                    <div className='border-border/60 flex flex-col gap-1 border-b p-3'>
                      <dt className='text-muted-foreground'>稳定性降级</dt>
                      <dd className='font-mono font-semibold tabular-nums'>
                        {summary.degradedCount}
                      </dd>
                    </div>
                    <div className='border-border/60 flex flex-col gap-1 border-r border-b p-3'>
                      <dt className='text-muted-foreground'>稳定性试放</dt>
                      <dd className='font-mono font-semibold tabular-nums'>
                        {summary.probingCount}
                      </dd>
                    </div>
                    <div className='border-border/60 flex flex-col gap-1 border-b p-3'>
                      <dt className='text-muted-foreground'>探索采样</dt>
                      <dd className='font-mono font-semibold tabular-nums'>
                        {summary.explorationCount}
                      </dd>
                    </div>
                    <div className='col-span-2 flex flex-col gap-1 p-3'>
                      <dt className='text-muted-foreground'>失败记录</dt>
                      <dd className='font-mono font-semibold tabular-nums'>
                        {summary.failedCount}
                      </dd>
                    </div>
                  </dl>
                </aside>

                <div className='flex min-w-0 flex-col gap-6'>
                  <SideDrawerSection>
                    <SideDrawerSectionHeader
                      title='渠道信息'
                      description='渠道倍率、备注和关联分组'
                      icon={
                        <HugeiconsIcon icon={Route01Icon} aria-hidden='true' />
                      }
                      iconTone='info'
                    />
                    <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
                      <div className='min-w-0'>
                        <span className='text-muted-foreground text-xs'>
                          成本倍率
                        </span>
                        <p className='mt-1 font-mono font-medium tabular-nums'>
                          {formatMonitorRatio(
                            props.channel?.cost_ratio ?? null
                          )}
                        </p>
                        {props.channel?.previous_cost_ratio != null &&
                        props.channel.previous_cost_ratio !==
                          props.channel.cost_ratio ? (
                          <p className='text-muted-foreground mt-0.5 text-xs tabular-nums'>
                            上次{' '}
                            {formatMonitorRatio(
                              props.channel.previous_cost_ratio
                            )}
                          </p>
                        ) : null}
                      </div>
                      <div className='min-w-0'>
                        <span className='text-muted-foreground text-xs'>
                          上游倍率
                        </span>
                        <p className='mt-1 font-mono font-medium tabular-nums'>
                          {formatMonitorRatio(props.channel?.ratio ?? null)}
                        </p>
                      </div>
                      <div className='min-w-0'>
                        <span className='text-muted-foreground text-xs'>
                          关联分组
                        </span>
                        <div className='mt-1 flex flex-wrap gap-1.5'>
                          {(props.channel?.groups ?? []).length > 0 ? (
                            props.channel?.groups.map((group) => (
                              <Badge key={group} variant='outline'>
                                {group}
                              </Badge>
                            ))
                          ) : (
                            <span className='text-muted-foreground text-sm'>
                              暂无
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className='min-w-0'>
                      <span className='text-muted-foreground text-xs'>
                        渠道备注
                      </span>
                      <p className='mt-1 break-words'>
                        {props.channel?.channel_remark || '暂无备注'}
                      </p>
                      {props.channel?.remark &&
                      props.channel.remark !== props.channel.channel_remark ? (
                        <p className='text-muted-foreground mt-1 text-xs break-words'>
                          监控备注：{props.channel.remark}
                        </p>
                      ) : null}
                    </div>
                  </SideDrawerSection>

                  <SideDrawerSection>
                    <SideDrawerSectionHeader
                      title='分组与模型路由'
                      description='按分组、模型和实际优先级检查当前调度位置'
                      icon={
                        <HugeiconsIcon icon={Layers01Icon} aria-hidden='true' />
                      }
                      iconTone='primary'
                    />
                    <div className='flex flex-col gap-5'>
                      {routesByGroup.map(
                        (
                          { group, routes: groupRoutes, summary: groupSummary },
                          groupIndex
                        ) => (
                          <section key={group} className='flex flex-col gap-3'>
                            {groupIndex > 0 ? <Separator /> : null}
                            <div className='flex items-center justify-between gap-3'>
                              <div className='min-w-0'>
                                <h3 className='font-medium break-words'>
                                  {group}
                                </h3>
                                <p className='text-muted-foreground text-xs'>
                                  {groupRoutes.length} 个模型路由
                                </p>
                              </div>
                              <Badge variant='outline'>
                                {groupSummary
                                  ? formatChannelMonitorSmartSchedulePriorityWeightRange(
                                      groupSummary
                                    )
                                  : '暂无'}
                              </Badge>
                            </div>
                            <div className='flex flex-col gap-2'>
                              {groupRoutes.map((route) => {
                                const key =
                                  channelMonitorSmartScheduleRouteKey(route)
                                const performance = performanceByRoute.get(key)
                                const stability = stabilityByRoute.get(key)
                                const placement = props.placements.get(key)
                                const updatePending =
                                  updateMutation.isPending &&
                                  updateMutation.variables != null &&
                                  channelMonitorSmartScheduleRouteKey({
                                    channel_id:
                                      updateMutation.variables.channelId,
                                    group: updateMutation.variables.group,
                                    model: updateMutation.variables.model,
                                  }) === key
                                return (
                                  <div
                                    key={key}
                                    className='bg-muted/30 flex flex-col gap-3 rounded-lg border px-3 py-3'
                                  >
                                    <div className='flex items-start justify-between gap-3'>
                                      <div className='min-w-0'>
                                        <p className='font-medium break-words'>
                                          {route.model}
                                        </p>
                                        <p className='text-muted-foreground mt-1 text-xs'>
                                          {formatPerformance(performance)} ·{' '}
                                          {formatStability(
                                            stability,
                                            props.stabilityMetricsAvailable
                                          )}
                                        </p>
                                      </div>
                                      <div className='flex shrink-0 items-center gap-2'>
                                        {updatePending ? <Spinner /> : null}
                                        <Switch
                                          checked={channelMonitorSmartScheduleRouteParticipates(
                                            route
                                          )}
                                          disabled={updateMutation.isPending}
                                          onCheckedChange={(checked) =>
                                            updateMutation.mutate({
                                              channelId: route.channel_id,
                                              group: route.group,
                                              model: route.model,
                                              excluded: !checked,
                                            })
                                          }
                                          aria-label={`${route.group} ${route.model} 参与智能调度`}
                                        />
                                      </div>
                                    </div>
                                    <div className='flex flex-wrap items-center gap-x-4 gap-y-2 text-xs'>
                                      <span className='font-mono font-medium tabular-nums'>
                                        P{route.priority} / W{route.weight}
                                      </span>
                                      <span className='text-muted-foreground font-mono tabular-nums'>
                                        得分{' '}
                                        {route.state.last_schedule_score == null
                                          ? '-'
                                          : (
                                              route.state.last_schedule_score *
                                              100
                                            ).toFixed(1)}
                                      </span>
                                      <Badge variant='outline'>
                                        {
                                          ROUTE_ROLE_LABEL[
                                            placement?.role ?? 'unavailable'
                                          ]
                                        }
                                      </Badge>
                                      {placement?.estimatedShare != null ? (
                                        <span className='font-mono font-medium tabular-nums'>
                                          预计流量{' '}
                                          {(
                                            placement.estimatedShare * 100
                                          ).toFixed(1)}
                                          %
                                        </span>
                                      ) : null}
                                      <ChannelMonitorSmartScheduleRouteState
                                        route={route}
                                        onProtectedStatusClick={() =>
                                          setClearTarget(route)
                                        }
                                      />
                                      {route.state.stability_until > 0 &&
                                      route.state.stability_state ===
                                        'degraded' ? (
                                        <span className='text-muted-foreground'>
                                          至{' '}
                                          {formatTimestampToDate(
                                            route.state.stability_until
                                          )}
                                        </span>
                                      ) : null}
                                    </div>
                                    {stability?.jitter_available ? (
                                      <div
                                        data-slot='smart-schedule-jitter-detail'
                                        className='border-border/60 flex flex-wrap items-center gap-x-3 gap-y-1 border-t pt-2 text-xs'
                                        aria-label={`${route.model} 成功延迟抖动详情`}
                                      >
                                        <Badge
                                          variant={
                                            stability.jitter_penalty > 0
                                              ? 'warning'
                                              : 'secondary'
                                          }
                                        >
                                          延迟抖动
                                        </Badge>
                                        <span className='text-muted-foreground tabular-nums'>
                                          基线{' '}
                                          {stability.first_token_baseline_ms ==
                                          null
                                            ? '-'
                                            : `${Math.round(stability.first_token_baseline_ms)} ms`}
                                        </span>
                                        <span className='text-muted-foreground tabular-nums'>
                                          P50{' '}
                                          {stability.first_token_p50_ms == null
                                            ? '-'
                                            : `${Math.round(stability.first_token_p50_ms)} ms`}
                                        </span>
                                        <span className='text-muted-foreground tabular-nums'>
                                          P95{' '}
                                          {stability.first_token_p95_ms == null
                                            ? '-'
                                            : `${Math.round(stability.first_token_p95_ms)} ms`}
                                        </span>
                                        <span className='text-muted-foreground tabular-nums'>
                                          阈值{' '}
                                          {stability.jitter_threshold_ms == null
                                            ? '-'
                                            : `${Math.round(stability.jitter_threshold_ms)} ms`}
                                        </span>
                                        <span className='text-muted-foreground tabular-nums'>
                                          慢成功 {stability.jitter_slow_count}/
                                          {stability.jitter_sample_count} · 容忍{' '}
                                          {stability.jitter_allowed_count} ·
                                          超额惩罚{' '}
                                          {Math.round(stability.jitter_penalty)}
                                        </span>
                                      </div>
                                    ) : null}
                                    <p className='text-muted-foreground text-xs break-words'>
                                      {getRoutePlacementDescription(
                                        route,
                                        placement
                                      )}
                                    </p>
                                    {route.state.last_schedule_time > 0 ? (
                                      <p className='text-muted-foreground text-xs'>
                                        最近调度{' '}
                                        {formatTimestampToDate(
                                          route.state.last_schedule_time
                                        )}
                                      </p>
                                    ) : null}
                                    {route.state.last_schedule_error ? (
                                      <p className='text-destructive flex items-start gap-1 text-xs break-words'>
                                        <HugeiconsIcon
                                          icon={CancelCircleIcon}
                                        />
                                        {route.state.last_schedule_error}
                                      </p>
                                    ) : null}
                                    {route.state.probe_last_time > 0 ? (
                                      <div className='border-border/60 flex flex-col gap-1.5 border-t pt-2 text-xs'>
                                        <div className='flex flex-wrap items-center gap-2'>
                                          <Badge
                                            variant={
                                              route.state.probe_last_success
                                                ? 'secondary'
                                                : 'destructive'
                                            }
                                          >
                                            {route.state.probe_last_success
                                              ? '探测成功'
                                              : '探测失败'}
                                          </Badge>
                                          <span className='text-muted-foreground'>
                                            最近探测{' '}
                                            {formatTimestampToDate(
                                              route.state.probe_last_time
                                            )}
                                          </span>
                                        </div>
                                        <p className='text-muted-foreground break-words'>
                                          {formatProbeSummary(route.state)}
                                        </p>
                                        {route.state.probe_last_error ? (
                                          <p className='text-destructive flex items-start gap-1 break-words'>
                                            <HugeiconsIcon
                                              icon={CancelCircleIcon}
                                              aria-hidden='true'
                                            />
                                            {route.state.probe_last_error}
                                          </p>
                                        ) : null}
                                      </div>
                                    ) : null}
                                  </div>
                                )
                              })}
                            </div>
                          </section>
                        )
                      )}
                    </div>
                  </SideDrawerSection>
                </div>
              </div>
            </div>
          )}
          <SheetFooter className={sideDrawerFooterClassName('grid-cols-1')}>
            <SheetClose
              render={<Button variant='outline' className='w-full sm:w-auto' />}
            >
              关闭
            </SheetClose>
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ChannelMonitorSmartScheduleClearDialog
        route={clearTarget}
        onOpenChange={(open) => {
          if (!open) setClearTarget(null)
        }}
      />
    </>
  )
}
