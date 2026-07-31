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
  PinIcon,
  Search01Icon,
  ViewIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatMonitorRatio } from '../lib/format'
import { formatChannelMonitorSmartScheduleEstimatedShare } from '../lib/smart-schedule-display'
import {
  channelMonitorSmartScheduleRouteKey,
  channelMonitorSmartScheduleRouteParticipates,
  compareChannelMonitorSmartScheduleRoutesByAttention,
  getChannelMonitorSmartSchedulePoolStatus,
  getChannelMonitorSmartScheduleRouteDisplayStatus,
  type ChannelMonitorSmartSchedulePoolStatus,
  type ChannelMonitorSmartSchedulePoolSummary,
  type ChannelMonitorSmartScheduleRouteDisplayStatus,
  type ChannelMonitorSmartScheduleRoutePlacement,
} from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorItem,
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
} from '../types'
import {
  ChannelMonitorSmartScheduleRouteDetails,
  ChannelMonitorSmartScheduleRouteStatus,
} from './channel-monitor-smart-schedule-route-details'

export type ChannelMonitorSmartSchedulePoolView = {
  summary: ChannelMonitorSmartSchedulePoolSummary
  routes: ChannelMonitorSmartScheduleRoute[]
}

type ChannelMonitorSmartSchedulePoolProps = {
  pool: ChannelMonitorSmartSchedulePoolView
  policy: ChannelMonitorSmartScheduleGroupPolicy | undefined
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
}

type RouteFilter = 'all' | 'traffic' | 'attention' | 'backup' | 'excluded'
type RouteSort = 'schedule' | 'traffic' | 'score' | 'cost' | 'name'

const ROUTE_FILTER_OPTIONS = [
  { value: 'all', label: '全部状态' },
  { value: 'traffic', label: '正在承接流量' },
  { value: 'attention', label: '需要关注' },
  { value: 'backup', label: '备用渠道' },
  { value: 'excluded', label: '未参与' },
]

const ROUTE_SORT_OPTIONS = [
  { value: 'schedule', label: '调度顺序' },
  { value: 'traffic', label: '预计流量从高到低' },
  { value: 'score', label: '最终得分从高到低' },
  { value: 'cost', label: '成本倍率从低到高' },
  { value: 'name', label: '渠道名称' },
]

const ATTENTION_STATUSES =
  new Set<ChannelMonitorSmartScheduleRouteDisplayStatus>([
    'degraded',
    'probing',
    'exploring',
    'failed',
    'unavailable',
  ])

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

function formatSampleMode(
  policy: ChannelMonitorSmartScheduleGroupPolicy | undefined
) {
  if (!policy || policy.sample_mode === 'off') return '样本补充关闭'
  if (policy.sample_mode === 'traffic') {
    return `探索流量 ${policy.exploration_traffic_percent}%`
  }
  return `每 ${policy.probe_interval_minutes} 分钟文本探测`
}

function routeMatchesFilter(
  route: ChannelMonitorSmartScheduleRoute,
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined,
  filter: RouteFilter
) {
  if (filter === 'all') return true
  const status = getChannelMonitorSmartScheduleRouteDisplayStatus(
    route,
    placement
  )
  if (filter === 'traffic') {
    return (placement?.estimatedShare ?? 0) > 0
  }
  if (filter === 'attention') return ATTENTION_STATUSES.has(status)
  if (filter === 'backup') {
    return status === 'first_backup' || status === 'standby'
  }
  return status === 'excluded'
}

function compareNullableDescending(
  first: number | null | undefined,
  second: number | null | undefined
) {
  if (first == null && second == null) return 0
  if (first == null) return 1
  if (second == null) return -1
  return second - first
}

function RouteSamples(props: { route: ChannelMonitorSmartScheduleRoute }) {
  const samples = props.route.shared_samples
  if (samples.sample_count === 0) {
    return <span className='text-muted-foreground text-xs'>暂无样本</span>
  }

  return (
    <div className='min-w-0 text-xs tabular-nums'>
      <div className='truncate'>
        <span
          className={
            samples.last_success ? 'text-foreground' : 'text-destructive'
          }
        >
          {samples.last_success ? '样本成功' : '样本失败'}
        </span>{' '}
        · {samples.sample_count} 次 · 成功 {samples.success_count} 次
      </div>
      <div className='text-muted-foreground mt-0.5 truncate font-mono'>
        {samples.average_first_token_ms == null
          ? '首字 -'
          : `首字 ${samples.average_first_token_ms.toFixed(0)} ms`}
        {' · '}
        {samples.average_tps == null
          ? 'TPS -'
          : `TPS ${samples.average_tps.toFixed(2)}`}
      </div>
    </div>
  )
}

function ManualPrimaryIndicator(props: {
  route: ChannelMonitorSmartScheduleRoute
}) {
  if (props.route.state.manual_primary_until <= 0) return null

  const degradeLabel = props.route.state.manual_primary_allow_stability_degrade
    ? '允许稳定性降级'
    : '固定期间不降级'
  const label = `管理员固定至 ${formatTimestampToDate(props.route.state.manual_primary_until)} · ${degradeLabel}`
  return (
    <span className='text-primary shrink-0' title={label} aria-label={label}>
      <HugeiconsIcon icon={PinIcon} className='size-3.5' aria-hidden='true' />
    </span>
  )
}

function TrafficDistribution(props: {
  routes: readonly ChannelMonitorSmartScheduleRoute[]
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>
}) {
  const trafficRoutes = props.routes
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
        item.share != null && item.share > 0
    )
    .sort((first, second) => second.share - first.share)
  const visibleTrafficRoutes = trafficRoutes.slice(0, 4)
  const otherTrafficShare = trafficRoutes
    .slice(4)
    .reduce((total, item) => total + item.share, 0)

  if (visibleTrafficRoutes.length === 0) {
    return (
      <div className='text-muted-foreground py-1 text-xs'>当前没有流入渠道</div>
    )
  }

  return (
    <div className='grid gap-2'>
      {visibleTrafficRoutes.map((item) => {
        const percentage = item.share * 100
        return (
          <div
            key={channelMonitorSmartScheduleRouteKey(item.route)}
            className='grid grid-cols-[minmax(0,10rem)_minmax(5rem,1fr)_3.75rem] items-center gap-2 text-xs'
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
        <div className='grid grid-cols-[minmax(0,10rem)_minmax(5rem,1fr)_3.75rem] items-center gap-2 text-xs'>
          <span className='text-muted-foreground'>其他渠道</span>
          <span className='bg-muted h-1.5 overflow-hidden rounded-full'>
            <span
              className='bg-muted-foreground/50 block h-full rounded-full'
              style={{ width: `${Math.max(2, otherTrafficShare * 100)}%` }}
            />
          </span>
          <span className='text-right font-mono tabular-nums'>
            {(otherTrafficShare * 100).toFixed(1)}%
          </span>
        </div>
      ) : null}
    </div>
  )
}

function RouteActions(props: {
  route: ChannelMonitorSmartScheduleRoute
  disabled: boolean
  detailsExpanded: boolean
  onSetPrimary: (route: ChannelMonitorSmartScheduleRoute) => void
  onOpenDetails: (route: ChannelMonitorSmartScheduleRoute) => void
}) {
  const fixed = props.route.state.manual_primary_until > 0
  return (
    <div className='flex items-center justify-end gap-1'>
      <Button
        type='button'
        variant='ghost'
        size='icon-xs'
        className={cn(fixed && 'text-primary')}
        disabled={
          props.disabled ||
          !channelMonitorSmartScheduleRouteParticipates(props.route)
        }
        onClick={() => props.onSetPrimary(props.route)}
        aria-label={
          fixed
            ? `重新设置 ${props.route.channel_name} 的固定时长`
            : `固定 ${props.route.channel_name} 为主渠道`
        }
        title={fixed ? '重新设置固定时长' : '固定为主渠道'}
      >
        <HugeiconsIcon icon={PinIcon} aria-hidden='true' />
      </Button>
      <Button
        type='button'
        variant='ghost'
        size='icon-xs'
        onClick={() => props.onOpenDetails(props.route)}
        aria-label={`查看 ${props.route.channel_name} 的调度详情`}
        aria-expanded={props.detailsExpanded}
        aria-controls={`channel-monitor-route-details-${props.route.channel_id}`}
        title='查看调度详情'
      >
        <HugeiconsIcon icon={ViewIcon} aria-hidden='true' />
      </Button>
    </div>
  )
}

export function ChannelMonitorSmartSchedulePool(
  props: ChannelMonitorSmartSchedulePoolProps
) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<RouteFilter>('all')
  const [sort, setSort] = useState<RouteSort>('cost')
  const [detailRouteKey, setDetailRouteKey] = useState<string | null>(null)
  const status = getChannelMonitorSmartSchedulePoolStatus(props.pool.summary)
  const filteredRoutes = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase()
    return props.pool.routes
      .filter((route) => {
        const key = channelMonitorSmartScheduleRouteKey(route)
        const channel = props.channelsById.get(route.channel_id)
        const remark = channel?.channel_remark || channel?.remark || ''
        const matchesQuery =
          normalizedQuery === '' ||
          route.channel_name.toLowerCase().includes(normalizedQuery) ||
          String(route.channel_id).includes(normalizedQuery) ||
          remark.toLowerCase().includes(normalizedQuery)
        return (
          matchesQuery &&
          routeMatchesFilter(route, props.placements.get(key), filter)
        )
      })
      .sort((first, second) => {
        const firstKey = channelMonitorSmartScheduleRouteKey(first)
        const secondKey = channelMonitorSmartScheduleRouteKey(second)
        if (sort === 'traffic') {
          const trafficOrder = compareNullableDescending(
            props.placements.get(firstKey)?.estimatedShare,
            props.placements.get(secondKey)?.estimatedShare
          )
          if (trafficOrder !== 0) return trafficOrder
          return (
            compareChannelMonitorSmartScheduleRoutesByAttention(
              first,
              second,
              props.placements
            ) ||
            first.channel_name.localeCompare(second.channel_name) ||
            first.channel_id - second.channel_id
          )
        }
        if (sort === 'score') {
          return compareNullableDescending(
            first.state.last_schedule_score,
            second.state.last_schedule_score
          )
        }
        if (sort === 'cost') {
          const firstRatio = props.channelsById.get(
            first.channel_id
          )?.cost_ratio
          const secondRatio = props.channelsById.get(
            second.channel_id
          )?.cost_ratio
          if (firstRatio == null && secondRatio == null) return 0
          if (firstRatio == null) return 1
          if (secondRatio == null) return -1
          return firstRatio - secondRatio
        }
        if (sort === 'name') {
          return (
            first.channel_name.localeCompare(second.channel_name) ||
            first.channel_id - second.channel_id
          )
        }
        return (
          compareChannelMonitorSmartScheduleRoutesByAttention(
            first,
            second,
            props.placements
          ) ||
          first.channel_name.localeCompare(second.channel_name) ||
          first.channel_id - second.channel_id
        )
      })
  }, [
    filter,
    props.channelsById,
    props.placements,
    props.pool.routes,
    query,
    sort,
  ])
  const detailRoute =
    props.pool.routes.find(
      (route) => channelMonitorSmartScheduleRouteKey(route) === detailRouteKey
    ) ?? null
  const detailPlacement = detailRoute
    ? props.placements.get(channelMonitorSmartScheduleRouteKey(detailRoute))
    : undefined
  const detailChannel = detailRoute
    ? props.channelsById.get(detailRoute.channel_id)
    : undefined
  const openDetails = (route: ChannelMonitorSmartScheduleRoute) => {
    setDetailRouteKey(channelMonitorSmartScheduleRouteKey(route))
  }

  return (
    <section
      className='border-border bg-card min-w-0 overflow-hidden rounded-md border'
      aria-label={`${props.pool.summary.model} 调度池`}
    >
      <header className='flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='min-w-0'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <h3
              className='truncate text-sm font-semibold'
              title={props.pool.summary.model}
            >
              {props.pool.summary.model}
            </h3>
            <Badge variant={getPoolStatusVariant(status)}>{status}</Badge>
          </div>
          <p className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs'>
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
          </p>
        </div>
      </header>

      <div className='bg-muted/15 grid gap-3 border-b px-4 py-3 lg:grid-cols-[8rem_minmax(0,1fr)]'>
        <div>
          <div className='text-sm font-medium'>预计流量分布</div>
          <div className='text-muted-foreground mt-0.5 text-xs'>当前流入层</div>
        </div>
        <TrafficDistribution
          routes={props.pool.routes}
          placements={props.placements}
        />
      </div>

      <div className='flex flex-col gap-2 border-b px-3 py-2.5 lg:flex-row lg:items-center'>
        <InputGroup className='lg:max-w-sm'>
          <InputGroupAddon>
            <HugeiconsIcon icon={Search01Icon} aria-hidden='true' />
          </InputGroupAddon>
          <InputGroupInput
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder='搜索渠道名称、ID 或备注'
            aria-label='搜索调度池渠道'
          />
          {query ? (
            <InputGroupAddon align='inline-end'>
              <InputGroupButton
                size='icon-xs'
                onClick={() => setQuery('')}
                aria-label='清空渠道搜索'
                title='清空搜索'
              >
                <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
              </InputGroupButton>
            </InputGroupAddon>
          ) : null}
        </InputGroup>
        <div className='flex min-w-0 flex-wrap items-center gap-2 lg:ml-auto'>
          <Select
            items={ROUTE_FILTER_OPTIONS}
            value={filter}
            onValueChange={(value) => {
              if (value !== null) setFilter(value as RouteFilter)
            }}
          >
            <SelectTrigger
              size='sm'
              className='w-36 text-xs'
              aria-label='按状态筛选'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='all'>全部状态</SelectItem>
                <SelectItem value='traffic'>正在承接流量</SelectItem>
                <SelectItem value='attention'>需要关注</SelectItem>
                <SelectItem value='backup'>备用渠道</SelectItem>
                <SelectItem value='excluded'>未参与</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            items={ROUTE_SORT_OPTIONS}
            value={sort}
            onValueChange={(value) => {
              if (value !== null) setSort(value as RouteSort)
            }}
          >
            <SelectTrigger
              size='sm'
              className='w-44 text-xs'
              aria-label='按渠道排序'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='schedule'>调度顺序</SelectItem>
                <SelectItem value='traffic'>预计流量从高到低</SelectItem>
                <SelectItem value='score'>最终得分从高到低</SelectItem>
                <SelectItem value='cost'>成本倍率从低到高</SelectItem>
                <SelectItem value='name'>渠道名称</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <span className='text-muted-foreground ml-auto text-xs tabular-nums lg:ml-1'>
            {filteredRoutes.length === props.pool.routes.length
              ? `共 ${props.pool.routes.length} 条渠道`
              : `显示 ${filteredRoutes.length} / ${props.pool.routes.length} 条`}
          </span>
        </div>
      </div>

      {filteredRoutes.length === 0 ? (
        <div className='px-4 py-10 text-center'>
          <p className='text-sm font-medium'>没有符合条件的渠道</p>
          <p className='text-muted-foreground mt-1 text-xs'>
            调整搜索内容或状态筛选后重试
          </p>
        </div>
      ) : (
        <>
          <div
            className='hidden max-h-[36rem] overflow-auto md:block'
            data-schedule-scroll-region='true'
          >
            <table
              className='w-full min-w-[60rem] table-fixed text-left text-xs tabular-nums'
              data-schedule-route-list='desktop-table'
            >
              <thead className='bg-card/95 sticky top-0 z-10 border-b supports-backdrop-filter:backdrop-blur-sm'>
                <tr className='text-muted-foreground'>
                  <th className='w-[22%] px-3 py-2 font-medium'>渠道</th>
                  <th className='w-[11%] px-2 py-2 font-medium'>状态</th>
                  <th className='w-[8%] px-2 py-2 font-medium'>成本倍率</th>
                  <th className='w-[8%] px-2 py-2 font-medium'>最终得分</th>
                  <th className='w-[10%] px-2 py-2 font-medium'>
                    优先级 / 权重
                  </th>
                  <th className='w-[9%] px-2 py-2 font-medium'>预计流量</th>
                  <th className='w-[17%] px-2 py-2 font-medium'>共享样本</th>
                  <th className='w-[7%] px-2 py-2 text-center font-medium'>
                    参与
                  </th>
                  <th className='w-[8%] px-3 py-2 text-right font-medium'>
                    操作
                  </th>
                </tr>
              </thead>
              <tbody className='divide-y'>
                {filteredRoutes.map((route) => {
                  const key = channelMonitorSmartScheduleRouteKey(route)
                  const channel = props.channelsById.get(route.channel_id)
                  const remark = channel?.channel_remark || channel?.remark
                  const placement = props.placements.get(key)
                  const updatePending = props.updateRouteKey === key
                  return (
                    <tr
                      key={key}
                      className='hover:bg-muted/35 h-15 transition-colors'
                      data-schedule-route-row={route.channel_id}
                    >
                      <td className='px-3 py-2 align-middle'>
                        <button
                          type='button'
                          className='focus-visible:ring-ring/50 block w-full min-w-0 rounded-sm text-left outline-none focus-visible:ring-3'
                          onClick={() => openDetails(route)}
                        >
                          <span className='flex min-w-0 items-center gap-1.5'>
                            <span
                              className='truncate font-medium'
                              title={route.channel_name}
                            >
                              {route.channel_name}
                            </span>
                            <ManualPrimaryIndicator route={route} />
                          </span>
                          <span className='text-muted-foreground mt-0.5 block truncate'>
                            ID {route.channel_id} · {remark || '暂无备注'}
                          </span>
                        </button>
                      </td>
                      <td className='px-2 py-2 align-middle'>
                        <ChannelMonitorSmartScheduleRouteStatus
                          route={route}
                          placement={placement}
                          onClearProtection={() =>
                            props.onClearProtection(route)
                          }
                        />
                      </td>
                      <td className='px-2 py-2 align-middle font-mono'>
                        {formatMonitorRatio(channel?.cost_ratio)}
                      </td>
                      <td className='px-2 py-2 align-middle font-mono font-medium'>
                        {route.state.last_schedule_score == null
                          ? '-'
                          : `${(route.state.last_schedule_score * 100).toFixed(1)} 分`}
                      </td>
                      <td className='px-2 py-2 align-middle font-mono'>
                        P{route.priority} · W{route.weight}
                      </td>
                      <td className='px-2 py-2 align-middle font-mono font-medium'>
                        {formatChannelMonitorSmartScheduleEstimatedShare(
                          placement
                        )}
                      </td>
                      <td className='px-2 py-2 align-middle'>
                        <RouteSamples route={route} />
                      </td>
                      <td className='px-2 py-2 text-center align-middle'>
                        <div className='flex items-center justify-center'>
                          {updatePending ? (
                            <Spinner className='size-4' />
                          ) : (
                            <Switch
                              checked={channelMonitorSmartScheduleRouteParticipates(
                                route
                              )}
                              disabled={props.updateDisabled}
                              onCheckedChange={(checked) =>
                                props.onParticipationChange(route, checked)
                              }
                              aria-label={`${route.channel_name} ${route.group} ${route.model} 参与智能调度`}
                            />
                          )}
                        </div>
                      </td>
                      <td className='px-3 py-2 align-middle'>
                        <RouteActions
                          route={route}
                          disabled={props.updateDisabled}
                          detailsExpanded={detailRouteKey === key}
                          onSetPrimary={props.onSetPrimary}
                          onOpenDetails={openDetails}
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          <div
            className='max-h-[42rem] divide-y overflow-y-auto md:hidden'
            data-schedule-route-list='mobile-list'
          >
            {filteredRoutes.map((route) => {
              const key = channelMonitorSmartScheduleRouteKey(route)
              const channel = props.channelsById.get(route.channel_id)
              const remark = channel?.channel_remark || channel?.remark
              const placement = props.placements.get(key)
              const updatePending = props.updateRouteKey === key
              return (
                <article key={key} className='px-3 py-3'>
                  <div className='flex min-w-0 items-start justify-between gap-3'>
                    <button
                      type='button'
                      className='focus-visible:ring-ring/50 min-w-0 flex-1 rounded-sm text-left outline-none focus-visible:ring-3'
                      onClick={() => openDetails(route)}
                    >
                      <span className='flex min-w-0 items-center gap-1.5'>
                        <span className='truncate text-sm font-medium'>
                          {route.channel_name}
                        </span>
                        <ManualPrimaryIndicator route={route} />
                      </span>
                      <span className='text-muted-foreground mt-0.5 block truncate text-xs'>
                        ID {route.channel_id} · {remark || '暂无备注'}
                      </span>
                    </button>
                    <ChannelMonitorSmartScheduleRouteStatus
                      route={route}
                      placement={placement}
                      onClearProtection={() => props.onClearProtection(route)}
                    />
                  </div>

                  <div className='mt-3 grid grid-cols-3 gap-3 border-y py-2 text-xs'>
                    <div>
                      <span className='text-muted-foreground block'>得分</span>
                      <span className='font-mono font-medium'>
                        {route.state.last_schedule_score == null
                          ? '-'
                          : `${(route.state.last_schedule_score * 100).toFixed(1)} 分`}
                      </span>
                    </div>
                    <div>
                      <span className='text-muted-foreground block'>路由</span>
                      <span className='font-mono'>
                        P{route.priority} · W{route.weight}
                      </span>
                    </div>
                    <div>
                      <span className='text-muted-foreground block'>流量</span>
                      <span className='font-mono font-medium'>
                        {formatChannelMonitorSmartScheduleEstimatedShare(
                          placement
                        )}
                      </span>
                    </div>
                  </div>

                  <div className='mt-2 flex items-center justify-between gap-3'>
                    <RouteSamples route={route} />
                    <div className='flex shrink-0 items-center gap-2'>
                      {updatePending ? (
                        <Spinner className='size-4' />
                      ) : (
                        <Switch
                          checked={channelMonitorSmartScheduleRouteParticipates(
                            route
                          )}
                          disabled={props.updateDisabled}
                          onCheckedChange={(checked) =>
                            props.onParticipationChange(route, checked)
                          }
                          aria-label={`${route.channel_name} ${route.group} ${route.model} 参与智能调度`}
                        />
                      )}
                      <RouteActions
                        route={route}
                        disabled={props.updateDisabled}
                        detailsExpanded={detailRouteKey === key}
                        onSetPrimary={props.onSetPrimary}
                        onOpenDetails={openDetails}
                      />
                    </div>
                  </div>
                </article>
              )
            })}
          </div>
        </>
      )}

      <ChannelMonitorSmartScheduleRouteDetails
        open={detailRoute !== null}
        route={detailRoute}
        channel={detailChannel}
        placement={detailPlacement}
        updatePending={
          detailRoute != null &&
          props.updateRouteKey ===
            channelMonitorSmartScheduleRouteKey(detailRoute)
        }
        updateDisabled={props.updateDisabled}
        onOpenChange={(open) => {
          if (!open) setDetailRouteKey(null)
        }}
        onParticipationChange={props.onParticipationChange}
        onClearProtection={props.onClearProtection}
        onSetPrimary={props.onSetPrimary}
        onClearPrimary={props.onClearPrimary}
      />
    </section>
  )
}
