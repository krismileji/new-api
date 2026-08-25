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
import { Activity01Icon, Refresh01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ChannelMonitorStatusWindow,
  ChannelMonitorStatusWindowDetails,
  type ChannelMonitorStatusWindowPresentation,
} from '@/features/channel-monitor/components/channel-monitor-status-window'
import { formatChannelMonitorStatusWindowRange } from '@/features/channel-monitor/lib/status-window'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getPricingGroupMonitor } from './api'
import type {
  ChannelGroupMonitorBucket,
  ChannelGroupMonitorStatus,
  PricingGroupMonitor,
} from './types'

const STATUS_PRESENTATION: Record<
  ChannelGroupMonitorStatus,
  { label: string; dot: string; badge: 'secondary' | 'warning' | 'destructive' }
> = {
  unconfigured: {
    label: '未配置',
    dot: 'bg-muted-foreground/60',
    badge: 'secondary',
  },
  paused: {
    label: '已停用',
    dot: 'bg-muted-foreground/60',
    badge: 'secondary',
  },
  pending: { label: '待检测', dot: 'bg-primary', badge: 'secondary' },
  healthy: { label: '正常', dot: 'bg-success', badge: 'secondary' },
  unavailable: {
    label: '无可用路由',
    dot: 'bg-destructive',
    badge: 'destructive',
  },
  unhealthy: { label: '异常', dot: 'bg-destructive', badge: 'destructive' },
  rate_limited: { label: '波动', dot: 'bg-warning', badge: 'warning' },
  stale: { label: '数据过期', dot: 'bg-warning', badge: 'warning' },
}

const DISPLAY_UNIT_LABEL = {
  minute: '分钟',
  hour: '小时',
  day: '天',
} as const

function formatLatency(value: number | null): string {
  if (value == null) return '--'
  if (value >= 1000) return `${(value / 1000).toFixed(2)} 秒`
  return `${Math.round(value)} 毫秒`
}

function formatHoverDuration(value: number | null): string {
  if (value == null || !Number.isFinite(value)) return '--'
  return `${(value / 1000).toFixed(2)} 秒`
}

function formatRate(value: number | null): string {
  if (value == null) return '--'
  return `${value.toFixed(1)}%`
}

function formatTPS(value: number | null): string {
  if (value == null || !Number.isFinite(value)) return '--'
  return value.toFixed(value >= 100 ? 0 : 1)
}

function averageBucketMetric(
  total: number | undefined,
  sampleCount: number | undefined
): number | null {
  if (
    sampleCount == null ||
    sampleCount <= 0 ||
    !Number.isFinite(sampleCount)
  ) {
    return null
  }
  const normalizedTotal = total ?? 0
  if (!Number.isFinite(normalizedTotal)) return null
  return normalizedTotal / sampleCount
}

const BUCKET_RESULT_LABEL: Record<
  Exclude<ChannelGroupMonitorBucket['result'], ''>,
  string
> = {
  success: '成功',
  upstream_failure: '上游失败',
  rate_limited: '限流',
  local_failure: '本地失败',
  unavailable: '无可用路由',
  skipped: '跳过',
  timeout: '超时',
}

const BUCKET_RESULT_COLOR: Record<
  Exclude<ChannelGroupMonitorBucket['result'], ''>,
  string
> = {
  success: 'bg-success',
  upstream_failure: 'bg-destructive',
  rate_limited: 'bg-warning',
  local_failure: 'bg-warning/70',
  unavailable: 'bg-destructive/70',
  skipped: 'bg-muted-foreground/50',
  timeout: 'bg-warning',
}

function groupMonitorBucketPresentation(
  bucket: ChannelGroupMonitorBucket,
  displayUnit: PricingGroupMonitor['display_unit'],
  enabled: boolean
): ChannelMonitorStatusWindowPresentation & {
  status: string
  statusVariant: 'secondary' | 'warning' | 'destructive' | 'outline'
  description?: string
} {
  const timeRange = formatChannelMonitorStatusWindowRange(
    bucket.started_at,
    displayUnit
  )
  const result = bucket.latest_result || bucket.result
  const hasLatest = Boolean(bucket.latest_result)
  if (!result) {
    return {
      ariaLabel: `${timeRange} · ${enabled ? '已开启但未执行' : '未安排探测'}`,
      className: enabled ? 'bg-muted-foreground/35' : 'bg-muted/60',
      state: enabled ? 'not-executed' : 'not-scheduled',
      status: enabled ? '未执行' : '未安排',
      statusVariant: enabled ? 'secondary' : 'outline',
      description: enabled
        ? '周期探测已开启，但本时间格内没有执行。'
        : '分组监控当前未开启周期探测。',
    }
  }
  let statusVariant: 'secondary' | 'warning' | 'destructive' | 'outline' =
    'destructive'
  if (result === 'success') {
    statusVariant = 'secondary'
  } else if (result === 'rate_limited' || result === 'local_failure') {
    statusVariant = 'warning'
  } else if (result === 'skipped') {
    statusVariant = 'outline'
  } else if (result === 'timeout') {
    statusVariant = 'warning'
  }
  const firstToken = formatHoverDuration(
    hasLatest
      ? (bucket.latest_first_token_ms ?? null)
      : averageBucketMetric(
          bucket.first_token_total_ms,
          bucket.first_token_sample_count
        )
  )
  const tps = formatTPS(
    hasLatest
      ? (bucket.latest_tps ?? null)
      : averageBucketMetric(bucket.tps_total, bucket.tps_sample_count)
  )
  const responseTime = formatHoverDuration(
    hasLatest
      ? (bucket.latest_response_time_ms ?? null)
      : averageBucketMetric(
          bucket.response_time_total_ms,
          bucket.response_time_sample_count
        )
  )
  return {
    ariaLabel: `${timeRange} · ${BUCKET_RESULT_LABEL[result]} · 首字 ${firstToken} · TPS ${tps} · 耗时 ${responseTime}`,
    className: BUCKET_RESULT_COLOR[result],
    state: 'executed',
    status: BUCKET_RESULT_LABEL[result],
    statusVariant,
  }
}

export function GroupMonitorBucketDetails(props: {
  bucket: ChannelGroupMonitorBucket
  displayUnit: PricingGroupMonitor['display_unit']
  enabled: boolean
}) {
  const presentation = groupMonitorBucketPresentation(
    props.bucket,
    props.displayUnit,
    props.enabled
  )
  const hasLatest = Boolean(props.bucket.latest_result)
  const firstToken = hasLatest
    ? formatHoverDuration(props.bucket.latest_first_token_ms ?? null)
    : formatHoverDuration(
        averageBucketMetric(
          props.bucket.first_token_total_ms,
          props.bucket.first_token_sample_count
        )
      )
  const tps = hasLatest
    ? formatTPS(props.bucket.latest_tps ?? null)
    : formatTPS(
        averageBucketMetric(
          props.bucket.tps_total,
          props.bucket.tps_sample_count
        )
      )
  const responseTime = hasLatest
    ? formatHoverDuration(props.bucket.latest_response_time_ms ?? null)
    : formatHoverDuration(
        averageBucketMetric(
          props.bucket.response_time_total_ms,
          props.bucket.response_time_sample_count
        )
      )
  return (
    <ChannelMonitorStatusWindowDetails
      timeRange={formatChannelMonitorStatusWindowRange(
        props.bucket.started_at,
        props.displayUnit
      )}
      status={presentation.status}
      statusVariant={presentation.statusVariant}
      description={presentation.description}
      details={
        props.bucket.result
          ? [
              {
                label: '首字',
                value: firstToken,
              },
              {
                label: 'TPS',
                value: tps,
              },
              {
                label: '耗时',
                value: responseTime,
              },
            ]
          : undefined
      }
    />
  )
}

function GroupMonitorSkeleton() {
  return (
    <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
      {Array.from({ length: 6 }, (_, index) => (
        <Skeleton key={index} className='h-44 rounded-lg' />
      ))}
    </div>
  )
}

export function GroupMonitorContent(props: { result: PricingGroupMonitor }) {
  if (props.result.items.length === 0) {
    return (
      <Empty className='min-h-80 border border-dashed'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Activity01Icon} />
          </EmptyMedia>
          <EmptyTitle>暂无分组监控</EmptyTitle>
          <EmptyDescription>当前账号没有可展示的分组状态</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
      {props.result.items.map((item) => {
        const presentation = STATUS_PRESENTATION[item.status]
        const updatedAt = formatTimestampToDate(item.last_finished_at)
        const recentWindow = item.recent_window ?? []
        return (
          <article
            key={item.group}
            className='border-border/70 bg-card relative min-w-0 overflow-hidden rounded-lg border p-4 shadow-xs'
          >
            <div
              aria-hidden
              className={cn('absolute inset-y-0 left-0 w-1', presentation.dot)}
            />
            <div className='flex min-w-0 items-start justify-between gap-3 pl-2'>
              <div className='flex min-w-0 items-center gap-3'>
                <span className='bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center rounded-md text-base font-semibold'>
                  {item.initial || '?'}
                </span>
                <h2
                  className='truncate text-base font-semibold'
                  title={item.group}
                >
                  {item.group}
                </h2>
              </div>
              <Badge variant={presentation.badge}>{presentation.label}</Badge>
            </div>

            <dl className='mt-5 grid grid-cols-2 gap-x-4 gap-y-4 pl-2'>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>首字响应</dt>
                <dd className='mt-1 truncate font-mono text-sm font-medium tabular-nums'>
                  {formatLatency(item.latest_first_token_ms)}
                </dd>
              </div>
              <div className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>成功率</dt>
                <dd className='mt-1 font-mono text-sm font-medium tabular-nums'>
                  {formatRate(item.success_rate)}
                </dd>
              </div>
              <div className='col-span-2 min-w-0'>
                <dt className='text-muted-foreground text-xs'>探测模型</dt>
                <dd
                  className='mt-1 truncate font-mono text-xs tabular-nums'
                  title={item.probe_model || undefined}
                >
                  {item.probe_model || '--'}
                </dd>
              </div>
              <div className='col-span-2 min-w-0'>
                <dt className='text-muted-foreground text-xs'>更新时间</dt>
                <dd
                  className='mt-1 truncate font-mono text-xs tabular-nums'
                  title={updatedAt}
                >
                  {updatedAt}
                </dd>
              </div>
            </dl>
            <div className='mt-5 pl-2'>
              <div className='text-muted-foreground mb-1.5 flex items-center justify-between gap-2 text-[11px]'>
                <span>
                  近 {props.result.display_value}{' '}
                  {DISPLAY_UNIT_LABEL[props.result.display_unit]} 状态
                </span>
                <span className='tabular-nums'>
                  {recentWindow.length} 个时间格
                </span>
              </div>
              <ChannelMonitorStatusWindow
                buckets={recentWindow}
                bucketSlot='group-monitor-bucket'
                bucketStateDataAttribute='data-group-monitor-bucket-state'
                gridProps={{
                  'aria-label': `${item.group} 近 ${props.result.display_value} ${DISPLAY_UNIT_LABEL[props.result.display_unit]}分组监控结果`,
                  'data-window-buckets': recentWindow.length,
                  'data-group-monitor-window-value': props.result.display_value,
                  'data-group-monitor-window-unit': props.result.display_unit,
                }}
                getBucketPresentation={(bucket) =>
                  groupMonitorBucketPresentation(
                    bucket,
                    props.result.display_unit,
                    props.result.enabled
                  )
                }
                renderDetails={(bucket) => (
                  <GroupMonitorBucketDetails
                    bucket={bucket}
                    displayUnit={props.result.display_unit}
                    enabled={props.result.enabled}
                  />
                )}
              />
            </div>
          </article>
        )
      })}
    </div>
  )
}

export function GroupMonitor() {
  const query = useQuery({
    queryKey: ['pricing', 'group-monitor'],
    queryFn: getPricingGroupMonitor,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  })
  const result = query.data?.data

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[1320px] px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
        <header className='mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between'>
          <div>
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <HugeiconsIcon icon={Activity01Icon} aria-hidden='true' />
              服务状态
            </div>
            <h1 className='mt-2 text-3xl font-semibold tracking-normal'>
              分组监控
            </h1>
            {result ? (
              <div className='mt-2 flex flex-wrap items-center gap-2'>
                <p className='text-muted-foreground text-sm'>
                  成功率按近 {result.display_value}{' '}
                  {DISPLAY_UNIT_LABEL[result.display_unit]}内的有效逻辑探测统计
                </p>
                {!result.enabled ? (
                  <Badge variant='secondary'>分组监控已停用</Badge>
                ) : null}
              </div>
            ) : null}
          </div>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => void query.refetch()}
                  disabled={query.isFetching}
                  aria-label='刷新分组监控'
                >
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    className={query.isFetching ? 'animate-spin' : undefined}
                  />
                </Button>
              }
            />
            <TooltipContent>刷新</TooltipContent>
          </Tooltip>
        </header>

        {query.isLoading ? <GroupMonitorSkeleton /> : null}
        {query.isError && !result ? (
          <Empty className='min-h-80 border border-dashed'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <HugeiconsIcon icon={Activity01Icon} />
              </EmptyMedia>
              <EmptyTitle>分组监控加载失败</EmptyTitle>
              <EmptyDescription>请刷新后重试</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : null}
        {result ? <GroupMonitorContent result={result} /> : null}
      </PageTransition>
    </PublicLayout>
  )
}
