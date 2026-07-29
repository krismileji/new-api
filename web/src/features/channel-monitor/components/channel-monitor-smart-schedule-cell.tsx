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
import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import {
  channelMonitorSmartScheduleRouteKey,
  compareChannelMonitorSmartScheduleRoutesByPool,
  getChannelMonitorSmartScheduleRouteDisplayStatus,
  summarizeChannelMonitorSmartScheduleChannel,
  type ChannelMonitorSmartScheduleRouteDisplayStatus,
  type ChannelMonitorSmartScheduleRoutePlacement,
} from '../lib/smart-schedule-summary'
import type { ChannelMonitorSmartScheduleRoute } from '../types'

type ChannelMonitorSmartScheduleCellProps = {
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  effectiveRoutes?: readonly ChannelMonitorSmartScheduleRoute[]
  groupRatios: Readonly<Record<string, number>>
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>
  pending: boolean
  onUpdate: (excluded: boolean) => void
  onOpen: () => void
  onClearStability: (route: ChannelMonitorSmartScheduleRoute) => void
}

const ROUTE_STATUS_LABEL: Record<
  ChannelMonitorSmartScheduleRouteDisplayStatus,
  string
> = {
  degraded: '稳定性降级',
  probing: '稳定性试放',
  exploring: '探索采样',
  failed: '最近失败',
  primary: '主候选',
  candidate: '候选',
  standby: '备用',
  excluded: '未参与',
  unavailable: '不可调度',
}

export function ChannelMonitorSmartScheduleCell(
  props: ChannelMonitorSmartScheduleCellProps
) {
  const summary = summarizeChannelMonitorSmartScheduleChannel(
    props.routes,
    props.groupRatios
  )
  if (!summary) {
    return <span className='text-muted-foreground text-sm'>暂无路由</span>
  }

  const participating = summary.participatingCount > 0
  const effectiveRoutes = props.effectiveRoutes ?? props.routes
  const effectiveSummary = summarizeChannelMonitorSmartScheduleChannel(
    effectiveRoutes,
    props.groupRatios
  )
  const highlightedRoutes = [...effectiveRoutes]
    .sort((first, second) =>
      compareChannelMonitorSmartScheduleRoutesByPool(
        first,
        second,
        props.placements,
        props.groupRatios
      )
    )
    .slice(0, 2)
  const hiddenRouteCount = effectiveRoutes.length - highlightedRoutes.length

  return (
    <div className='flex min-w-[280px] flex-col gap-1'>
      <div className='flex items-center gap-2'>
        <div
          className='flex shrink-0 items-center gap-1.5'
          onClick={(event) => event.stopPropagation()}
        >
          {props.pending ? <Spinner /> : null}
          <Switch
            checked={participating}
            disabled={props.pending}
            onCheckedChange={(checked) => props.onUpdate(!checked)}
            aria-label={`${participating ? '取消' : '开启'} ${summary.channelName} 的智能调度参与`}
          />
        </div>
        <button
          type='button'
          className='group focus-visible:ring-ring flex min-w-0 flex-1 items-center gap-1.5 rounded-md text-left outline-none focus-visible:ring-2'
          onClick={props.onOpen}
          aria-label={`查看 ${summary.channelName} 的智能调度详情`}
        >
          <span className='font-medium tabular-nums'>
            {summary.participatingCount}/{summary.routeCount} 路由参与
          </span>
          <span className='text-muted-foreground text-xs tabular-nums'>
            · {effectiveSummary?.activeCount ?? 0} 可调度
          </span>
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            className='text-muted-foreground transition-transform group-hover:translate-x-0.5'
          />
        </button>
      </div>
      <div className='mt-1 flex flex-col gap-1'>
        {effectiveRoutes.length === 0 ? (
          <span className='text-muted-foreground text-xs'>
            当前无已配置策略路由
          </span>
        ) : null}
        {highlightedRoutes.map((route) => {
          const placement = props.placements.get(
            channelMonitorSmartScheduleRouteKey(route)
          )
          const status = getChannelMonitorSmartScheduleRouteDisplayStatus(
            route,
            placement
          )
          const protectedRoute = status === 'degraded' || status === 'probing'
          return (
            <div
              key={channelMonitorSmartScheduleRouteKey(route)}
              className='bg-muted/40 flex min-w-0 flex-col gap-1 rounded-md px-2 py-1.5 text-xs'
            >
              <button
                type='button'
                className='hover:text-foreground focus-visible:ring-ring flex min-w-0 items-center justify-between gap-2 rounded-sm text-left transition-colors outline-none focus-visible:ring-2'
                onClick={props.onOpen}
                aria-label={`查看 ${route.group} ${route.model} 的智能调度详情`}
              >
                <span
                  className='min-w-0 truncate font-medium'
                  title={`${route.group} / ${route.model}`}
                >
                  {route.group} / {route.model}
                </span>
                <span className='shrink-0 font-mono tabular-nums'>
                  P{route.priority} / W{route.weight}
                </span>
              </button>
              <div className='text-muted-foreground flex items-center justify-between gap-2'>
                {protectedRoute ? (
                  <Badge
                    render={<button type='button' />}
                    variant={status === 'degraded' ? 'destructive' : 'warning'}
                    className='cursor-pointer'
                    onClick={() => props.onClearStability(route)}
                    aria-label={`解除 ${route.channel_name} ${route.group} ${route.model} 的${ROUTE_STATUS_LABEL[status]}保护`}
                  >
                    {ROUTE_STATUS_LABEL[status]}
                  </Badge>
                ) : (
                  <span>{ROUTE_STATUS_LABEL[status]}</span>
                )}
                {placement?.estimatedShare != null ? (
                  <span className='font-mono tabular-nums'>
                    预计 {(placement.estimatedShare * 100).toFixed(1)}%
                  </span>
                ) : null}
              </div>
            </div>
          )
        })}
        {hiddenRouteCount > 0 ? (
          <span className='text-muted-foreground text-xs'>
            还有 {hiddenRouteCount} 条分组模型路由
          </span>
        ) : null}
      </div>
    </div>
  )
}
