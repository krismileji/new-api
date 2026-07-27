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
import { Alert02Icon, CancelCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
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
import { Badge } from '@/components/ui/badge'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import {
  clearChannelMonitorSmartScheduleRouteStability,
  updateChannelMonitorSmartScheduleRouteConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import { CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY } from '../lib/query-options'
import {
  channelMonitorSmartScheduleRouteKey,
  formatChannelMonitorSmartSchedulePriorityWeightRange,
  summarizeChannelMonitorSmartScheduleChannel,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
} from '../types'
import { ChannelMonitorSmartScheduleRouteState } from './channel-monitor-smart-schedule-route-state'

type ChannelMonitorSmartScheduleChannelSheetProps = {
  channelName: string
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  performanceItems: readonly ChannelMonitorSmartScheduleRoutePerformance[]
  stabilityItems: readonly ChannelMonitorSmartScheduleRouteStability[]
  stabilityMetricsAvailable: boolean
  rangeMinutes: number
  open: boolean
  onOpenChange: (open: boolean) => void
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

function formatSuccess(
  metric: ChannelMonitorSmartScheduleRouteStability | undefined,
  available: boolean
) {
  if (!available) return '未启用统计'
  if (!metric || metric.sample_count === 0) return '暂无样本'
  return `${(metric.success_rate * 100).toFixed(1)}% · ${metric.sample_count} 次`
}

export function ChannelMonitorSmartScheduleChannelSheet(
  props: ChannelMonitorSmartScheduleChannelSheetProps
) {
  const queryClient = useQueryClient()
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const summary = useMemo(
    () => summarizeChannelMonitorSmartScheduleChannel(props.routes),
    [props.routes]
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
  const clearMutation = useMutation({
    mutationFn: clearChannelMonitorSmartScheduleRouteStability,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      setClearTarget(null)
      toast.success(
        response.data.cleared
          ? `已解除保护，恢复 P${response.data.priority} / W${response.data.weight}`
          : '当前路由没有需要解除的保护'
      )
    },
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
      })
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    },
  })
  const routesByGroup = useMemo(() => {
    const groups = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
    for (const route of props.routes) {
      const routes = groups.get(route.group)
      if (routes) routes.push(route)
      else groups.set(route.group, [route])
    }
    return [...groups.entries()]
      .sort((first, second) => first[0].localeCompare(second[0]))
      .map(([group, routes]) => ({
        group,
        routes,
        summary:
          summarizeChannelMonitorSmartScheduleChannel(routes)?.groups.at(0),
      }))
  }, [props.routes])

  return (
    <>
      <Sheet open={props.open} onOpenChange={props.onOpenChange}>
        <SheetContent
          side='right'
          className='flex max-h-screen w-full flex-col gap-0 overflow-hidden sm:max-w-2xl'
        >
          <SheetHeader className='shrink-0 border-b'>
            <SheetTitle>智能调度详情 · {props.channelName}</SheetTitle>
            <SheetDescription>
              {summary
                ? `${summary.routeCount} 条路由 · ${summary.participatingCount} 条参与 · 统计近 ${props.rangeMinutes} 分钟`
                : '当前渠道暂无可调度路由'}
            </SheetDescription>
          </SheetHeader>
          {!summary ? (
            <Empty className='min-h-64 border-0'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <HugeiconsIcon icon={Alert02Icon} />
                </EmptyMedia>
                <EmptyTitle>暂无智能调度路由</EmptyTitle>
                <EmptyDescription>请先为渠道关联分组和模型</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className='min-h-0 flex-1 overflow-y-auto px-4 py-4'>
              <div className='bg-border grid grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-4'>
                <div className='bg-background flex flex-col gap-1 px-3 py-3'>
                  <span className='text-muted-foreground text-xs'>
                    参与路由
                  </span>
                  <span className='font-mono font-semibold tabular-nums'>
                    {summary.participatingCount}/{summary.routeCount}
                  </span>
                </div>
                <div className='bg-background flex flex-col gap-1 px-3 py-3'>
                  <span className='text-muted-foreground text-xs'>
                    低成功率
                  </span>
                  <span className='font-mono font-semibold tabular-nums'>
                    {summary.degradedCount}
                  </span>
                </div>
                <div className='bg-background flex flex-col gap-1 px-3 py-3'>
                  <span className='text-muted-foreground text-xs'>
                    稳定性试放
                  </span>
                  <span className='font-mono font-semibold tabular-nums'>
                    {summary.probingCount}
                  </span>
                </div>
                <div className='bg-background flex flex-col gap-1 px-3 py-3'>
                  <span className='text-muted-foreground text-xs'>
                    失败记录
                  </span>
                  <span className='font-mono font-semibold tabular-nums'>
                    {summary.failedCount}
                  </span>
                </div>
              </div>

              <div className='mt-5 flex flex-col gap-5'>
                {routesByGroup.map(
                  (
                    { group, routes: groupRoutes, summary: groupSummary },
                    groupIndex
                  ) => (
                    <section key={group} className='flex flex-col gap-3'>
                      {groupIndex > 0 ? <Separator /> : null}
                      <div className='flex items-center justify-between gap-3'>
                        <div className='min-w-0'>
                          <h3 className='font-medium break-words'>{group}</h3>
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
                        {[...groupRoutes]
                          .sort((first, second) =>
                            first.model.localeCompare(second.model)
                          )
                          .map((route) => {
                            const key =
                              channelMonitorSmartScheduleRouteKey(route)
                            const performance = performanceByRoute.get(key)
                            const stability = stabilityByRoute.get(key)
                            const updatePending =
                              updateMutation.isPending &&
                              updateMutation.variables != null &&
                              channelMonitorSmartScheduleRouteKey({
                                channel_id: updateMutation.variables.channelId,
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
                                      {formatPerformance(performance)} · 成功率{' '}
                                      {formatSuccess(
                                        stability,
                                        props.stabilityMetricsAvailable
                                      )}
                                    </p>
                                  </div>
                                  <div className='flex shrink-0 items-center gap-2'>
                                    {updatePending ? <Spinner /> : null}
                                    <Switch
                                      checked={!route.state.excluded}
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
                                          route.state.last_schedule_score * 100
                                        ).toFixed(1)}
                                  </span>
                                  <ChannelMonitorSmartScheduleRouteState
                                    route={route}
                                    onProtectedStatusClick={() =>
                                      setClearTarget(route)
                                    }
                                  />
                                  {route.state.stability_until > 0 &&
                                  route.state.stability_state === 'degraded' ? (
                                    <span className='text-muted-foreground'>
                                      至{' '}
                                      {formatTimestampToDate(
                                        route.state.stability_until
                                      )}
                                    </span>
                                  ) : null}
                                </div>
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
                                    <HugeiconsIcon icon={CancelCircleIcon} />
                                    {route.state.last_schedule_error}
                                  </p>
                                ) : null}
                              </div>
                            )
                          })}
                      </div>
                    </section>
                  )
                )}
              </div>
            </div>
          )}
        </SheetContent>
      </Sheet>
      <AlertDialog
        open={clearTarget != null}
        onOpenChange={(open) => {
          if (!open) setClearTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认解除智能调度保护？</AlertDialogTitle>
            <AlertDialogDescription>
              将立即清除“{props.channelName} / {clearTarget?.group} /{' '}
              {clearTarget?.model}”的
              {clearTarget?.state.stability_state === 'degraded'
                ? '低成功率'
                : '稳定性试放'}
              状态，并恢复保护前保存的优先级和权重。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearMutation.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={clearMutation.isPending || clearTarget == null}
              onClick={() => {
                if (!clearTarget) return
                clearMutation.mutate({
                  channelId: clearTarget.channel_id,
                  group: clearTarget.group,
                  model: clearTarget.model,
                })
              }}
            >
              {clearMutation.isPending ? '解除中...' : '确认解除'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
