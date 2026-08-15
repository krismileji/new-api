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
  PauseIcon,
  PlayIcon,
  Refresh01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { memo } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader } from '@/components/ui/card'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatChannelMonitorCost } from '../lib/format'
import {
  areChannelStatusProbeCardPropsEqual,
  formatChannelStatusProbeNextRun,
  type ChannelStatusProbeCardRenderProps,
} from '../lib/status-probe-card-render'
import type {
  ChannelStatusProbeDisplayUnit,
  ChannelStatusProbeHealth,
  ChannelStatusProbeBucket,
  ChannelStatusProbeModelStatus,
  ChannelStatusProbeResult,
} from '../types'

const HEALTH_PRESENTATION: Record<
  ChannelStatusProbeHealth,
  { label: string; dot: string; badge: 'secondary' | 'warning' | 'destructive' }
> = {
  unconfigured: {
    label: '未配置',
    dot: 'bg-muted-foreground/45',
    badge: 'secondary',
  },
  paused: {
    label: '已暂停',
    dot: 'bg-muted-foreground/60',
    badge: 'secondary',
  },
  pending: { label: '待检测', dot: 'bg-primary', badge: 'secondary' },
  healthy: { label: '正常', dot: 'bg-success', badge: 'secondary' },
  partial: { label: '部分异常', dot: 'bg-warning', badge: 'warning' },
  unhealthy: { label: '异常', dot: 'bg-destructive', badge: 'destructive' },
  rate_limited: { label: '已限流', dot: 'bg-warning', badge: 'warning' },
  stale: { label: '已过期', dot: 'bg-warning', badge: 'warning' },
}

const RESULT_LABEL: Record<ChannelStatusProbeResult, string> = {
  success: '成功',
  upstream_failure: '上游失败',
  rate_limited: '限流',
  local_failure: '本地失败',
  skipped: '跳过',
  canceled: '取消',
}

const NANO_CNY_PER_CNY = 1_000_000_000

const BUCKET_COLOR: Record<'' | ChannelStatusProbeResult, string> = {
  '': 'bg-muted/60',
  success: 'bg-success',
  upstream_failure: 'bg-destructive',
  rate_limited: 'bg-warning',
  local_failure: 'bg-warning/70',
  skipped: 'bg-muted-foreground/45',
  canceled: 'bg-muted-foreground/70',
}

const DISPLAY_UNIT_LABEL: Record<ChannelStatusProbeDisplayUnit, string> = {
  minute: '分钟',
  hour: '小时',
  day: '天',
}

function formatDuration(value: number | null) {
  if (value == null) return '-'
  if (value >= 1000) return `${(value / 1000).toFixed(2)} s`
  return `${Math.round(value)} ms`
}

function formatTPS(value: number | null) {
  if (value == null) return '-'
  return value.toFixed(value >= 100 ? 0 : 1)
}

function displayRangeLabel(value: number, unit: ChannelStatusProbeDisplayUnit) {
  return `近 ${value} ${DISPLAY_UNIT_LABEL[unit]}`
}

function bucketTimeLabel(
  bucket: ChannelStatusProbeBucket,
  unit: ChannelStatusProbeDisplayUnit
) {
  const timestamp = formatTimestampToDate(bucket.started_at)
  if (unit === 'day') return timestamp.slice(0, 10)
  if (unit === 'hour') return timestamp.slice(5, 13)
  return timestamp.slice(11, 16)
}

function bucketTooltip(
  bucket: ChannelStatusProbeBucket,
  unit: ChannelStatusProbeDisplayUnit
) {
  const time = bucketTimeLabel(bucket, unit)
  const models = bucket.models?.join('、') || '无'
  if (!bucket.result) return `${time} · 无探测`
  return `${time} · 成功 ${bucket.success} · 失败 ${bucket.upstream_failure + bucket.local_failure} · 限流 ${bucket.rate_limited} · 跳过 ${bucket.skipped + bucket.canceled} · 模型 ${models}`
}

const ChannelStatusProbeModelStatuses = memo(
  function ChannelStatusProbeModelStatuses(props: {
    channelName: string
    modelStatuses: ChannelStatusProbeModelStatus[]
    displayValue: number
    displayUnit: ChannelStatusProbeDisplayUnit
    displayRangeLabel: string
  }) {
    return (
      <section
        className='flex min-h-0 w-full flex-1 flex-col gap-1.5'
        aria-label={`${props.channelName} 各模型${props.displayRangeLabel}状态`}
      >
        <div className='text-muted-foreground flex items-center justify-between gap-2 text-[11px]'>
          <span>{props.displayRangeLabel}状态</span>
          <span className='tabular-nums'>
            {props.modelStatuses.length} 个模型
          </span>
        </div>
        <div className='no-scrollbar min-h-10 flex-1 overflow-y-auto pr-1'>
          {props.modelStatuses.length === 0 ? (
            <div className='text-muted-foreground flex h-full min-h-14 items-center justify-center text-xs'>
              尚未配置探测模型
            </div>
          ) : (
            <div className='flex flex-col gap-2.5'>
              {props.modelStatuses.map((modelStatus) => {
                const modelPresentation =
                  HEALTH_PRESENTATION[modelStatus.health_status]
                const latest = modelStatus.latest
                return (
                  <div
                    key={modelStatus.model_name}
                    className='flex min-w-0 flex-col gap-1'
                    data-slot='status-probe-model'
                  >
                    <div className='flex min-w-0 items-center justify-between gap-2 text-[11px]'>
                      <span
                        className='flex min-w-0 items-center gap-1.5'
                        title={modelStatus.model_name}
                      >
                        <span
                          className={cn(
                            'size-1.5 shrink-0 rounded-full',
                            modelPresentation.dot
                          )}
                          aria-hidden='true'
                        />
                        <span className='truncate font-medium'>
                          {modelStatus.model_name}
                        </span>
                      </span>
                      <span
                        className='text-muted-foreground shrink-0 tabular-nums'
                        title={
                          latest
                            ? formatTimestampToDate(latest.finished_at)
                            : '等待首次检测'
                        }
                      >
                        {latest
                          ? `${RESULT_LABEL[latest.result]} · ${formatTimestampToDate(latest.finished_at).slice(11, 19)}`
                          : modelPresentation.label}
                      </span>
                    </div>
                    <div
                      className='grid h-2.5 min-w-0 gap-px overflow-hidden rounded-sm'
                      style={{
                        gridTemplateColumns: `repeat(${modelStatus.recent_window.length}, minmax(0, 1fr))`,
                      }}
                      aria-label={`${props.channelName} ${modelStatus.model_name} ${props.displayRangeLabel}探测结果`}
                      data-window-buckets={modelStatus.recent_window.length}
                      data-status-window-value={props.displayValue}
                      data-status-window-unit={props.displayUnit}
                    >
                      {modelStatus.recent_window.map((bucket) => {
                        const tooltip = bucketTooltip(bucket, props.displayUnit)
                        return (
                          <span
                            key={bucket.started_at}
                            className={cn(
                              'h-2.5 min-w-0',
                              BUCKET_COLOR[bucket.result]
                            )}
                            title={tooltip}
                            aria-label={tooltip}
                            data-slot='status-probe-bucket'
                          />
                        )
                      })}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </section>
    )
  }
)

export const ChannelStatusProbeCard = memo(function ChannelStatusProbeCard(
  props: ChannelStatusProbeCardRenderProps
) {
  const presentation = HEALTH_PRESENTATION[props.channel.health_status]
  const config = props.channel.config
  const latest = props.channel.latest
  const latestProbeCostCNY =
    latest?.settled_cost_nano_cny == null
      ? null
      : latest.settled_cost_nano_cny / NANO_CNY_PER_CNY
  const displayValue = config?.display_value ?? 60
  const displayUnit = config?.display_unit ?? 'minute'
  const displayRangeLabelValue = displayRangeLabel(displayValue, displayUnit)
  const probePending =
    props.channel.running || Boolean(config?.manual_request_id)
  const disabled = props.actionPending || probePending
  let channelStatusLabel = ''
  if (props.channel.channel_status === 2) {
    channelStatusLabel = '手动禁用'
  } else if (props.channel.channel_status === 3) {
    channelStatusLabel = '自动禁用'
  }

  return (
    <Card
      size='sm'
      className='hover:ring-foreground/20 h-[28rem] gap-0 rounded-lg py-0 transition-colors [contain-intrinsic-size:448px] [content-visibility:auto]'
      data-testid='channel-status-probe-card'
    >
      <CardHeader className='grid-cols-[minmax(0,1fr)_auto] gap-3 border-b py-3'>
        <button
          type='button'
          className='focus-visible:ring-ring/50 min-w-0 text-left outline-none focus-visible:ring-2'
          onClick={() => props.onOpenHistory(props.channel.id)}
          aria-label={`查看 ${props.channel.name} 的状态探测记录`}
        >
          <div className='flex min-w-0 items-center gap-2'>
            <span
              className={cn('size-2 shrink-0 rounded-full', presentation.dot)}
              aria-hidden='true'
            />
            <span className='truncate font-medium' title={props.channel.name}>
              {props.channel.name}
            </span>
            <span className='text-muted-foreground shrink-0 tabular-nums'>
              #{props.channel.id}
            </span>
          </div>
          <div className='mt-1 flex min-w-0 items-center gap-2'>
            <Badge variant={presentation.badge} className='px-1.5'>
              {presentation.label}
            </Badge>
            {props.channel.running && <Badge variant='outline'>检测中</Badge>}
            {!props.channel.running && config?.manual_request_id && (
              <Badge variant='outline'>排队中</Badge>
            )}
            {channelStatusLabel && (
              <span className='text-muted-foreground truncate text-xs'>
                {channelStatusLabel}
              </span>
            )}
          </div>
        </button>

        <div className='flex items-center gap-0.5'>
          {config && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    disabled={props.actionPending}
                    onClick={() => props.onToggleEnabled(props.channel)}
                    aria-label={
                      config.enabled ? '暂停周期探测' : '恢复周期探测'
                    }
                  />
                }
              >
                <HugeiconsIcon icon={config.enabled ? PauseIcon : PlayIcon} />
              </TooltipTrigger>
              <TooltipContent>
                {config.enabled ? '暂停周期探测' : '恢复周期探测'}
              </TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={disabled || !config}
                  onClick={() => props.onRun(props.channel)}
                  aria-label='立即检测'
                />
              }
            >
              <HugeiconsIcon
                icon={Refresh01Icon}
                className={cn(probePending && 'animate-spin')}
              />
            </TooltipTrigger>
            <TooltipContent>立即检测</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={props.actionPending}
                  onClick={() => props.onOpenConfig(props.channel.id)}
                  aria-label='配置状态探测'
                />
              }
            >
              <HugeiconsIcon icon={Settings02Icon} />
            </TooltipTrigger>
            <TooltipContent>配置状态探测</TooltipContent>
          </Tooltip>
        </div>
      </CardHeader>

      <CardContent className='flex min-h-0 flex-1 overflow-hidden px-0 py-0'>
        <button
          type='button'
          className='focus-visible:ring-ring/50 flex min-h-0 w-full flex-col gap-3 overflow-hidden px-3 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset'
          onClick={() => props.onOpenHistory(props.channel.id)}
          aria-label={`打开 ${props.channel.name} 状态探测记录`}
        >
          <div
            className='text-muted-foreground w-full truncate text-xs'
            title={props.channel.remark || '暂无备注'}
          >
            备注：{props.channel.remark || '-'}
          </div>

          <dl className='grid w-full grid-cols-2 gap-x-4 gap-y-2'>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>最近首字</dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatDuration(latest?.first_token_ms ?? null)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>最近 TPS</dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatTPS(latest?.tps ?? null)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>最近探测成本</dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatChannelMonitorCost(latestProbeCostCNY)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>今日探测成本</dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatChannelMonitorCost(props.channel.today_probe_cost_cny)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>
                {displayRangeLabelValue}平均首字
              </dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatDuration(props.channel.avg_first_token_ms)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-xs'>
                {displayRangeLabelValue}平均 TPS
              </dt>
              <dd className='mt-0.5 truncate font-mono text-base font-semibold tabular-nums'>
                {formatTPS(props.channel.avg_tps)}
              </dd>
            </div>
          </dl>

          <div className='text-muted-foreground flex min-w-0 items-center gap-1 text-xs'>
            <span className='shrink-0'>
              成本倍率{' '}
              <span className='text-foreground font-mono tabular-nums'>
                {props.channel.cost_ratio == null
                  ? '-'
                  : props.channel.cost_ratio.toFixed(4)}
              </span>
            </span>
            <span aria-hidden='true'>·</span>
            <span className='shrink-0'>
              模型 {props.channel.configured_model_count} 个
            </span>
            {config && (
              <>
                <span aria-hidden='true'>·</span>
                <span className='truncate'>
                  每 {config.interval_seconds} 秒
                </span>
              </>
            )}
          </div>

          <ChannelStatusProbeModelStatuses
            channelName={props.channel.name}
            modelStatuses={props.channel.model_statuses}
            displayValue={displayValue}
            displayUnit={displayUnit}
            displayRangeLabel={displayRangeLabelValue}
          />
        </button>
      </CardContent>
      <CardFooter className='min-h-11 justify-between gap-2 px-3 py-2 text-[11px]'>
        <Badge
          variant={config?.record_sample ? 'secondary' : 'outline'}
          aria-label={
            config?.record_sample ? '计入智能调度样本' : '不计入智能调度样本'
          }
        >
          {config?.record_sample ? '计入样本' : '不计入样本'}
        </Badge>
        <span className='text-muted-foreground shrink-0 tabular-nums'>
          下次{' '}
          {formatChannelStatusProbeNextRun(
            config?.next_run_at ?? 0,
            props.serverNow
          )}
        </span>
      </CardFooter>
    </Card>
  )
}, areChannelStatusProbeCardPropsEqual)
