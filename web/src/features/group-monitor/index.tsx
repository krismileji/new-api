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
import { formatMonitorRatio } from '@/features/channel-monitor/lib/format'
import { formatChannelMonitorStatusWindowRange } from '@/features/channel-monitor/lib/status-window'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { useGroupMonitor } from './hooks/use-group-monitor'
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
    label: '暂不可用',
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

const GROUP_MONITOR_COLUMNS =
  'lg:grid-cols-[minmax(0,1fr)_5.5rem_7rem_5.5rem_5rem_minmax(0,1.4fr)]'
const GROUP_MONITOR_CACHE_COLUMNS =
  'lg:grid-cols-[minmax(0,1fr)_5.5rem_6rem_5.5rem_5rem_5.5rem_minmax(0,1.4fr)]'

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
  unavailable: '暂不可用',
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
    <div
      className='flex flex-col gap-3'
      role='status'
      aria-label='正在加载分组监控'
    >
      <Skeleton className='h-10 rounded-lg' />
      {Array.from({ length: 8 }, (_, index) => (
        <Skeleton key={index} className='h-40 rounded-lg lg:h-16' />
      ))}
    </div>
  )
}

export function GroupMonitorContent(props: { result: PricingGroupMonitor }) {
  const categories = new Map<string, PricingGroupMonitor['items']>()
  for (const category of props.result.categories ?? []) {
    categories.set(category, [])
  }
  for (const item of props.result.items) {
    const category = item.category?.trim() || '未分类'
    const items = categories.get(category)
    if (items) {
      items.push(item)
    } else {
      categories.set(category, [item])
    }
  }

  if (categories.size === 0) {
    return (
      <Empty className='min-h-80 border border-dashed'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Activity01Icon} />
          </EmptyMedia>
          <EmptyTitle>暂无分组监控</EmptyTitle>
          <EmptyDescription>暂未配置监控分类和分组</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex min-w-0 flex-col gap-5'>
      <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-lg border px-4 py-2.5 text-xs'>
        <div className='flex flex-wrap items-center gap-x-4 gap-y-1'>
          <span className='font-medium'>
            共 {props.result.items.length} 个分组
          </span>
          <span className='text-muted-foreground'>
            {categories.size} 个分类
          </span>
        </div>
        <Badge variant='outline'>
          <span
            aria-hidden='true'
            className={cn(
              'size-1.5 rounded-full',
              props.result.enabled ? 'bg-success' : 'bg-muted-foreground/60'
            )}
          />
          {props.result.enabled ? '监控运行中' : '监控已停用'}
        </Badge>
      </div>

      {[...categories].map(([category, items]) => (
        <section
          key={category}
          aria-label={category}
          className='flex min-w-0 flex-col gap-2'
        >
          <div className='flex min-w-0 items-center gap-2'>
            <h2 className='min-w-0 text-sm font-semibold break-words'>
              {category}
            </h2>
            <Badge variant='outline' className='shrink-0'>
              {items.length} 个分组
            </Badge>
          </div>
          {items.length === 0 ? (
            <p className='text-muted-foreground rounded-lg border border-dashed px-4 py-3 text-sm'>
              此分类暂无监控分组
            </p>
          ) : null}
          {items.length > 0 ? (
            <div className='bg-card min-w-0 overflow-hidden rounded-xl border'>
              <div
                aria-hidden='true'
                className={cn(
                  'bg-muted/40 text-muted-foreground hidden items-center gap-x-4 border-b px-4 py-2 text-xs lg:grid',
                  props.result.show_cache_rate
                    ? GROUP_MONITOR_CACHE_COLUMNS
                    : GROUP_MONITOR_COLUMNS
                )}
              >
                <span>分组 / 探测模型</span>
                <span>当前状态</span>
                <span>分组倍率</span>
                <span>首字响应</span>
                <span>成功率</span>
                {props.result.show_cache_rate ? <span>缓存率</span> : null}
                <span>
                  近 {props.result.display_value}{' '}
                  {DISPLAY_UNIT_LABEL[props.result.display_unit]} 状态
                </span>
              </div>
              <ul
                aria-label={`${category}分组列表`}
                className='divide-border/60 divide-y'
              >
                {items.map((item) => {
                  const presentation = STATUS_PRESENTATION[item.status]
                  const updatedAt = formatTimestampToDate(item.last_finished_at)
                  const recentWindow = item.recent_window ?? []
                  return (
                    <li key={item.group}>
                      <article
                        aria-label={item.group}
                        className={cn(
                          'hover:bg-muted/30 grid min-w-0 grid-cols-3 items-center gap-x-4 gap-y-3 px-4 py-3 transition-colors',
                          props.result.show_cache_rate
                            ? GROUP_MONITOR_CACHE_COLUMNS
                            : GROUP_MONITOR_COLUMNS
                        )}
                      >
                        <div className='col-span-2 flex min-w-0 items-center gap-2.5 lg:col-span-1'>
                          <span
                            aria-hidden='true'
                            className='bg-muted text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-md text-xs font-semibold'
                          >
                            {item.initial || '?'}
                          </span>
                          <div className='min-w-0'>
                            <h3
                              className='truncate text-sm font-semibold'
                              title={item.group}
                            >
                              {item.group}
                            </h3>
                            <p
                              className='text-muted-foreground mt-0.5 truncate font-mono text-[11px]'
                              title={item.probe_model || undefined}
                            >
                              <span className='sr-only'>探测模型：</span>
                              {item.probe_model || '--'}
                            </p>
                          </div>
                        </div>
                        <div className='flex justify-end lg:justify-start'>
                          <Badge variant={presentation.badge}>
                            <span
                              aria-hidden='true'
                              className={cn(
                                'size-1.5 shrink-0 rounded-full',
                                presentation.dot
                              )}
                            />
                            {presentation.label}
                          </Badge>
                        </div>

                        <dl
                          className={cn(
                            'col-span-3 grid gap-x-4 gap-y-3 lg:grid-cols-subgrid',
                            props.result.show_cache_rate
                              ? 'grid-cols-2 sm:grid-cols-4 lg:col-span-4'
                              : 'grid-cols-3'
                          )}
                        >
                          <div className='min-w-0'>
                            <dt className='text-muted-foreground mb-1 text-[11px] lg:sr-only'>
                              分组倍率
                            </dt>
                            <dd
                              className='truncate font-mono text-xs font-medium tabular-nums'
                              title={`${formatMonitorRatio(item.group_ratio ?? 1)}x`}
                            >
                              {formatMonitorRatio(item.group_ratio ?? 1)}x
                            </dd>
                          </div>
                          <div className='min-w-0'>
                            <dt className='text-muted-foreground mb-1 text-[11px] lg:sr-only'>
                              首字响应
                            </dt>
                            <dd className='truncate font-mono text-xs font-medium tabular-nums'>
                              {formatLatency(item.latest_first_token_ms)}
                            </dd>
                          </div>
                          <div className='min-w-0'>
                            <dt className='text-muted-foreground mb-1 text-[11px] lg:sr-only'>
                              成功率
                            </dt>
                            <dd className='font-mono text-xs font-medium tabular-nums'>
                              {formatRate(item.success_rate)}
                            </dd>
                          </div>
                          {props.result.show_cache_rate ? (
                            <div
                              className='min-w-0'
                              title='近 24 小时命中缓存的请求数 / 有效缓存样本数'
                            >
                              <dt className='text-muted-foreground mb-1 text-[11px] lg:sr-only'>
                                缓存率
                              </dt>
                              <dd className='font-mono text-xs font-medium tabular-nums'>
                                {item.cache_rate == null
                                  ? '暂无数据'
                                  : formatRate(item.cache_rate)}
                              </dd>
                            </div>
                          ) : null}
                        </dl>
                        <div className='col-span-3 min-w-0 lg:col-span-1'>
                          <div className='text-muted-foreground mb-1.5 flex items-center justify-between gap-2 text-[11px] lg:hidden'>
                            <span>
                              近 {props.result.display_value}{' '}
                              {DISPLAY_UNIT_LABEL[props.result.display_unit]}{' '}
                              状态
                            </span>
                            <span className='tabular-nums'>
                              {recentWindow.length} 个时间格
                            </span>
                          </div>
                          {recentWindow.length > 0 ? (
                            <ChannelMonitorStatusWindow
                              buckets={recentWindow}
                              bucketSlot='group-monitor-bucket'
                              bucketStateDataAttribute='data-group-monitor-bucket-state'
                              gridProps={{
                                'aria-label': `${item.group} 近 ${props.result.display_value} ${DISPLAY_UNIT_LABEL[props.result.display_unit]}分组监控结果`,
                                'data-window-buckets': recentWindow.length,
                                'data-group-monitor-window-value':
                                  props.result.display_value,
                                'data-group-monitor-window-unit':
                                  props.result.display_unit,
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
                          ) : (
                            <p className='text-muted-foreground text-[11px]'>
                              暂无探测记录
                            </p>
                          )}
                          <p
                            className='text-muted-foreground mt-1.5 truncate text-[10px] tabular-nums'
                            title={updatedAt}
                          >
                            更新时间：{updatedAt}
                          </p>
                        </div>
                      </article>
                    </li>
                  )
                })}
              </ul>
            </div>
          ) : null}
        </section>
      ))}
    </div>
  )
}

export function GroupMonitor() {
  const query = useGroupMonitor()
  const result = query.data?.data

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[1320px] px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
        <header className='mb-5 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between'>
          <div>
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <HugeiconsIcon icon={Activity01Icon} aria-hidden='true' />
              服务状态
            </div>
            <h1 className='mt-1 text-2xl font-semibold tracking-normal'>
              分组监控
            </h1>
            {result ? (
              <div className='mt-2 flex flex-wrap items-center gap-2'>
                <p className='text-muted-foreground text-sm'>
                  成功率按近 {result.display_value}{' '}
                  {DISPLAY_UNIT_LABEL[result.display_unit]}内的有效逻辑探测统计
                  {result.show_cache_rate
                    ? '；缓存率按近 24 小时实际请求的有效缓存样本统计'
                    : null}
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
