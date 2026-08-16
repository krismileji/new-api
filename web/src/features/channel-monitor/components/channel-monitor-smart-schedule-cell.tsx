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
import { InformationCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'

import {
  channelMonitorSmartScheduleRouteIsBreakEvenFallback,
  channelMonitorSmartScheduleRouteIsAvailable,
  channelMonitorSmartScheduleRouteIsRateLimitCoolingDown,
  channelMonitorSmartScheduleRouteIsTrafficPaused,
  channelMonitorSmartScheduleRouteParticipates,
} from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'
import { ChannelMonitorSmartScheduleClearDialog } from './channel-monitor-smart-schedule-clear-dialog'

type ChannelMonitorSmartScheduleCellProps = {
  channelName: string
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  selectedGroupModel: Pick<
    ChannelMonitorSmartScheduleRoute,
    'group' | 'model'
  > | null
  pending: boolean
  onUpdate: (excluded: boolean) => void
}

type SmartScheduleStatusBadge = {
  key: string
  label: string
  variant: 'default' | 'secondary' | 'destructive' | 'warning' | 'outline'
  clearProtectionLabel?: string
}

function formatRemainingTime(until: number, now: number) {
  const minutes = Math.ceil((until - now) / 60)
  if (minutes <= 0) return ''
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.ceil(minutes / 60)
  if (hours < 24) return `${hours} 小时`
  return `${Math.ceil(hours / 24)} 天`
}

function formatTrafficPercent(value: number) {
  return Number.isInteger(value) ? value.toFixed(0) : value.toFixed(1)
}

function ChannelMonitorSmartScheduleCellStatus(props: {
  route: ChannelMonitorSmartScheduleRoute | undefined
  hasSelection: boolean
  onClearProtection: (route: ChannelMonitorSmartScheduleRoute) => void
}) {
  const route = props.route
  if (!route) {
    return (
      <Badge variant='outline'>
        {props.hasSelection ? '无对应路由' : '未选择分组模型'}
      </Badge>
    )
  }

  const now = Math.floor(Date.now() / 1000)
  const statuses: SmartScheduleStatusBadge[] = []
  const details: Array<{ label: string; value: string }> = []
  const participates = channelMonitorSmartScheduleRouteParticipates(route)
  const trafficPaused =
    participates &&
    route.channel_status === CHANNEL_STATUS.ENABLED &&
    route.enabled &&
    channelMonitorSmartScheduleRouteIsTrafficPaused(route, now)
  const rateLimitCoolingDown =
    participates &&
    route.channel_status === CHANNEL_STATUS.ENABLED &&
    route.enabled &&
    !trafficPaused &&
    channelMonitorSmartScheduleRouteIsRateLimitCoolingDown(route, now)
  const available =
    participates &&
    channelMonitorSmartScheduleRouteIsAvailable(route) &&
    !rateLimitCoolingDown
  let unavailableClearProtectionLabel: string | undefined
  if (participates && route.state.stability_state === 'degraded') {
    unavailableClearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性降级保护`
  } else if (participates && route.state.stability_state === 'probing') {
    unavailableClearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性释放`
  } else if (
    participates &&
    route.state.temporary_traffic_kind === 'insufficient_samples'
  ) {
    unavailableClearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的统一探索采样`
  } else if (
    participates &&
    route.state.temporary_traffic_kind === 'adaptive_sampling'
  ) {
    unavailableClearProtectionLabel = `解除 ${route.channel_name} ${route.group} ${route.model} 的自适应备援采样`
  }
  const stabilityRemaining = formatRemainingTime(
    Math.max(route.state.stability_until, route.state.runtime_protection_until),
    now
  )

  if (available && route.state.stability_state === 'degraded') {
    statuses.push({
      key: 'degraded',
      label: `稳定性降级${stabilityRemaining ? ` · ${stabilityRemaining}` : ''}`,
      variant: 'destructive',
      clearProtectionLabel: `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性降级保护`,
    })
  } else if (available && route.state.stability_state === 'probing') {
    statuses.push({
      key: 'probing',
      label: '稳定性释放',
      variant: 'warning',
      clearProtectionLabel: `解除 ${route.channel_name} ${route.group} ${route.model} 的稳定性释放`,
    })
  }

  const fixedRemaining = participates
    ? formatRemainingTime(route.state.manual_primary_until, now)
    : ''
  const breakEvenFallback =
    channelMonitorSmartScheduleRouteIsBreakEvenFallback(route)
  const decision = route.state.last_schedule_score_details?.decision
  const breakEvenTakingOver =
    breakEvenFallback &&
    ((decision?.actual_highest_priority === route.priority &&
      (decision.actual_top_layer_channel_ids ?? []).includes(
        route.channel_id
      )) ||
      decision?.selection_reason.includes('保本兜底层接管') === true)
  if (available && breakEvenFallback) {
    let label = '保本兜底'
    if (fixedRemaining) label = '保本兜底 · 已手动固定'
    else if (breakEvenTakingOver) label = '保本兜底 · 接管中'
    statuses.push({
      key: 'break-even-fallback',
      label,
      variant: fixedRemaining || breakEvenTakingOver ? 'warning' : 'outline',
    })
  } else if (available && fixedRemaining) {
    statuses.push({
      key: 'fixed',
      label: `固定主渠道 · ${fixedRemaining}`,
      variant: 'default',
    })
  }

  if (
    available &&
    route.state.temporary_traffic_kind === 'insufficient_samples'
  ) {
    statuses.push({
      key: 'exploration',
      label: `统一探索采样 ${formatTrafficPercent(route.state.temporary_traffic_target_percent)}%`,
      variant: 'warning',
      clearProtectionLabel: `解除 ${route.channel_name} ${route.group} ${route.model} 的统一探索采样`,
    })
  } else if (
    available &&
    route.state.temporary_traffic_kind === 'adaptive_sampling'
  ) {
    statuses.push({
      key: 'adaptive-sampling',
      label: `自适应备援采样 ${formatTrafficPercent(route.state.temporary_traffic_target_percent)}%`,
      variant: 'warning',
      clearProtectionLabel: `解除 ${route.channel_name} ${route.group} ${route.model} 的自适应备援采样`,
    })
  }

  if (!participates) {
    statuses.push({ key: 'excluded', label: '未参与调度', variant: 'outline' })
  } else if (route.channel_status !== CHANNEL_STATUS.ENABLED) {
    statuses.push({
      key: 'channel-disabled',
      label: '渠道禁用',
      variant: 'destructive',
      clearProtectionLabel: unavailableClearProtectionLabel,
    })
  } else if (!route.enabled) {
    statuses.push({
      key: 'route-disabled',
      label: '路由禁用',
      variant: 'destructive',
      clearProtectionLabel: unavailableClearProtectionLabel,
    })
  } else if (trafficPaused) {
    statuses.push({
      key: 'traffic-paused',
      label: '流量已暂停',
      variant: 'warning',
    })
  } else if (rateLimitCoolingDown) {
    statuses.push({
      key: 'rate-limit-cooldown',
      label: '429 冷却',
      variant: 'warning',
    })
  } else if (route.state.last_schedule_status === 'failed') {
    statuses.push({ key: 'failed', label: '调度失败', variant: 'destructive' })
  }

  if (statuses.length === 0) {
    statuses.push({ key: 'normal', label: '常规调度', variant: 'secondary' })
  }

  details.push({
    label: '当前路由',
    value: `P${route.priority} · W${route.weight}`,
  })
  if (trafficPaused) {
    details.push({
      label: '暂停至',
      value: formatTimestampToDate(route.traffic_paused_until ?? 0),
    })
  }
  if (rateLimitCoolingDown) {
    details.push({
      label: '429 冷却至',
      value: formatTimestampToDate(route.rate_limit_cooldown_until ?? 0),
    })
  }
  if (breakEvenFallback) {
    details.push(
      {
        label: '成本倍率',
        value: route.cost_ratio == null ? '-' : route.cost_ratio.toFixed(6),
      },
      {
        label: '分组倍率',
        value: route.group_ratio == null ? '-' : route.group_ratio.toFixed(6),
      },
      {
        label: '倍率差',
        value: route.gross_margin == null ? '-' : route.gross_margin.toFixed(6),
      }
    )
  }
  if (participates && route.state.stability_since > 0) {
    details.push({
      label:
        route.state.stability_state === 'probing' ? '释放开始' : '降级开始',
      value: formatTimestampToDate(route.state.stability_since),
    })
  }
  if (participates && route.state.stability_until > 0) {
    details.push({
      label: '预计试放',
      value: formatTimestampToDate(route.state.stability_until),
    })
  }
  if (participates && route.state.runtime_protection_until > now) {
    details.push({
      label: '即时保护至',
      value: formatTimestampToDate(route.state.runtime_protection_until),
    })
  }
  if (participates && route.state.temporary_traffic_since > 0) {
    details.push({
      label: '采样开始',
      value: formatTimestampToDate(route.state.temporary_traffic_since),
    })
  }
  if (participates && route.state.temporary_traffic_kind !== '') {
    details.push({
      label: '目标流量',
      value: `${formatTrafficPercent(route.state.temporary_traffic_target_percent)}%`,
    })
  }
  if (fixedRemaining) {
    details.push(
      {
        label: '固定到期',
        value: formatTimestampToDate(route.state.manual_primary_until),
      },
      {
        label: '稳定性策略',
        value: route.state.manual_primary_allow_stability_degrade
          ? '允许降级'
          : '固定期间不降级',
      }
    )
  }
  if (route.state.last_schedule_time > 0) {
    details.push({
      label: '最近调度',
      value: formatTimestampToDate(route.state.last_schedule_time),
    })
  }

  const visibleStatuses = statuses.slice(0, 2)
  const hiddenStatusCount = statuses.length - visibleStatuses.length
  const statusSummary = statuses.map((status) => status.label).join('、')

  return (
    <Popover>
      <div className='flex h-5 min-w-0 items-center gap-1'>
        {visibleStatuses.map((status) => {
          if (status.clearProtectionLabel) {
            return (
              <Badge
                key={status.key}
                render={<button type='button' />}
                variant={status.variant}
                className='cursor-pointer'
                title={status.clearProtectionLabel}
                aria-label={status.clearProtectionLabel}
                onClick={(event) => {
                  event.stopPropagation()
                  props.onClearProtection(route)
                }}
              >
                {status.label}
              </Badge>
            )
          }
          return (
            <Badge key={status.key} variant={status.variant}>
              {status.label}
            </Badge>
          )
        })}
        {hiddenStatusCount > 0 ? (
          <Badge variant='outline'>+{hiddenStatusCount}</Badge>
        ) : null}
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-xs'
              className='text-muted-foreground size-5'
              title={`查看当前调度状态详情：${statusSummary}`}
              aria-label={`查看当前调度状态详情：${statusSummary}`}
              onClick={(event) => event.stopPropagation()}
            />
          }
        >
          <HugeiconsIcon icon={InformationCircleIcon} aria-hidden='true' />
        </PopoverTrigger>
      </div>
      <PopoverContent side='right' align='start' className='w-80'>
        <PopoverHeader>
          <PopoverTitle>当前调度状态</PopoverTitle>
          <PopoverDescription className='break-all'>
            {route.group} / {route.model}
          </PopoverDescription>
        </PopoverHeader>
        <dl className='grid grid-cols-[72px_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs'>
          <dt className='text-muted-foreground'>状态</dt>
          <dd className='min-w-0 text-right font-medium break-words'>
            {statusSummary}
          </dd>
          {details.map((detail) => (
            <div key={detail.label} className='contents'>
              <dt className='text-muted-foreground'>{detail.label}</dt>
              <dd className='min-w-0 text-right font-medium break-words tabular-nums'>
                {detail.value}
              </dd>
            </div>
          ))}
        </dl>
        {route.state.last_schedule_error ? (
          <div className='text-xs'>
            <div className='text-muted-foreground'>最近说明</div>
            <p className='mt-1 max-h-24 overflow-y-auto leading-5 break-words'>
              {route.state.last_schedule_error}
            </p>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}

export function ChannelMonitorSmartScheduleCell(
  props: ChannelMonitorSmartScheduleCellProps
) {
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const selectedGroupModel = props.selectedGroupModel
  let selectedRoute: ChannelMonitorSmartScheduleRoute | undefined
  let participatingCount = 0
  for (const route of props.routes) {
    if (channelMonitorSmartScheduleRouteParticipates(route)) {
      participatingCount += 1
    }
    if (
      selectedGroupModel &&
      route.group === selectedGroupModel.group &&
      route.model === selectedGroupModel.model
    ) {
      selectedRoute = route
    }
  }
  const participating = participatingCount > 0
  const allParticipating =
    props.routes.length > 0 && participatingCount === props.routes.length
  const nonParticipatingRoutes = props.routes.filter(
    (route) => !channelMonitorSmartScheduleRouteParticipates(route)
  )
  const nonParticipatingModelLabel = nonParticipatingRoutes
    .map((route) => `${route.group} / ${route.model}`)
    .join('、')
  let participationVariant: 'outline' | 'secondary' | 'warning' = 'outline'
  let participationLabel = `未参与 0/${props.routes.length}`
  if (allParticipating) {
    participationVariant = 'secondary'
    participationLabel = `全部 ${participatingCount}/${props.routes.length}`
  } else if (participating) {
    participationVariant = 'warning'
    participationLabel = `部分 ${participatingCount}/${props.routes.length}`
  }

  return (
    <div className='flex min-w-[240px] flex-col items-start gap-1'>
      <div
        className='flex h-5 shrink-0 items-center gap-1.5'
        data-smart-schedule-line='participation'
        onClick={(event) => event.stopPropagation()}
      >
        {props.pending ? <Spinner /> : null}
        <Switch
          checked={participating}
          disabled={props.routes.length === 0 || props.pending}
          onCheckedChange={(checked) => props.onUpdate(!checked)}
          aria-label={`${participating ? '取消' : '开启'} ${props.channelName} 全部路由的智能调度参与`}
        />
        {nonParticipatingRoutes.length > 0 && participating ? (
          <TooltipProvider delay={150}>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span
                    className='focus-visible:ring-ring/50 inline-flex min-w-0 cursor-help rounded-4xl focus-visible:ring-2 focus-visible:outline-none'
                    tabIndex={0}
                    aria-label={`查看未参与智能调度的模型：${nonParticipatingModelLabel}`}
                  />
                }
              >
                <Badge
                  variant={participationVariant}
                  className='whitespace-nowrap'
                >
                  {participationLabel}
                </Badge>
              </TooltipTrigger>
              <TooltipContent
                side='top'
                className='max-w-80 text-left leading-5 whitespace-normal'
              >
                <div className='space-y-1'>
                  <div className='font-medium'>未参与智能调度的模型</div>
                  <div className='max-h-48 overflow-y-auto break-all'>
                    {nonParticipatingRoutes.map((route) => (
                      <div key={`${route.group}\u0000${route.model}`}>
                        {route.group} / {route.model}
                      </div>
                    ))}
                  </div>
                </div>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : (
          <Badge variant={participationVariant} className='whitespace-nowrap'>
            {participationLabel}
          </Badge>
        )}
      </div>
      <div
        className='flex h-5 min-w-0 items-center'
        data-smart-schedule-line='routing'
      >
        <span className='flex min-w-0 flex-1 items-baseline gap-2 text-xs whitespace-nowrap'>
          <span className='text-muted-foreground'>优先级</span>
          <span className='font-mono text-sm font-medium tabular-nums'>
            {selectedRoute?.priority ?? '—'}
          </span>
          <span className='bg-border h-3.5 w-px shrink-0' aria-hidden='true' />
          <span className='text-muted-foreground'>权重</span>
          <span className='font-mono text-sm font-medium tabular-nums'>
            {selectedRoute?.weight ?? '—'}
          </span>
        </span>
      </div>
      <div
        className='flex h-5 min-w-0 items-center'
        data-smart-schedule-line='scores'
      >
        <span className='flex min-w-0 flex-1 items-baseline gap-2 text-xs whitespace-nowrap'>
          <span className='text-muted-foreground'>当前得分</span>
          <span className='font-mono text-sm font-medium tabular-nums'>
            {selectedRoute?.current_window_score == null
              ? '—'
              : (selectedRoute.current_window_score * 100).toFixed(1)}
          </span>
          <span className='bg-border h-3.5 w-px shrink-0' aria-hidden='true' />
          <span className='text-muted-foreground'>最近得分</span>
          <span className='font-mono text-sm font-medium tabular-nums'>
            {selectedRoute?.state.last_schedule_score == null
              ? '—'
              : (selectedRoute.state.last_schedule_score * 100).toFixed(1)}
          </span>
        </span>
      </div>
      <div
        className='flex h-5 min-w-0 items-center'
        data-smart-schedule-line='status'
      >
        <ChannelMonitorSmartScheduleCellStatus
          route={selectedRoute}
          hasSelection={selectedGroupModel !== null}
          onClearProtection={setClearTarget}
        />
      </div>
      {clearTarget ? (
        <ChannelMonitorSmartScheduleClearDialog
          route={clearTarget}
          onOpenChange={(open) => {
            if (!open) setClearTarget(null)
          }}
        />
      ) : null}
    </div>
  )
}
