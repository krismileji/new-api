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
import type { TFunction } from 'i18next'
import { memo, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

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
  channelModelDetectionResultLabel,
  channelModelDetectionResultTone,
  formatChannelModelDetectionRelativeTime,
  isKnownChannelModelDetectionOutcome,
} from '../lib/model-detection'
import { formatChannelMonitorStatusWindowRange } from '../lib/status-window'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionDetectorState,
  ChannelModelDetectionDisplayUnit,
  ChannelModelDetectionHealth,
  ChannelModelDetectionPreset,
  ChannelModelDetectionRunStatus,
  ChannelModelDetectionResultBucket,
  ChannelModelDetectionTargetSummary,
} from '../types-model-detection'
import {
  ChannelMonitorStatusWindow,
  ChannelMonitorStatusWindowDetails,
  type ChannelMonitorStatusWindowPresentation,
} from './channel-monitor-status-window'

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>['variant']>

type ModelDetectionCostSummary = {
  value: string
  detail: string | null
}

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
  displayValue: number
  displayUnit: ChannelModelDetectionDisplayUnit
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

const RESULT_BUCKET_COLOR = {
  running: 'bg-primary',
  success: 'bg-success',
  attention: 'bg-warning',
  unhealthy: 'bg-destructive',
  failed: 'bg-warning/70',
  inactive: 'bg-muted-foreground/70',
} as const

const DISPLAY_UNIT_LABEL: Record<ChannelModelDetectionDisplayUnit, string> = {
  minute: '分钟',
  hour: '小时',
  day: '天',
}

function modelDetectionCostSummary(
  cost: ChannelModelDetectionChannel['latest_run_cost'],
  fallbackSettledCostCNY: number | null,
  t: TFunction
): ModelDetectionCostSummary {
  let settledValue: string | null = null
  if (cost?.settled_request_count && cost.settled_cost_cny != null) {
    settledValue = formatChannelMonitorCost(Number(cost.settled_cost_cny))
  } else if (
    cost == null &&
    fallbackSettledCostCNY != null &&
    fallbackSettledCostCNY > 0
  ) {
    settledValue = formatChannelMonitorCost(fallbackSettledCostCNY)
  }

  let unresolvedValue: string | null = null
  if (cost?.unresolved_request_count) {
    unresolvedValue = t('Pending verification')
  }

  if (settledValue) {
    return { value: settledValue, detail: unresolvedValue }
  }
  if (unresolvedValue) {
    return {
      value: unresolvedValue,
      detail: null,
    }
  }
  if (cost == null && fallbackSettledCostCNY != null) {
    return {
      value: formatChannelMonitorCost(fallbackSettledCostCNY),
      detail: null,
    }
  }
  return { value: '-', detail: null }
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
  const latest = target.latest
  if (!latest) {
    return {
      label: '等待首次检测',
      icon: Clock01Icon,
      tone: 'text-muted-foreground',
    }
  }
  const outcome = latest.outcome_code
  const label = channelModelDetectionResultLabel({
    status: latest.status,
    outcomeCode: outcome,
    errorCode: latest.error_code,
    title: latest.title_cn,
  })
  const resultTone = channelModelDetectionResultTone({
    claimedModel: target.claimed_model,
    status: latest.status,
    outcomeCode: outcome,
    errorCode: latest.error_code,
    fingerprintModel: latest.fingerprint_model,
    fingerprintClaimMismatch: latest.fingerprint_claim_mismatch,
  })
  if (resultTone === 'running') {
    return { label, icon: Clock01Icon, tone: 'text-primary' }
  }
  if (resultTone === 'failed') {
    return { label, icon: Cancel01Icon, tone: 'text-warning' }
  }
  if (resultTone === 'inactive') {
    return { label, icon: Cancel01Icon, tone: 'text-muted-foreground' }
  }
  if (!outcome) {
    return { label, icon: Alert02Icon, tone: 'text-warning' }
  }
  if (!isKnownChannelModelDetectionOutcome(outcome)) {
    return {
      label: '检测器返回了新结论，请升级主系统适配',
      icon: Alert02Icon,
      tone: 'text-warning',
    }
  }
  if (resultTone === 'unhealthy') {
    return { label, icon: Alert02Icon, tone: 'text-destructive' }
  }
  if (resultTone === 'attention') {
    return { label, icon: Alert02Icon, tone: 'text-warning' }
  }
  return { label, icon: CheckmarkCircle02Icon, tone: 'text-success' }
}

function modelDetectionBucketPresentation(
  bucket: ChannelModelDetectionResultBucket,
  displayUnit: ChannelModelDetectionDisplayUnit,
  automaticDetectionEnabled: boolean
): ChannelMonitorStatusWindowPresentation & {
  status: string
  statusVariant: BadgeVariant
  description?: string
} {
  const timeRange = formatChannelMonitorStatusWindowRange(
    bucket.started_at,
    displayUnit
  )
  if (!bucket.result) {
    if (automaticDetectionEnabled) {
      return {
        ariaLabel: `${timeRange} · 定时检测已开启但未执行`,
        className: 'bg-muted-foreground/35',
        state: 'not-executed',
        status: '未执行',
        statusVariant: 'secondary',
        description: '定时检测已开启，但本时间格内没有检测任务。',
      }
    }
    return {
      ariaLabel: `${timeRange} · 定时检测未开启`,
      className: 'bg-muted/60',
      state: 'not-scheduled',
      status: '未安排',
      statusVariant: 'outline',
      description: '该渠道当前未参加统一定时检测。',
    }
  }
  const status = {
    success: '正常',
    attention: '需关注',
    unhealthy: '异常',
    failed: '执行失败',
    running: '进行中',
    inactive: '跳过',
  }[bucket.result]
  let statusVariant: BadgeVariant = 'outline'
  if (bucket.result === 'success' || bucket.result === 'running') {
    statusVariant = 'secondary'
  } else if (bucket.result === 'unhealthy') {
    statusVariant = 'destructive'
  } else if (bucket.result === 'attention' || bucket.result === 'failed') {
    statusVariant = 'warning'
  }
  return {
    ariaLabel: `${timeRange} · 检测 ${bucket.detection_count} · 正常 ${bucket.success} · 关注 ${bucket.attention} · 异常 ${bucket.unhealthy} · 执行失败 ${bucket.failed} · 进行中 ${bucket.running} · 跳过 ${bucket.inactive}`,
    className: RESULT_BUCKET_COLOR[bucket.result],
    state: 'executed',
    status,
    statusVariant,
  }
}

function ModelDetectionBucketDetails(props: {
  bucket: ChannelModelDetectionResultBucket
  displayUnit: ChannelModelDetectionDisplayUnit
  automaticDetectionEnabled: boolean
}) {
  const presentation = modelDetectionBucketPresentation(
    props.bucket,
    props.displayUnit,
    props.automaticDetectionEnabled
  )
  const details = props.bucket.result
    ? [
        { label: '检测总数', value: props.bucket.detection_count },
        { label: '正常', value: props.bucket.success },
        { label: '需关注', value: props.bucket.attention },
        { label: '异常', value: props.bucket.unhealthy },
        { label: '执行失败', value: props.bucket.failed },
        { label: '进行中', value: props.bucket.running },
        { label: '跳过', value: props.bucket.inactive },
      ]
    : undefined
  return (
    <ChannelMonitorStatusWindowDetails
      timeRange={formatChannelMonitorStatusWindowRange(
        props.bucket.started_at,
        props.displayUnit
      )}
      status={presentation.status}
      statusVariant={presentation.statusVariant}
      description={presentation.description}
      details={details}
    />
  )
}

function ModelDetectionTarget(props: {
  target: ChannelModelDetectionTargetSummary
  serverNow: number
  displayValue: number
  displayUnit: ChannelModelDetectionDisplayUnit
  automaticDetectionEnabled: boolean
}) {
  const presentation = outcomePresentation(props.target)
  const latest = props.target.latest
  const costLines = channelModelDetectionCostLines(latest?.cost ?? null)
  const progress = latest?.progress
  const progressValue = progress?.planned
    ? Math.min(100, (progress.logical_completed / progress.planned) * 100)
    : 0
  const recentWindow = props.target.recent_window
  const displayRange = `近 ${props.displayValue} ${DISPLAY_UNIT_LABEL[props.displayUnit]}`

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

      <div className='flex min-w-0 flex-col gap-1'>
        <div className='text-muted-foreground flex items-center justify-between gap-2 text-[11px]'>
          <span>{displayRange}检测</span>
          <span className='tabular-nums'>{recentWindow.length} 个时间格</span>
        </div>
        <ChannelMonitorStatusWindow
          buckets={recentWindow}
          bucketSlot='model-detection-bucket'
          bucketStateDataAttribute='data-model-detection-bucket-state'
          gridProps={{
            'aria-label': `${props.target.request_model} ${displayRange}模型检测结果`,
            'data-window-buckets': recentWindow.length,
            'data-model-detection-window-value': props.displayValue,
            'data-model-detection-window-unit': props.displayUnit,
          }}
          getBucketPresentation={(bucket) =>
            modelDetectionBucketPresentation(
              bucket,
              props.displayUnit,
              props.automaticDetectionEnabled
            )
          }
          renderDetails={(bucket) => (
            <ModelDetectionBucketDetails
              bucket={bucket}
              displayUnit={props.displayUnit}
              automaticDetectionEnabled={props.automaticDetectionEnabled}
            />
          )}
        />
      </div>

      {progress && (
        <div className='flex flex-col gap-1'>
          <div className='text-muted-foreground flex items-center justify-between gap-2 text-[11px] tabular-nums'>
            <span>
              {latest.status === 'running' || latest.status === 'completed'
                ? '检测进度'
                : '任务进度'}
            </span>
            <span>
              {progress.logical_completed} / {progress.planned}
            </span>
          </div>
          <Progress
            value={progressValue}
            className='[&_[data-slot=progress-indicator]]:duration-500 [&_[data-slot=progress-track]]:h-2.5 [&_[data-slot=progress-track]]:rounded-sm'
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
    const { t } = useTranslation()
    const presentation = HEALTH_PRESENTATION[props.channel.health_status]
    const activeRun = props.channel.active_run
    const config = props.channel.config
    const latestRunCost = props.channel.latest_run_cost
    const latestCostSummary = modelDetectionCostSummary(latestRunCost, null, t)
    const todayCostSummary = modelDetectionCostSummary(
      props.channel.today_model_detection_cost ?? null,
      props.channel.today_model_detection_cost_cny ?? 0,
      t
    )
    const detectorBlocked =
      props.detectorState === 'offline' ||
      props.detectorState === 'incompatible' ||
      props.detectorState === 'unconfigured'
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
        className='hover:ring-foreground/20 h-[28rem] min-w-0 gap-0 rounded-lg py-0 transition-colors [contain-intrinsic-size:448px] [content-visibility:auto]'
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
                {latestCostSummary.value}
              </dd>
              {latestCostSummary.detail && (
                <div className='text-muted-foreground truncate text-[10px] tabular-nums'>
                  {latestCostSummary.detail}
                </div>
              )}
            </div>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-[11px]'>
                今日模型检测成本
              </dt>
              <dd className='mt-0.5 truncate font-mono text-sm font-semibold tabular-nums'>
                {todayCostSummary.value}
              </dd>
              {todayCostSummary.detail && (
                <div className='text-muted-foreground truncate text-[10px] tabular-nums'>
                  {todayCostSummary.detail}
                </div>
              )}
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
            <div
              role='group'
              className='flex min-h-0 w-full min-w-0 cursor-pointer flex-col gap-3 px-3 py-3 text-left'
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
                      displayValue={props.displayValue}
                      displayUnit={props.displayUnit}
                      automaticDetectionEnabled={Boolean(
                        props.scheduleEnabled &&
                        config.schedule_enabled &&
                        target.enabled
                      )}
                    />
                  ))}
                </div>
              </div>
            </div>
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
