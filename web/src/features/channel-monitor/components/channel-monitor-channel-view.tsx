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
  Edit02Icon,
  GaugeIcon,
  HistoryIcon,
  Layers01Icon,
  PowerOffIcon,
  PowerServiceIcon,
  Refresh01Icon,
  Settings02Icon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getChannelMonitorStatusLabel } from '../constants'
import { formatChannelMonitorCost, formatMonitorRatio } from '../lib/format'
import type {
  ChannelMonitorChannelPerformance,
  ChannelMonitorItem,
  ChannelMonitorSuccessSummary,
} from '../types'
import { ChannelMonitorFetchStatus } from './channel-monitor-fetch-status'
import {
  ChannelMonitorFirstTokenValue,
  ChannelMonitorTPSValue,
} from './channel-monitor-performance-value'
import { ChannelMonitorSmartScheduleCell } from './channel-monitor-smart-schedule-cell'
import { ChannelMonitorStatusBadge } from './channel-monitor-status-badge'
import { ChannelMonitorSuccessRateValue } from './channel-monitor-success-rate-value'
import { GroupRatioValue } from './group-ratio-value'
import { RatioChangeBadge } from './ratio-change-badge'

type ChannelMonitorChannelViewProps = {
  channels: ChannelMonitorItem[]
  groupRatios: Record<string, number>
  groupCoefficients: Record<string, number>
  performanceByChannel: Map<number, ChannelMonitorChannelPerformance>
  successByChannel: Map<number, ChannelMonitorSuccessSummary>
  successMetricsAvailable: boolean
  performanceRangeLabel: string
  performanceLoading: boolean
  performanceError: boolean
  onFetchUpstreamBalance: (channel: ChannelMonitorItem) => void
  onFetchUpstreamRatio: (channel: ChannelMonitorItem) => void
  onToggleStatus: (channel: ChannelMonitorItem) => void
  onTestConnection: (channel: ChannelMonitorItem) => void
  onEditRatio: (channel: ChannelMonitorItem) => void
  onEditConcurrency: (channel: ChannelMonitorItem) => void
  onEditGroups: (channel: ChannelMonitorItem) => void
  onConfigureUpstream: (channel: ChannelMonitorItem) => void
  onViewHistory: (channel: ChannelMonitorItem) => void
  onOpenCostHistory: (channel: ChannelMonitorItem) => void
  onOpenSuccessDetail: (channel: ChannelMonitorItem) => void
  onUpdateSmartSchedule: (
    channel: ChannelMonitorItem,
    excluded: boolean
  ) => void
  smartScheduleEnabled: boolean
  fetchingBalanceChannelId: number | null
  fetchingRatioChannelId: number | null
  updatingStatusChannelId: number | null
  updatingSmartScheduleChannelId: number | null
}

type ChannelActionButtonProps = {
  label: string
  icon: React.ComponentProps<typeof HugeiconsIcon>['icon']
  onClick: () => void
  disabled?: boolean
  loading?: boolean
  className?: string
  size?: 'icon-xs' | 'icon-sm'
}

type ChannelPerformanceCellProps = {
  performance: ChannelMonitorChannelPerformance | undefined
  loading: boolean
  error: boolean
}

type ChannelUpstreamBalanceCellProps = {
  channel: ChannelMonitorItem
}

type ChannelMonitorUpdateMetaProps = {
  timestamp: number
  className?: string
}

const upstreamBalanceFormatter = new Intl.NumberFormat(undefined, {
  minimumFractionDigits: 0,
  maximumFractionDigits: 4,
})

function ChannelMonitorUpdateMeta(props: ChannelMonitorUpdateMetaProps) {
  if (props.timestamp <= 0) {
    return null
  }

  const label = `更新：${formatTimestampToDate(props.timestamp)}`

  return (
    <div
      className={cn(
        'text-muted-foreground flex w-full min-w-0 flex-col items-start gap-0.5 text-xs leading-4',
        props.className
      )}
    >
      <span className='block w-full truncate' title={label}>
        {label}
      </span>
    </div>
  )
}

function ChannelActionButton(props: ChannelActionButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size={props.size ?? 'icon-sm'}
            onClick={props.onClick}
            disabled={props.disabled}
            aria-label={props.label}
            className={props.className}
          >
            {props.loading ? <Spinner /> : <HugeiconsIcon icon={props.icon} />}
          </Button>
        }
      />
      <TooltipContent>{props.label}</TooltipContent>
    </Tooltip>
  )
}

function ChannelPerformanceCell(props: ChannelPerformanceCellProps) {
  if (props.loading) {
    return <Skeleton className='h-9 w-28' />
  }
  if (props.error) {
    return <span className='text-destructive text-xs'>加载失败</span>
  }
  if (!props.performance) {
    return <span className='text-muted-foreground text-xs'>暂无样本</span>
  }
  return (
    <div className='flex w-full flex-col items-start gap-0.5 text-xs'>
      <div className='flex items-baseline gap-1.5'>
        <span className='text-muted-foreground'>首字</span>
        <ChannelMonitorFirstTokenValue
          value={props.performance.average_first_token_ms}
        />
      </div>
      <div className='flex items-baseline gap-1.5'>
        <span className='text-muted-foreground'>TPS</span>
        <ChannelMonitorTPSValue value={props.performance.average_tps} />
      </div>
      <span className='text-muted-foreground'>
        {props.performance.sample_count} 次请求
      </span>
    </div>
  )
}

function ChannelUpstreamBalanceCell(props: ChannelUpstreamBalanceCellProps) {
  if (!props.channel.upstream) {
    return <span className='text-muted-foreground'>-</span>
  }
  if (!props.channel.upstream.balance_sync_enabled) {
    return (
      <span
        className='text-muted-foreground block max-w-full truncate text-xs'
        title='余额同步已关闭'
      >
        余额同步已关闭
      </span>
    )
  }
  if (props.channel.upstream_balance == null) {
    if (props.channel.last_balance_error) {
      return (
        <span
          className='text-destructive text-xs'
          title={props.channel.last_balance_error}
        >
          无法获取
        </span>
      )
    }
    return <span className='text-muted-foreground text-xs'>暂无</span>
  }

  const titleParts: string[] = []
  if (props.channel.last_balance_error) {
    titleParts.push(`最近更新失败：${props.channel.last_balance_error}`)
  }
  const warningThreshold =
    props.channel.upstream?.balance_warning_threshold ?? null
  const balanceWarning =
    warningThreshold != null &&
    props.channel.upstream_balance < warningThreshold
  if (warningThreshold != null) {
    titleParts.push(
      `余额预警值：${upstreamBalanceFormatter.format(warningThreshold)}`
    )
  }
  return (
    <div
      className='flex w-full min-w-0 flex-col items-start gap-0.5'
      title={titleParts.join('；')}
    >
      <span
        className={cn(
          'font-mono font-semibold',
          balanceWarning && 'text-destructive'
        )}
      >
        {upstreamBalanceFormatter.format(props.channel.upstream_balance)}
      </span>
      {balanceWarning ? (
        <span className='text-destructive text-xs'>低于预警值</span>
      ) : null}
      {props.channel.last_balance_error ? (
        <span className='text-warning text-xs'>更新失败</span>
      ) : null}
      <ChannelMonitorUpdateMeta timestamp={props.channel.last_balance_time} />
    </div>
  )
}

function ChannelTodayCostCell(props: {
  channel: ChannelMonitorItem
  onOpenCostHistory: (channel: ChannelMonitorItem) => void
}) {
  let costTitle: string | undefined
  if (!props.channel.today_cost_configured) {
    costTitle = '查看每日成本详情'
  } else if (props.channel.today_cost_unresolved_count > 0) {
    costTitle = `今日有 ${props.channel.today_cost_unresolved_count} 次成本未确认`
  }

  return (
    <Button
      type='button'
      variant='link'
      onClick={() => props.onOpenCostHistory(props.channel)}
      aria-label={`查看渠道 ${props.channel.name} 的今日成本详情`}
      className='h-auto max-w-full min-w-0 justify-start p-0 text-left font-normal'
      title={costTitle}
    >
      {!props.channel.today_cost_configured ? (
        <span className='text-muted-foreground text-xs'>未配置</span>
      ) : (
        <span className='flex min-w-0 flex-col items-start gap-0.5'>
          <span className='font-mono font-semibold tabular-nums'>
            {formatChannelMonitorCost(props.channel.today_cost_cny)}
          </span>
          {!props.channel.today_cost_complete ? (
            <span className='text-warning text-xs'>不完整</span>
          ) : null}
        </span>
      )}
    </Button>
  )
}

export function ChannelMonitorChannelView(
  props: ChannelMonitorChannelViewProps
) {
  if (props.channels.length === 0) {
    return (
      <Empty className='min-h-72'>
        <EmptyHeader>
          <EmptyTitle>当前筛选下没有渠道</EmptyTitle>
          <EmptyDescription>切换上游类型或调整搜索条件</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='overflow-hidden rounded-lg border'>
      <Table
        className={cn(
          'table-fixed [&_td]:align-top [&_td]:overflow-hidden [&_td]:py-3',
          props.smartScheduleEnabled ? 'min-w-[1744px]' : 'min-w-[1584px]'
        )}
      >
        <colgroup>
          <col className='w-[160px]' />
          <col className='w-[208px]' />
          <col className='w-[128px]' />
          <col className='w-[224px]' />
          <col className='w-[200px]' />
          <col className='w-[176px]' />
          <col className='w-[128px]' />
          <col className='w-[136px]' />
          <col className='w-[120px]' />
          {props.smartScheduleEnabled ? <col className='w-[160px]' /> : null}
          <col className='w-[104px]' />
        </colgroup>
        <TableHeader>
          <TableRow className='[&_th]:text-left'>
            <TableHead>渠道</TableHead>
            <TableHead className='pl-[34px]'>上游余额</TableHead>
            <TableHead>今日成本</TableHead>
            <TableHead className='pl-[34px]'>成本倍率</TableHead>
            <TableHead>倍率更新状态</TableHead>
            <TableHead>关联分组</TableHead>
            <TableHead>性能（{props.performanceRangeLabel}）</TableHead>
            <TableHead
              className='whitespace-normal'
              title='按真实上游调用统计，包含重试过程中的失败'
            >
              成功率（{props.performanceRangeLabel}）
            </TableHead>
            <TableHead>并发限制</TableHead>
            {props.smartScheduleEnabled ? (
              <TableHead>智能调度</TableHead>
            ) : null}
            <TableHead>操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.channels.map((channel) => {
            const channelEnabled = channel.status === CHANNEL_STATUS.ENABLED
            const successMetric = props.successByChannel.get(channel.id)
            const channelStatusLabel = `渠道状态：${getChannelMonitorStatusLabel(channel.status)}`
            const refreshesMetricsTogether =
              channel.upstream?.type === 'custom' &&
              channel.upstream.ratio_sync_enabled &&
              channel.upstream.balance_sync_enabled &&
              channel.upstream.custom_config?.balance_reuse_ratio_request ===
                true
            const refreshingMetricsTogether =
              refreshesMetricsTogether &&
              (props.fetchingBalanceChannelId === channel.id ||
                props.fetchingRatioChannelId === channel.id)
            return (
              <TableRow key={channel.id} className='[&_td]:text-left'>
                <TableCell className='whitespace-normal'>
                  <div className='flex min-w-0 flex-col items-start gap-0.5'>
                    <div className='flex w-full min-w-0 flex-wrap items-center gap-1.5'>
                      <span
                        className={cn(
                          'size-2 shrink-0 rounded-full',
                          channelEnabled ? 'bg-success' : 'bg-destructive'
                        )}
                        role='img'
                        aria-label={channelStatusLabel}
                        title={channelStatusLabel}
                      />
                      <span
                        className='min-w-0 flex-1 truncate font-medium'
                        title={channel.name}
                      >
                        {channel.name}
                      </span>
                      {!channelEnabled && (
                        <ChannelMonitorStatusBadge
                          status={channel.status}
                          reason={channel.status_reason}
                          className='max-w-full shrink-0'
                        />
                      )}
                    </div>
                    {channel.channel_remark && (
                      <span
                        className='text-muted-foreground max-w-full truncate text-xs'
                        title={channel.channel_remark}
                      >
                        备注：{channel.channel_remark}
                      </span>
                    )}
                    <span className='text-muted-foreground text-xs'>
                      ID {channel.id}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <div className='grid w-full min-w-0 grid-cols-[24px_minmax(0,1fr)] items-start gap-x-0.5'>
                    {channel.upstream?.balance_sync_enabled ? (
                      <ChannelActionButton
                        label='更新上游余额'
                        icon={Refresh01Icon}
                        onClick={() => props.onFetchUpstreamBalance(channel)}
                        disabled={
                          props.fetchingBalanceChannelId !== null ||
                          props.fetchingRatioChannelId !== null
                        }
                        loading={
                          props.fetchingBalanceChannelId === channel.id ||
                          refreshingMetricsTogether
                        }
                        className='shrink-0'
                        size='icon-xs'
                      />
                    ) : null}
                    <div className='col-start-2 min-w-0'>
                      <ChannelUpstreamBalanceCell channel={channel} />
                    </div>
                  </div>
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <ChannelTodayCostCell
                    channel={channel}
                    onOpenCostHistory={props.onOpenCostHistory}
                  />
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <div className='grid w-full min-w-0 grid-cols-[24px_minmax(0,1fr)] items-start gap-x-0.5'>
                    {channel.upstream?.ratio_sync_enabled ? (
                      <ChannelActionButton
                        label='更新上游倍率'
                        icon={Refresh01Icon}
                        onClick={() => props.onFetchUpstreamRatio(channel)}
                        disabled={
                          props.fetchingBalanceChannelId !== null ||
                          props.fetchingRatioChannelId !== null
                        }
                        loading={
                          props.fetchingRatioChannelId === channel.id ||
                          refreshingMetricsTogether
                        }
                        className='shrink-0'
                        size='icon-xs'
                      />
                    ) : null}
                    <div className='col-start-2 min-w-0'>
                      <div className='flex min-w-0 items-center gap-2 whitespace-nowrap'>
                        <span className='font-mono text-base font-semibold'>
                          {formatMonitorRatio(channel.cost_ratio)}
                        </span>
                        <RatioChangeBadge
                          current={channel.cost_ratio}
                          previous={channel.previous_cost_ratio}
                        />
                      </div>
                      <ChannelMonitorUpdateMeta
                        timestamp={channel.updated_time}
                        className='mt-0.5'
                      />
                      {channel.upstream ? (
                        <span
                          className='text-muted-foreground mt-0.5 block w-full truncate text-xs'
                          title={`上游分组：${channel.upstream.group}`}
                        >
                          上游分组：{channel.upstream.group}
                        </span>
                      ) : null}
                    </div>
                  </div>
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <ChannelMonitorFetchStatus channel={channel} />
                </TableCell>
                <TableCell className='whitespace-normal'>
                  {channel.groups.length === 0 ? (
                    <span className='text-muted-foreground'>-</span>
                  ) : (
                    <div className='flex w-full min-w-0 flex-wrap gap-1'>
                      {channel.groups.map((group) => {
                        const groupRatio = props.groupRatios[group] ?? 1
                        const coefficient = props.groupCoefficients[group] ?? 1
                        return (
                          <Badge
                            key={group}
                            variant='outline'
                            className='max-w-full min-w-0'
                            title={group}
                          >
                            <span className='min-w-0 truncate'>{group}</span>
                            <span className='shrink-0'>
                              {' '}
                              ×{' '}
                              <GroupRatioValue
                                groupRatio={groupRatio}
                                costRatio={channel.cost_ratio}
                                coefficient={coefficient}
                              />
                            </span>
                          </Badge>
                        )
                      })}
                    </div>
                  )}
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <ChannelPerformanceCell
                    performance={props.performanceByChannel.get(channel.id)}
                    loading={props.performanceLoading}
                    error={props.performanceError}
                  />
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <ChannelMonitorSuccessRateValue
                    rate={successMetric?.actual_success_rate}
                    successCount={successMetric?.actual_success_count}
                    sampleCount={successMetric?.actual_sample_count}
                    available={props.successMetricsAvailable}
                    loading={props.performanceLoading}
                    error={props.performanceError}
                    onClick={() => props.onOpenSuccessDetail(channel)}
                    detailLabel={`查看 ${channel.name} 的成功率明细`}
                  />
                </TableCell>
                <TableCell className='whitespace-normal'>
                  {channel.concurrency_limit > 0 ? (
                    <div className='flex min-w-0 flex-col items-start gap-0.5'>
                      <span className='font-mono text-sm font-medium'>
                        {channel.concurrency_active}/{channel.concurrency_limit}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        当前/上限
                      </span>
                    </div>
                  ) : (
                    <span className='text-muted-foreground text-sm'>不限</span>
                  )}
                </TableCell>
                {props.smartScheduleEnabled ? (
                  <TableCell className='whitespace-normal'>
                    <ChannelMonitorSmartScheduleCell
                      channel={channel}
                      pending={
                        props.updatingSmartScheduleChannelId === channel.id
                      }
                      onUpdate={(excluded) =>
                        props.onUpdateSmartSchedule(channel, excluded)
                      }
                    />
                  </TableCell>
                ) : null}
                <TableCell>
                  <div className='inline-grid grid-cols-3 gap-0.5'>
                    <ChannelActionButton
                      label={
                        channel.status === CHANNEL_STATUS.ENABLED
                          ? '禁用渠道'
                          : '启用渠道'
                      }
                      icon={
                        channel.status === CHANNEL_STATUS.ENABLED
                          ? PowerOffIcon
                          : PowerServiceIcon
                      }
                      onClick={() => props.onToggleStatus(channel)}
                      disabled={props.updatingStatusChannelId !== null}
                      loading={props.updatingStatusChannelId === channel.id}
                      className={
                        channel.status === CHANNEL_STATUS.ENABLED
                          ? 'text-destructive hover:text-destructive'
                          : 'text-success hover:text-success'
                      }
                    />
                    <ChannelActionButton
                      label='测试连接'
                      icon={TestTubeIcon}
                      onClick={() => props.onTestConnection(channel)}
                    />
                    <ChannelActionButton
                      label={
                        channel.ratio == null ? '记录渠道倍率' : '修改渠道倍率'
                      }
                      icon={Edit02Icon}
                      onClick={() => props.onEditRatio(channel)}
                    />
                    <ChannelActionButton
                      label='设置并发限制'
                      icon={GaugeIcon}
                      onClick={() => props.onEditConcurrency(channel)}
                    />
                    <ChannelActionButton
                      label='更改关联分组'
                      icon={Layers01Icon}
                      onClick={() => props.onEditGroups(channel)}
                    />
                    <ChannelActionButton
                      label={channel.upstream ? '编辑上游配置' : '配置上游'}
                      icon={Settings02Icon}
                      onClick={() => props.onConfigureUpstream(channel)}
                    />
                    <ChannelActionButton
                      label='倍率变更历史'
                      icon={HistoryIcon}
                      onClick={() => props.onViewHistory(channel)}
                    />
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
