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
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ComponentProps } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTimestampToDate } from '@/lib/format'

import {
  channelModelDetectionPresetLabel,
  channelModelDetectionPresetSourceLabel,
} from '../lib/model-detection'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionKnownOutcomeCode,
  ChannelModelDetectionRunHistoryPage,
  ChannelModelDetectionRunSummary,
  ChannelModelDetectionRunStatus,
  ChannelModelDetectionTrigger,
} from '../types-model-detection'

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>['variant']>

export type {
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionRunHistoryPage,
  ChannelModelDetectionRunSummary,
} from '../types-model-detection'

export type ChannelModelDetectionHistorySheetProps = {
  channel: ChannelModelDetectionChannel
  open: boolean
  query: ChannelModelDetectionHistoryQuery
  data?: ChannelModelDetectionRunHistoryPage
  loading?: boolean
  refreshing?: boolean
  error?: string | null
  onOpenChange: (open: boolean) => void
  onQueryChange: (query: ChannelModelDetectionHistoryQuery) => void
  onRefresh?: () => void
  onOpenRun?: (run: ChannelModelDetectionRunSummary) => void
}

const TRIGGER_OPTIONS: ReadonlyArray<{
  value: ChannelModelDetectionTrigger | null
  label: string
}> = [
  { value: null, label: '全部触发方式' },
  { value: 'manual', label: '手动' },
  { value: 'scheduled', label: '定时' },
]

const STATUS_OPTIONS: ReadonlyArray<{
  value: ChannelModelDetectionRunStatus | null
  label: string
}> = [
  { value: null, label: '全部状态' },
  { value: 'queued', label: '排队中' },
  { value: 'waiting_detector', label: '等待检测器' },
  { value: 'submitting', label: '提交中' },
  { value: 'submission_unknown', label: '启动待确认' },
  { value: 'running', label: '检测中' },
  { value: 'canceling', label: '取消中' },
  { value: 'completed', label: '已完成' },
  { value: 'partial', label: '部分完成' },
  { value: 'failed', label: '失败' },
  { value: 'external_session_conflict', label: '外部会话冲突' },
  { value: 'canceled', label: '已取消' },
]

const OUTCOME_OPTIONS: ReadonlyArray<{
  value: ChannelModelDetectionKnownOutcomeCode | null
  label: string
}> = [
  { value: null, label: '全部结论' },
  {
    value: 'juice_pass_fingerprint_strong',
    label: 'Juice 通过，指纹明确',
  },
  {
    value: 'juice_pass_fingerprint_unclear',
    label: 'Juice 通过，指纹不明确',
  },
  {
    value: 'juice_mismatch_fingerprint_strong',
    label: 'Juice 不符，指纹明确',
  },
  {
    value: 'juice_mismatch_fingerprint_unclear',
    label: 'Juice 不符，指纹不明确',
  },
  {
    value: 'juice_insufficient_fingerprint_strong',
    label: 'Juice 证据不足，指纹明确',
  },
  {
    value: 'juice_insufficient_fingerprint_unclear',
    label: 'Juice 证据不足，指纹不明确',
  },
  { value: 'possible_non_gpt', label: '可能不是 GPT' },
]

const PAGE_SIZE_OPTIONS = [
  { value: 10, label: '每页 10 条' },
  { value: 20, label: '每页 20 条' },
  { value: 50, label: '每页 50 条' },
  { value: 100, label: '每页 100 条' },
] as const

const RUN_STATUS: Record<
  ChannelModelDetectionRunStatus,
  { label: string; variant: BadgeVariant }
> = {
  queued: { label: '排队中', variant: 'outline' },
  waiting_detector: { label: '等待检测器', variant: 'outline' },
  submitting: { label: '提交中', variant: 'secondary' },
  submission_unknown: { label: '启动待确认', variant: 'warning' },
  running: { label: '检测中', variant: 'secondary' },
  canceling: { label: '取消中', variant: 'warning' },
  completed: { label: '已完成', variant: 'secondary' },
  partial: { label: '部分完成', variant: 'warning' },
  failed: { label: '失败', variant: 'destructive' },
  external_session_conflict: {
    label: '外部会话冲突',
    variant: 'warning',
  },
  canceled: { label: '已取消', variant: 'outline' },
}

function formatQuota(value: number) {
  return value.toLocaleString('zh-CN')
}

function triggerLabel(trigger: ChannelModelDetectionTrigger) {
  return trigger === 'manual' ? '手动' : '定时'
}

function createdByLabel(run: ChannelModelDetectionRunSummary) {
  if (run.created_by_username) return run.created_by_username
  if (run.created_by_user_id > 0) return `管理员 #${run.created_by_user_id}`
  return run.trigger === 'scheduled' ? '系统调度' : '未知管理员'
}

function errorSummary(message: string) {
  return message
    .replaceAll(/Bearer\s+\S+/gi, 'Bearer ***')
    .replaceAll(
      /(authorization|api[-_ ]?key|token|secret)\s*[:=]\s*\S+/gi,
      '$1=***'
    )
    .slice(0, 240)
}

function CostSummary(props: { cost: ChannelModelDetectionCost }) {
  const cost = props.cost
  const estimateCost =
    cost.estimated_cost_cny == null
      ? '预计成本暂无法估算'
      : `预计成本 ¥${cost.estimated_cost_cny}`
  const estimateQuota =
    cost.estimated_quota == null
      ? '预计额度暂无法估算'
      : `预计额度 ${formatQuota(cost.estimated_quota)}`

  let settledCost = '尚无已结算渠道成本'
  if (cost.settled_request_count > 0) {
    settledCost =
      cost.settled_cost_cny == null
        ? '已结算渠道成本暂无法估算'
        : `已结算渠道成本 ¥${cost.settled_cost_cny}`
  }

  let unresolvedCost = '无待核实请求'
  if (
    cost.unresolved_request_count > 0 ||
    cost.unresolved_cost_unknown_count > 0
  ) {
    unresolvedCost =
      cost.unresolved_cost_cny == null
        ? '待核实预计成本暂无法估算'
        : `待核实预计成本 ¥${cost.unresolved_cost_cny}`
  }

  return (
    <section className='border-t pt-3' aria-label='检测成本'>
      {cost.status === 'not_started' && (
        <div className='text-muted-foreground mb-2 text-xs'>
          尚未发出上游请求
        </div>
      )}
      {cost.status === 'pending' && (
        <div className='text-muted-foreground mb-2 text-xs'>成本结算中</div>
      )}
      <dl className='grid min-w-0 grid-cols-1 gap-x-4 gap-y-3 text-xs sm:grid-cols-2'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>运行前估算</dt>
          <dd className='mt-0.5 font-medium'>{estimateCost}</dd>
          <dd className='text-muted-foreground mt-0.5 tabular-nums'>
            {estimateQuota} · 无法估算请求数 {cost.cost_estimate_unknown_count}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>等价计费额度</dt>
          <dd className='mt-0.5 font-medium tabular-nums'>
            等价已结算额度 {formatQuota(cost.settled_quota)}
          </dd>
          <dd className='text-muted-foreground mt-0.5 tabular-nums'>
            计价基数 {formatQuota(cost.cost_basis_quota)}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>实际结算</dt>
          <dd className='mt-0.5 font-medium'>{settledCost}</dd>
          <dd className='text-muted-foreground mt-0.5 tabular-nums'>
            已结算请求数 {cost.settled_request_count}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>待核实</dt>
          <dd className='mt-0.5 font-medium'>{unresolvedCost}</dd>
          <dd className='text-muted-foreground mt-0.5 tabular-nums'>
            待核实请求数 {cost.unresolved_request_count} · 无法估算请求数{' '}
            {cost.unresolved_cost_unknown_count}
          </dd>
        </div>
      </dl>
    </section>
  )
}

function RunItem(props: {
  run: ChannelModelDetectionRunSummary
  onOpenRun?: (run: ChannelModelDetectionRunSummary) => void
}) {
  const run = props.run
  const status = RUN_STATUS[run.status]
  const progressValue = run.progress.planned
    ? Math.min(
        100,
        (run.progress.logical_completed / run.progress.planned) * 100
      )
    : 0
  const safeError = errorSummary(run.error_message)

  return (
    <article
      className='min-w-0 rounded-lg border p-3 sm:p-4'
      data-slot='model-detection-history-run'
      data-run-id={run.run_id}
    >
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <span className='font-medium'>
              {triggerLabel(run.trigger)} ·{' '}
              {channelModelDetectionPresetLabel(run.preset)}
            </span>
            <Badge variant={status.variant}>{status.label}</Badge>
          </div>
          <div className='text-muted-foreground mt-1 min-w-0 truncate text-xs'>
            轮次 {run.run_id} · 档位来源{' '}
            {channelModelDetectionPresetSourceLabel(run.preset_source)}
          </div>
        </div>
        {props.onOpenRun && (
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            className='shrink-0'
            onClick={() => props.onOpenRun?.(run)}
            aria-label={`查看轮次 ${run.run_id} 详情`}
            title='查看轮次详情'
          >
            <HugeiconsIcon icon={ArrowRight01Icon} />
          </Button>
        )}
      </div>

      <div className='mt-3 min-w-0'>
        <div className='text-muted-foreground mb-1 flex min-w-0 flex-wrap justify-between gap-x-3 gap-y-1 text-xs tabular-nums'>
          <span>
            逻辑完成 {run.progress.logical_completed} / {run.progress.planned}
          </span>
          <span>
            目标 {run.completed_target_count} / {run.target_count}
          </span>
        </div>
        <Progress
          value={progressValue}
          aria-label={`轮次 ${run.run_id} 逻辑进度 ${run.progress.logical_completed} / ${run.progress.planned}`}
        />
      </div>

      <dl className='mt-3 grid min-w-0 grid-cols-1 gap-x-4 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-4'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>排队时间</dt>
          <dd className='mt-0.5 tabular-nums'>
            {formatTimestampToDate(run.queued_at)}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>开始时间</dt>
          <dd className='mt-0.5 tabular-nums'>
            {formatTimestampToDate(run.started_at)}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>完成时间</dt>
          <dd className='mt-0.5 tabular-nums'>
            {formatTimestampToDate(run.finished_at)}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground'>创建管理员</dt>
          <dd className='mt-0.5 truncate' title={createdByLabel(run)}>
            {createdByLabel(run)}
          </dd>
        </div>
      </dl>

      {(run.error_code || safeError) && (
        <div className='text-destructive mt-3 min-w-0 text-xs break-words'>
          {run.error_code && <span>错误代码 {run.error_code}</span>}
          {run.error_code && safeError && <span> · </span>}
          {safeError && <span>{safeError}</span>}
        </div>
      )}

      <div className='mt-3'>
        <CostSummary cost={run.cost} />
      </div>
    </article>
  )
}

function HistoryFilters(props: {
  query: ChannelModelDetectionHistoryQuery
  onQueryChange: (query: ChannelModelDetectionHistoryQuery) => void
}) {
  const updateFilter = (patch: Partial<ChannelModelDetectionHistoryQuery>) => {
    props.onQueryChange({ ...props.query, ...patch, page: 1 })
  }

  return (
    <div
      className='grid min-w-0 shrink-0 grid-cols-1 gap-2 border-y p-4 sm:grid-cols-2 lg:grid-cols-4'
      data-slot='model-detection-history-filters'
    >
      <Select
        items={TRIGGER_OPTIONS}
        value={props.query.trigger || null}
        onValueChange={(value) => updateFilter({ trigger: value ?? '' })}
      >
        <SelectTrigger
          className='w-full min-w-0'
          aria-label='按触发方式筛选检测轮次'
          data-api-field='trigger'
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {TRIGGER_OPTIONS.map((option) => (
              <SelectItem key={option.value ?? 'all'} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>

      <Select
        items={STATUS_OPTIONS}
        value={props.query.status || null}
        onValueChange={(value) => updateFilter({ status: value ?? '' })}
      >
        <SelectTrigger
          className='w-full min-w-0'
          aria-label='按状态筛选检测轮次'
          data-api-field='status'
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {STATUS_OPTIONS.map((option) => (
              <SelectItem key={option.value ?? 'all'} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>

      <Input
        name='model'
        value={props.query.model}
        onChange={(event) => updateFilter({ model: event.target.value })}
        placeholder='输入完整请求模型'
        aria-label='按请求模型筛选检测轮次'
        data-api-field='model'
      />

      <Select
        items={OUTCOME_OPTIONS}
        value={props.query.outcome || null}
        onValueChange={(value) => updateFilter({ outcome: value ?? '' })}
      >
        <SelectTrigger
          className='w-full min-w-0'
          aria-label='按结论筛选检测轮次'
          data-api-field='outcome'
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {OUTCOME_OPTIONS.map((option) => (
              <SelectItem key={option.value ?? 'all'} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </div>
  )
}

function HistoryLoading() {
  return (
    <div
      className='flex min-w-0 flex-col gap-3 p-4'
      aria-label='正在加载检测历史'
    >
      {[0, 1, 2].map((index) => (
        <div key={index} className='rounded-lg border p-4'>
          <Skeleton className='h-5 w-40 max-w-full' />
          <Skeleton className='mt-3 h-1 w-full' />
          <Skeleton className='mt-4 h-20 w-full' />
        </div>
      ))}
    </div>
  )
}

export function ChannelModelDetectionHistorySheet(
  props: ChannelModelDetectionHistorySheetProps
) {
  const total = props.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / props.query.page_size))
  const hasItems = Boolean(props.data?.items.length)

  let historyContent
  if (props.loading && !props.data) {
    historyContent = <HistoryLoading />
  } else if (props.error && !props.data) {
    historyContent = (
      <Empty className='min-h-72 rounded-none'>
        <EmptyHeader>
          <EmptyTitle>检测历史加载失败</EmptyTitle>
          <EmptyDescription>{props.error}</EmptyDescription>
        </EmptyHeader>
        {props.onRefresh && (
          <Button type='button' variant='outline' onClick={props.onRefresh}>
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重试
          </Button>
        )}
      </Empty>
    )
  } else if (!hasItems) {
    historyContent = (
      <Empty className='min-h-72 rounded-none'>
        <EmptyHeader>
          <EmptyTitle>暂无检测记录</EmptyTitle>
          <EmptyDescription>当前筛选条件下没有模型检测轮次</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    historyContent = (
      <div className='flex min-w-0 flex-col gap-3 p-3 sm:p-4'>
        {props.data?.items.map((run) => (
          <RunItem key={run.run_id} run={run} onOpenRun={props.onOpenRun} />
        ))}
      </div>
    )
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full max-w-full min-w-0 overflow-x-hidden sm:max-w-3xl'>
        <SheetHeader className='min-w-0 pr-12'>
          <div className='flex min-w-0 items-start justify-between gap-3'>
            <div className='min-w-0'>
              <SheetTitle>模型检测历史</SheetTitle>
              <SheetDescription className='truncate'>
                {props.channel.name} · ID {props.channel.id}
              </SheetDescription>
            </div>
            {props.onRefresh && (
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                className='shrink-0'
                disabled={props.refreshing}
                onClick={props.onRefresh}
                aria-label='刷新模型检测历史'
                title='刷新'
              >
                <HugeiconsIcon
                  icon={Refresh01Icon}
                  className={props.refreshing ? 'animate-spin' : undefined}
                />
              </Button>
            )}
          </div>
        </SheetHeader>

        <HistoryFilters
          query={props.query}
          onQueryChange={props.onQueryChange}
        />

        {props.error && props.data && (
          <div
            className='text-destructive border-b px-4 py-2 text-xs'
            role='status'
          >
            刷新失败：{props.error}
          </div>
        )}

        <div className='min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto'>
          {historyContent}
        </div>

        <div className='flex min-w-0 shrink-0 flex-col gap-2 border-t px-4 py-3 sm:flex-row sm:items-center sm:justify-between'>
          <span className='text-muted-foreground min-w-0 text-xs tabular-nums'>
            共 {total} 条 · 第 {props.query.page}/{totalPages} 页
          </span>
          <div className='flex min-w-0 items-center justify-between gap-2 sm:justify-end'>
            <Select
              items={PAGE_SIZE_OPTIONS}
              value={props.query.page_size}
              onValueChange={(value) =>
                props.onQueryChange({
                  ...props.query,
                  page: 1,
                  page_size: value ?? props.query.page_size,
                })
              }
            >
              <SelectTrigger
                size='sm'
                className='min-w-0'
                aria-label='每页显示数量'
                data-api-field='page_size'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {PAGE_SIZE_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <div className='flex shrink-0 items-center gap-1'>
              <Button
                type='button'
                variant='outline'
                size='icon-sm'
                disabled={props.query.page <= 1 || props.refreshing}
                onClick={() =>
                  props.onQueryChange({
                    ...props.query,
                    page: Math.max(1, props.query.page - 1),
                  })
                }
                aria-label='上一页'
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='icon-sm'
                disabled={
                  props.query.page >= totalPages || props.refreshing || !total
                }
                onClick={() =>
                  props.onQueryChange({
                    ...props.query,
                    page: props.query.page + 1,
                  })
                }
                aria-label='下一页'
              >
                <HugeiconsIcon icon={ArrowRight01Icon} />
              </Button>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
