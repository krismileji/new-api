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
  Cancel01Icon,
  InformationCircleIcon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'

import { formatMonitorRatio } from '../lib/format'
import {
  formatChannelMonitorSmartScheduleEstimatedShare,
  formatChannelMonitorSmartScheduleTemporaryTraffic,
} from '../lib/smart-schedule-display'
import {
  channelMonitorSmartScheduleRouteIsBreakEvenFallback,
  channelMonitorSmartScheduleRouteParticipates,
  getChannelMonitorSmartScheduleRouteDisplayStatus,
  type ChannelMonitorSmartScheduleRoutePlacement,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
  ChannelMonitorSmartScheduleSampleItem,
} from '../types'
import { ChannelMonitorSmartScheduleGroupPause } from './channel-monitor-smart-schedule-group-pause'
import { ChannelMonitorSmartSchedulePrimaryControls } from './channel-monitor-smart-schedule-primary-controls'
import { ChannelMonitorSmartScheduleRouteState } from './channel-monitor-smart-schedule-route-state'
import { ChannelMonitorSmartScheduleSampleDetails } from './channel-monitor-smart-schedule-sample-details'
import { ChannelMonitorSmartScheduleScoreDetails } from './channel-monitor-smart-schedule-score-details'

type ChannelMonitorSmartScheduleRouteDetailsProps = {
  open: boolean
  route: ChannelMonitorSmartScheduleRoute | null
  channel: ChannelMonitorItem | undefined
  poolRoutes: readonly ChannelMonitorSmartScheduleRoute[]
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
  performance: ChannelMonitorSmartScheduleRoutePerformance | undefined
  businessPerformance: ChannelMonitorSmartScheduleRoutePerformance | undefined
  stability: ChannelMonitorSmartScheduleRouteStability | undefined
  samples: ChannelMonitorSmartScheduleSampleItem | undefined
  updatePending: boolean
  manualRoutingPending: boolean
  groupPausePending: boolean
  updateDisabled: boolean
  onOpenChange: (open: boolean) => void
  onParticipationChange: (
    route: ChannelMonitorSmartScheduleRoute,
    checked: boolean
  ) => void
  onClearProtection: (route: ChannelMonitorSmartScheduleRoute) => void
  onSetPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onClearPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onSaveManualRouting: (
    route: ChannelMonitorSmartScheduleRoute,
    priority: number,
    weight: number
  ) => void
  onGroupPauseChange: (
    route: ChannelMonitorSmartScheduleRoute,
    durationMinutes: number
  ) => void
}

export function ChannelMonitorSmartScheduleRouteStatus(props: {
  route: ChannelMonitorSmartScheduleRoute
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
  onClearProtection: () => void
}) {
  const status = getChannelMonitorSmartScheduleRouteDisplayStatus(
    props.route,
    props.placement
  )
  if (status === 'paused') {
    return <Badge variant='warning'>流量已暂停</Badge>
  }
  if (
    props.route.state.stability_state !== '' ||
    props.route.state.temporary_traffic_kind === 'insufficient_samples'
  ) {
    return (
      <ChannelMonitorSmartScheduleRouteState
        route={props.route}
        onProtectedStatusClick={props.onClearProtection}
      />
    )
  }

  if (status === 'failed') {
    return <Badge variant='destructive'>调度失败</Badge>
  }
  if (status === 'insufficient_samples') {
    return <Badge variant='warning'>样本不足补量</Badge>
  }
  if (status === 'priority_sampling') {
    return <Badge variant='warning'>低优先级轮转</Badge>
  }
  if (
    channelMonitorSmartScheduleRouteIsBreakEvenFallback(props.route) &&
    (status === 'primary' || status === 'candidate' || status === 'backup')
  ) {
    if (props.route.state.manual_primary_until > 0) {
      return <Badge variant='warning'>保本兜底 · 已手动固定</Badge>
    }
    if (props.placement?.isActualTopLayer) {
      return <Badge variant='warning'>保本兜底 · 接管中</Badge>
    }
    return <Badge variant='outline'>保本兜底</Badge>
  }
  if (status === 'primary') return <Badge>实际主渠道</Badge>
  if (status === 'candidate') {
    return <Badge variant='secondary'>实际最高层</Badge>
  }
  if (status === 'backup') return <Badge variant='outline'>备用顺位</Badge>
  if (status === 'excluded') return <Badge variant='outline'>未参与</Badge>
  return <Badge variant='destructive'>不可调度</Badge>
}

function DetailMetric(props: { label: string; value: string }) {
  return (
    <div className='bg-background min-w-0 rounded-md border px-3 py-2'>
      <div className='text-muted-foreground text-xs leading-4'>
        {props.label}
      </div>
      <div
        className='mt-1 font-mono text-sm leading-5 font-medium break-words tabular-nums'
        title={props.value}
      >
        {props.value}
      </div>
    </div>
  )
}

function formatPoolChannelReference(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  channelId: number
) {
  if (channelId <= 0) return '-'
  const route = routes.find((item) => item.channel_id === channelId)
  return route
    ? `${route.channel_name}（ID ${channelId}）`
    : `渠道 ID ${channelId}`
}

export function ChannelMonitorSmartScheduleRouteDetails(
  props: ChannelMonitorSmartScheduleRouteDetailsProps
) {
  if (!props.route) return null

  const route = props.route
  const remark = props.channel?.channel_remark || props.channel?.remark
  const currentWindowScore =
    route.current_window_score == null
      ? '-'
      : `${(route.current_window_score * 100).toFixed(1)} 分`
  const lastScheduleScore =
    route.state.last_schedule_score == null
      ? '-'
      : `${(route.state.last_schedule_score * 100).toFixed(1)} 分`
  const participates = channelMonitorSmartScheduleRouteParticipates(route)
  const decision = route.state.last_schedule_score_details?.decision
  const baseRank = route.state.base_rank || decision?.base_rank || 0
  const basePriority = route.state.base_priority || decision?.base_priority || 0
  const baseWeight = route.state.base_weight || decision?.base_weight || 0
  const scoringWinnerChannelId =
    props.placement?.scoringWinnerChannelId ??
    decision?.raw_winner_channel_id ??
    0
  const actualPrimaryChannelId =
    props.placement?.actualPrimaryChannelId ??
    decision?.actual_primary_channel_id ??
    0
  const actualHighestPriority =
    props.placement?.actualHighestPriority ??
    decision?.actual_highest_priority ??
    null
  const actualTopLayerChannelIds =
    props.placement?.actualTopLayerChannelIds ??
    decision?.actual_top_layer_channel_ids ??
    []
  const pending =
    props.updatePending || props.manualRoutingPending || props.groupPausePending
  const channelNameById = new Map(
    props.poolRoutes.map((poolRoute) => [
      poolRoute.channel_id,
      poolRoute.channel_name,
    ])
  )

  return (
    <Sheet
      open={props.open}
      onOpenChange={(open) => {
        if (!open && pending) return
        props.onOpenChange(open)
      }}
    >
      <SheetContent
        id={`channel-monitor-route-details-${route.channel_id}`}
        className='w-full gap-0 sm:max-w-3xl'
        showCloseButton={false}
        aria-label={`${route.channel_name} 调度详情`}
      >
        <SheetHeader className='relative border-b pr-12'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <SheetTitle className='truncate' title={route.channel_name}>
              {route.channel_name}
            </SheetTitle>
            {route.state.manual_primary_until > 0 ? (
              <Badge variant='secondary'>管理员固定</Badge>
            ) : null}
            {channelMonitorSmartScheduleRouteIsBreakEvenFallback(route) ? (
              <Badge variant='warning'>保本兜底</Badge>
            ) : null}
            {props.placement?.isScoringWinner ? (
              <Badge variant='outline'>评分第一</Badge>
            ) : null}
            {props.placement?.isActualPrimary ? (
              <Badge>实际主渠道</Badge>
            ) : null}
            {props.placement?.isActualTopLayer ? (
              <Badge variant='secondary'>实际最高层</Badge>
            ) : null}
          </div>
          <SheetDescription className='truncate' title={remark || undefined}>
            ID {route.channel_id} · {route.group} / {route.model}
            {remark ? ` · ${remark}` : ''}
          </SheetDescription>
          <SheetClose
            disabled={pending}
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                className='absolute top-3 right-3'
              />
            }
          >
            <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
            <span className='sr-only'>关闭渠道调度详情</span>
          </SheetClose>
        </SheetHeader>

        <div className='min-h-0 flex-1 overflow-y-auto'>
          <section className='bg-muted/20 px-4 py-4' aria-label='渠道调度摘要'>
            <div className='flex flex-wrap items-start justify-between gap-3'>
              <div className='min-w-0'>
                <div className='text-muted-foreground text-xs'>当前状态</div>
                <div className='mt-1 flex flex-wrap items-center gap-2'>
                  <ChannelMonitorSmartScheduleRouteStatus
                    route={route}
                    placement={props.placement}
                    onClearProtection={() => props.onClearProtection(route)}
                  />
                  {props.updatePending || props.groupPausePending ? (
                    <Spinner className='size-4' />
                  ) : null}
                  {route.state.last_schedule_time > 0 ? (
                    <span className='text-muted-foreground text-xs'>
                      更新于{' '}
                      {formatTimestampToDate(route.state.last_schedule_time)}
                    </span>
                  ) : null}
                </div>
              </div>
              <div className='text-right'>
                <div className='text-muted-foreground text-xs'>预计流量</div>
                <div className='mt-1 font-mono text-lg font-semibold tabular-nums'>
                  {formatChannelMonitorSmartScheduleEstimatedShare(
                    props.placement
                  )}
                </div>
              </div>
            </div>

            <div className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4'>
              <DetailMetric
                label='成本倍率'
                value={formatMonitorRatio(
                  route.cost_ratio ?? props.channel?.cost_ratio
                )}
              />
              <DetailMetric
                label='分组倍率'
                value={formatMonitorRatio(route.group_ratio)}
              />
              <DetailMetric
                label='倍率差'
                value={formatMonitorRatio(route.gross_margin)}
              />
              <DetailMetric
                label='当前窗口预计得分'
                value={currentWindowScore}
              />
              <DetailMetric label='最近调度得分' value={lastScheduleScore} />
              <DetailMetric
                label='当前 P / W'
                value={`P${route.priority} / W${route.weight}`}
              />
            </div>
          </section>

          <section className='border-t px-4 py-4' aria-label='评分与流量依据'>
            <div className='mb-3'>
              <h3 className='text-sm font-medium'>评分与流量依据</h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                用于解释本渠道为什么处于当前顺位和流量状态
              </p>
            </div>
            <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
              <DetailMetric
                label='基础排名'
                value={baseRank > 0 ? `第 ${baseRank} 名` : '-'}
              />
              <DetailMetric
                label='基础 P / W'
                value={baseRank > 0 ? `P${basePriority} / W${baseWeight}` : '-'}
              />
              <DetailMetric
                label='临时流量类型与目标'
                value={formatChannelMonitorSmartScheduleTemporaryTraffic(
                  route.state.temporary_traffic_kind,
                  route.state.temporary_traffic_target_percent
                )}
              />
              <DetailMetric
                label='最近调度'
                value={
                  route.state.last_schedule_time > 0
                    ? formatTimestampToDate(route.state.last_schedule_time)
                    : '-'
                }
              />
            </div>
          </section>

          <section className='border-t px-4 py-4' aria-label='调度池决策结果'>
            <div className='mb-3'>
              <h3 className='text-sm font-medium'>调度池决策结果</h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                当前渠道在本分组和模型中的实际路由位置
              </p>
            </div>
            <div className='grid gap-x-5 gap-y-3 sm:grid-cols-3'>
              <DetailMetric
                label='评分第一'
                value={formatPoolChannelReference(
                  props.poolRoutes,
                  scoringWinnerChannelId
                )}
              />
              <DetailMetric
                label='实际主渠道'
                value={formatPoolChannelReference(
                  props.poolRoutes,
                  actualPrimaryChannelId
                )}
              />
              <DetailMetric
                label='实际最高层'
                value={
                  actualHighestPriority == null
                    ? '-'
                    : `P${actualHighestPriority} · ${
                        actualTopLayerChannelIds
                          .map((channelId) =>
                            formatPoolChannelReference(
                              props.poolRoutes,
                              channelId
                            )
                          )
                          .join('、') || '渠道 -'
                      }`
                }
              />
            </div>
          </section>

          <section className='border-t px-4 py-3' aria-label='渠道调度控制'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <div className='flex flex-wrap items-center gap-2'>
                <Switch
                  checked={participates}
                  disabled={props.updateDisabled}
                  onCheckedChange={(checked) =>
                    props.onParticipationChange(route, checked)
                  }
                  aria-label={`${route.channel_name} ${route.group} ${route.model} 参与智能调度`}
                />
                <span className='text-muted-foreground text-xs'>参与调度</span>
              </div>
              <ChannelMonitorSmartSchedulePrimaryControls
                route={route}
                disabled={props.updateDisabled}
                onEdit={props.onSetPrimary}
                onClear={props.onClearPrimary}
              />
            </div>
          </section>

          <ChannelMonitorSmartScheduleGroupPause
            route={route}
            pending={props.groupPausePending}
            disabled={props.updateDisabled}
            onUpdate={props.onGroupPauseChange}
          />

          {!participates ? (
            <section className='border-t px-4 py-4' aria-label='人工路由设置'>
              <div className='flex items-start gap-2'>
                <HugeiconsIcon
                  icon={InformationCircleIcon}
                  className='text-muted-foreground mt-0.5 size-4 shrink-0'
                  aria-hidden='true'
                />
                <div>
                  <h3 className='text-sm font-medium'>人工路由设置</h3>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    该路由未参与智能调度，调度任务不会修改这里保存的优先级和权重。绝对优先级高于智能主渠道时，真实请求会优先进入该路由。
                  </p>
                </div>
              </div>
              <form
                key={`${route.channel_id}\u0000${route.group}\u0000${route.model}\u0000${route.priority}\u0000${route.weight}`}
                className='mt-4 flex flex-col gap-3 sm:flex-row sm:items-end'
                aria-label={`${route.channel_name} ${route.group} ${route.model} 人工路由设置`}
                onSubmit={(event) => {
                  event.preventDefault()
                  const formData = new FormData(event.currentTarget)
                  const priority = Number(formData.get('manual_priority'))
                  const weight = Number(formData.get('manual_weight'))
                  if (
                    !Number.isInteger(priority) ||
                    !Number.isInteger(weight)
                  ) {
                    return
                  }
                  props.onSaveManualRouting(route, priority, weight)
                }}
              >
                <FieldGroup className='grid flex-1 grid-cols-2 gap-3'>
                  <Field>
                    <FieldLabel htmlFor={`manual-priority-${route.channel_id}`}>
                      人工优先级
                    </FieldLabel>
                    <Input
                      id={`manual-priority-${route.channel_id}`}
                      name='manual_priority'
                      type='number'
                      min={0}
                      max={2_147_483_647}
                      step={1}
                      defaultValue={route.priority}
                      disabled={props.updateDisabled}
                      required
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`manual-weight-${route.channel_id}`}>
                      人工权重
                    </FieldLabel>
                    <Input
                      id={`manual-weight-${route.channel_id}`}
                      name='manual_weight'
                      type='number'
                      min={0}
                      max={2_147_483_647}
                      step={1}
                      defaultValue={route.weight}
                      disabled={props.updateDisabled}
                      required
                    />
                  </Field>
                </FieldGroup>
                <Button type='submit' disabled={props.updateDisabled}>
                  {props.manualRoutingPending ? (
                    <Spinner data-icon='inline-start' />
                  ) : (
                    <HugeiconsIcon icon={Tick02Icon} data-icon='inline-start' />
                  )}
                  保存人工路由
                </Button>
              </form>
            </section>
          ) : null}

          <ChannelMonitorSmartScheduleScoreDetails
            details={route.current_window_score_details}
            snapshotLabel='当前窗口只读预估'
            inputLabel='当前窗口输入'
            showDecision={false}
            defaultOpen={false}
            channelNameById={channelNameById}
          />
          <ChannelMonitorSmartScheduleScoreDetails
            details={route.state.last_schedule_score_details}
            snapshotLabel='最近一次实际调度快照'
            defaultOpen={false}
            channelNameById={channelNameById}
          />
          {!route.current_window_score_details &&
          !route.state.last_schedule_score_details ? (
            <section className='border-t px-4 py-4' aria-label='评分计算'>
              <div className='flex items-start gap-2'>
                <HugeiconsIcon
                  icon={InformationCircleIcon}
                  className='text-muted-foreground mt-0.5 size-4 shrink-0'
                  aria-hidden='true'
                />
                <div>
                  <h3 className='text-sm font-medium'>评分计算</h3>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    当前窗口和最近一次调度都没有可展示的评分快照。
                  </p>
                </div>
              </div>
            </section>
          ) : null}
          <ChannelMonitorSmartScheduleSampleDetails
            route={route}
            performance={props.performance}
            businessPerformance={props.businessPerformance}
            stability={props.stability}
            samples={props.samples}
          />
        </div>
      </SheetContent>
    </Sheet>
  )
}
