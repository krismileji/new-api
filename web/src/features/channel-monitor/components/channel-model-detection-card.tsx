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
  Cancel01Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  FingerPrintScanIcon,
  PauseIcon,
  PlayIcon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { memo, type ComponentProps } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import { formatChannelMonitorCost } from '../lib/format'
import {
  channelModelDetectionClaimedModelLabel,
  channelModelDetectionCostLines,
  channelModelDetectionPresetLabel,
  channelModelDetectionPresetSourceLabel,
  formatChannelModelDetectionRelativeTime,
  isKnownChannelModelDetectionOutcome,
} from '../lib/model-detection'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionDetectorState,
  ChannelModelDetectionHealth,
  ChannelModelDetectionPreset,
  ChannelModelDetectionRunStatus,
  ChannelModelDetectionTargetSummary,
} from '../types-model-detection'

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>['variant']>

const HEALTH_PRESENTATION: Record<
  ChannelModelDetectionHealth,
  { label: string; dot: string; badge: BadgeVariant }
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
  running: { label: '检测中', dot: 'bg-primary', badge: 'secondary' },
  healthy: { label: '正常', dot: 'bg-success', badge: 'secondary' },
  attention: { label: '需关注', dot: 'bg-warning', badge: 'warning' },
  unhealthy: {
    label: '检测到异常证据',
    dot: 'bg-destructive',
    badge: 'destructive',
  },
  detector_unavailable: {
    label: '检测器离线',
    dot: 'bg-muted-foreground/60',
    badge: 'outline',
  },
  stale: { label: '结果已过期', dot: 'bg-warning', badge: 'warning' },
}

export type ChannelModelDetectionCardProps = {
  channel: ChannelModelDetectionChannel
  detectorState: ChannelModelDetectionDetectorState
  scheduledPreset: ChannelModelDetectionPreset
  scheduleEnabled: boolean
  nextBatchAt: number
  serverNow: number
  actionPending?: boolean
  onOpenHistory: (channel: ChannelModelDetectionChannel) => void
  onOpenConfig: (channel: ChannelModelDetectionChannel) => void
  onOpenManualRun: (channel: ChannelModelDetectionChannel) => void
  onCancelRun: (channel: ChannelModelDetectionChannel) => void
  onToggleSchedule: (channel: ChannelModelDetectionChannel) => void
}

const ACTIVE_RUN_LABEL: Partial<
  Record<ChannelModelDetectionRunStatus, string>
> = {
  queued: '排队中',
  waiting_detector: '等待检测器',
  submitting: '提交中',
  submission_unknown: '启动待确认',
  running: '检测中',
  canceling: '取消中',
}

function ModelDetectionActiveRunProgress(props: {
  channelName: string
  activeRun: NonNullable<ChannelModelDetectionChannel['active_run']>
}) {
  const planned = Math.max(0, props.activeRun.progress.planned)
  const completed = Math.max(0, props.activeRun.progress.logical_completed)
  const progressValue = planned ? Math.min(100, (completed / planned) * 100) : 0
  const progressPercent = Math.round(progressValue)
  const statusLabel = ACTIVE_RUN_LABEL[props.activeRun.status] ?? '任务处理中'

  return (
    <section
      className='bg-muted/25 border-b px-3 py-2'
      data-slot='model-detection-run-progress'
    >
      <Progress
        value={progressValue}
        className='gap-x-2 gap-y-1.5 [&_[data-slot=progress-indicator]]:duration-500 [&_[data-slot=progress-track]]:h-2.5 [&_[data-slot=progress-track]]:rounded-sm'
        aria-label={`${props.channelName} 当前轮次进度 ${completed} / ${planned}（${progressPercent}%）`}
      >
        <ProgressLabel className='min-w-0 truncate text-[11px]'>
          当前轮次 · {statusLabel}
        </ProgressLabel>
        <ProgressValue className='shrink-0 text-[11px]'>
          {() => `${completed} / ${planned} · ${progressPercent}%`}
        </ProgressValue>
      </Progress>
    </section>
  )
}

function outcomePresentation(target: ChannelModelDetectionTargetSummary) {
  const outcome = target.latest?.outcome_code ?? ''
  if (!outcome) {
    return {
      label: '等待首次检测',
      icon: Clock01Icon,
      tone: 'text-muted-foreground',
    }
  }
  if (!isKnownChannelModelDetectionOutcome(outcome)) {
    return {
      label: '检测器返回了新结论，请升级主系统适配',
      icon: Alert02Icon,
      tone: 'text-warning',
    }
  }
  const label = target.latest?.title_cn || outcome
  if (
    outcome === 'juice_mismatch_fingerprint_strong' ||
    outcome === 'juice_mismatch_fingerprint_unclear' ||
    outcome === 'possible_non_gpt'
  ) {
    return { label, icon: Alert02Icon, tone: 'text-destructive' }
  }
  if (
    outcome === 'juice_insufficient_fingerprint_strong' ||
    outcome === 'juice_insufficient_fingerprint_unclear'
  ) {
    return { label, icon: Alert02Icon, tone: 'text-warning' }
  }
  return { label, icon: CheckmarkCircle02Icon, tone: 'text-success' }
}

function ModelDetectionTarget(props: {
  target: ChannelModelDetectionTargetSummary
  serverNow: number
}) {
  const presentation = outcomePresentation(props.target)
  const latest = props.target.latest
  const costLines = channelModelDetectionCostLines(latest?.cost ?? null)
  const progress = latest?.progress
  const progressValue = progress?.planned
    ? Math.min(100, (progress.logical_completed / progress.planned) * 100)
    : 0

  return (
    <article
      className='flex min-w-0 flex-col gap-1.5 border-b pb-3 last:border-b-0 last:pb-0'
      data-slot='model-detection-target'
    >
      <div className='flex min-w-0 items-center justify-between gap-2'>
        <span
          className='truncate text-sm font-medium'
          title={props.target.request_model}
        >
          {props.target.request_model}
        </span>
        <Badge variant='outline' className='max-w-[45%]'>
          申报{' '}
          {channelModelDetectionClaimedModelLabel(props.target.claimed_model)}
        </Badge>
      </div>

      <div
        className={cn(
          'flex min-w-0 items-start gap-1.5 text-xs',
          presentation.tone
        )}
      >
        <HugeiconsIcon
          icon={presentation.icon}
          className='mt-0.5 size-3.5 shrink-0'
          aria-hidden='true'
        />
        <span className='line-clamp-2 text-pretty'>{presentation.label}</span>
      </div>

      {progress && latest?.status !== 'completed' && (
        <div className='flex flex-col gap-1'>
          <div className='text-muted-foreground flex items-center justify-between gap-2 text-[11px] tabular-nums'>
            <span>{latest.status === 'running' ? '检测进度' : '任务进度'}</span>
            <span>
              {progress.logical_completed} / {progress.planned}
            </span>
          </div>
          <Progress
            value={progressValue}
            aria-label={`${props.target.request_model} 检测进度 ${progress.logical_completed} / ${progress.planned}`}
          />
        </div>
      )}

      <div className='text-muted-foreground flex min-w-0 flex-wrap gap-x-1.5 text-[11px]'>
        {latest ? (
          <>
            <span>
              {channelModelDetectionPresetSourceLabel(latest.preset_source)} ·{' '}
              {channelModelDetectionPresetLabel(latest.preset)}
            </span>
            <span aria-hidden='true'>·</span>
            <span className='tabular-nums'>
              {latest.progress.logical_completed}/{latest.progress.planned}
            </span>
            <span aria-hidden='true'>·</span>
            <span>
              {formatChannelModelDetectionRelativeTime(
                latest.finished_at || latest.updated_at,
                props.serverNow
              )}
            </span>
          </>
        ) : (
          <span>尚无检测报告</span>
        )}
      </div>

      <div className='text-muted-foreground flex min-w-0 flex-col gap-0.5 text-[11px] tabular-nums'>
        {costLines.slice(0, 2).map((line) => (
          <span key={line} className='truncate' title={line}>
            {line}
          </span>
        ))}
      </div>
    </article>
  )
}

function IconAction(props: {
  label: string
  icon: ComponentProps<typeof HugeiconsIcon>['icon']
  disabled?: boolean
  destructive?: boolean
  spinning?: boolean
  onClick: () => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant={props.destructive ? 'destructive' : 'ghost'}
            size='icon-sm'
            disabled={props.disabled}
            onClick={(event) => {
              event.stopPropagation()
              props.onClick()
            }}
            aria-label={props.label}
          />
        }
      >
        <HugeiconsIcon
          icon={props.icon}
          className={cn(props.spinning && 'animate-spin')}
        />
      </TooltipTrigger>
      <TooltipContent>{props.label}</TooltipContent>
    </Tooltip>
  )
}

export const ChannelModelDetectionCard = memo(
  function ChannelModelDetectionCard(props: ChannelModelDetectionCardProps) {
    const presentation = HEALTH_PRESENTATION[props.channel.health_status]
    const activeRun = props.channel.active_run
    const config = props.channel.config
    const latestRunCost = props.channel.latest_run_cost
    const latestModelDetectionCostCNY =
      latestRunCost?.settled_request_count &&
      latestRunCost.settled_cost_cny != null
        ? Number(latestRunCost.settled_cost_cny)
        : null
    const detectorBlocked =
      props.detectorState === 'offline' ||
      props.detectorState === 'incompatible' ||
      props.detectorState === 'unconfigured' ||
      props.detectorState === 'unknown'
    let runLabel = '选择档位并立即检测'
    if (detectorBlocked) {
      runLabel = '检测器当前不可用'
    } else if (!config) {
      runLabel = '请先配置检测目标'
    }
    let activityBadge: string | null = null
    if (activeRun && activeRun.status !== 'running') {
      activityBadge = ACTIVE_RUN_LABEL[activeRun.status] ?? '任务处理中'
    }
    let scheduleLabel = '不参加统一定时'
    if (config?.schedule_enabled) {
      scheduleLabel = props.scheduleEnabled
        ? `参加定时 · ${channelModelDetectionPresetLabel(props.scheduledPreset)}`
        : '统一定时已关闭'
    }

    return (
      <Card
        size='sm'
        className='hover:ring-foreground/20 h-[25rem] min-w-0 gap-0 rounded-lg py-0 transition-colors [contain-intrinsic-size:400px] [content-visibility:auto]'
        data-testid='channel-model-detection-card'
        data-health={props.channel.health_status}
      >
        <CardHeader className='grid-cols-[minmax(0,1fr)_auto] gap-2 border-b py-3'>
          <button
            type='button'
            className='focus-visible:ring-ring/50 min-w-0 text-left outline-none focus-visible:ring-2'
            onClick={() => props.onOpenHistory(props.channel)}
            aria-label={`查看 ${props.channel.name} 的模型检测记录`}
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
            <div className='mt-1 flex min-w-0 flex-wrap items-center gap-1.5'>
              <Badge variant={presentation.badge} className='max-w-full'>
                {presentation.label}
              </Badge>
              {activityBadge && (
                <Badge variant='outline'>{activityBadge}</Badge>
              )}
              {detectorBlocked &&
                props.channel.health_status !== 'detector_unavailable' && (
                  <Badge variant='outline'>检测器不可用</Badge>
                )}
            </div>
          </button>

          <div className='flex items-center gap-0.5'>
            {config && (
              <IconAction
                label={
                  config.schedule_enabled
                    ? '退出统一定时检测'
                    : '参加统一定时检测'
                }
                icon={config.schedule_enabled ? PauseIcon : PlayIcon}
                disabled={props.actionPending}
                onClick={() => props.onToggleSchedule(props.channel)}
              />
            )}
            {activeRun ? (
              <IconAction
                label='取消当前模型检测'
                icon={Cancel01Icon}
                disabled={
                  props.actionPending || activeRun.status === 'canceling'
                }
                destructive
                onClick={() => props.onCancelRun(props.channel)}
              />
            ) : (
              <IconAction
                label={runLabel}
                icon={FingerPrintScanIcon}
                disabled={props.actionPending || detectorBlocked || !config}
                onClick={() => props.onOpenManualRun(props.channel)}
              />
            )}
            <IconAction
              label='配置模型检测目标'
              icon={Settings02Icon}
              disabled={props.actionPending}
              onClick={() => props.onOpenConfig(props.channel)}
            />
          </div>
        </CardHeader>

        {activeRun ? (
          <ModelDetectionActiveRunProgress
            channelName={props.channel.name}
            activeRun={activeRun}
          />
        ) : null}

        <CardContent className='flex min-h-0 flex-1 flex-col px-0 py-0'>
          <dl className='grid w-full grid-cols-2 gap-x-4 border-b px-3 py-2'>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-[11px]'>
                最近模型检测成本
              </dt>
              <dd className='mt-0.5 truncate font-mono text-sm font-semibold tabular-nums'>
                {formatChannelMonitorCost(latestModelDetectionCostCNY)}
              </dd>
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-[11px]'>
                今日模型检测成本
              </dt>
              <dd className='mt-0.5 truncate font-mono text-sm font-semibold tabular-nums'>
                {formatChannelMonitorCost(
                  props.channel.today_model_detection_cost_cny
                )}
              </dd>
            </div>
          </dl>
          {!config || props.channel.targets.length === 0 ? (
            <div className='flex min-h-0 w-full min-w-0 flex-col gap-3 px-3 py-3'>
              <div
                className='text-muted-foreground w-full truncate text-xs'
                title={props.channel.remark || '暂无备注'}
              >
                备注：{props.channel.remark || '-'}
              </div>
              <Empty className='min-h-0 flex-1 px-3 py-4'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <HugeiconsIcon
                      icon={FingerPrintScanIcon}
                      aria-hidden='true'
                    />
                  </EmptyMedia>
                  <EmptyTitle>尚未配置检测目标</EmptyTitle>
                  <EmptyDescription>
                    配置请求模型和申报型号后才能开始检测
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={(event) => {
                      event.stopPropagation()
                      props.onOpenConfig(props.channel)
                    }}
                  >
                    <HugeiconsIcon
                      icon={Settings02Icon}
                      data-icon='inline-start'
                    />
                    配置检测目标
                  </Button>
                </EmptyContent>
              </Empty>
            </div>
          ) : (
            <button
              type='button'
              className='focus-visible:ring-ring/50 flex min-h-0 w-full min-w-0 flex-col gap-3 px-3 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset'
              onClick={() => props.onOpenHistory(props.channel)}
              aria-label={`打开 ${props.channel.name} 模型检测记录`}
            >
              <div
                className='text-muted-foreground w-full truncate text-xs'
                title={props.channel.remark || '暂无备注'}
              >
                备注：{props.channel.remark || '-'}
              </div>
              <div className='no-scrollbar min-h-0 w-full flex-1 overflow-y-auto pr-1'>
                <div className='flex min-w-0 flex-col gap-3'>
                  {props.channel.targets.map((target) => (
                    <ModelDetectionTarget
                      key={target.target_key}
                      target={target}
                      serverNow={props.serverNow}
                    />
                  ))}
                </div>
              </div>
            </button>
          )}
        </CardContent>

        <CardFooter className='min-h-11 justify-between gap-2 px-3 py-2 text-[11px]'>
          <span className='min-w-0 truncate'>{scheduleLabel}</span>
          <span className='text-muted-foreground shrink-0 tabular-nums'>
            {config?.schedule_enabled && props.scheduleEnabled
              ? `下批 ${formatChannelModelDetectionRelativeTime(props.nextBatchAt, props.serverNow)}`
              : `${props.channel.targets.length} 个目标`}
          </span>
        </CardFooter>
      </Card>
    )
  }
)
