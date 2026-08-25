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
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ArrowUp01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState, type ComponentProps } from 'react'

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
  channelModelDetectionResultLabel,
  channelModelDetectionResultTone,
} from '../lib/model-detection'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionKnownOutcomeCode,
  ChannelModelDetectionRunDetail,
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
  onLoadRunDetail?: (runId: string) => Promise<ChannelModelDetectionRunDetail>
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

function errorSummary(message: string) {
  return message
    .replaceAll(/Bearer\s+\S+/gi, 'Bearer ***')
    .replaceAll(
      /(authorization|api[-_ ]?key|token|secret)\s*[:=]\s*\S+/gi,
      '$1=***'
    )
    .slice(0, 240)
}

function statusDotClass(status: ChannelModelDetectionRunStatus) {
  if (status === 'completed') return 'bg-success'
  if (status === 'failed') return 'bg-destructive'
  if (status === 'partial' || status === 'canceling') return 'bg-warning'
  if (status === 'canceled') return 'bg-muted-foreground/60'
  return 'bg-primary'
}

function resultBadgeVariant(
  tone: ReturnType<typeof channelModelDetectionResultTone>
): BadgeVariant {
  if (tone === 'unhealthy' || tone === 'failed') return 'destructive'
  if (tone === 'attention') return 'warning'
  if (tone === 'inactive') return 'outline'
  return 'secondary'
}

function verdictLabel(value: string | undefined, kind: 'juice' | 'fingerprint') {
  const normalized = value?.trim().toLowerCase()
  if (kind === 'juice') {
    if (normalized === 'pass') return '通过'
    if (normalized === 'mismatch') return '不匹配'
    if (normalized === 'insufficient') return '证据不足'
    return '未返回'
  }
  if (normalized?.includes('strong')) return '明确'
  if (normalized === 'unclear') return '不明确'
  return '未返回'
}

function executionOverallVerdict(
  execution: ChannelModelDetectionRunDetail['executions'][number],
  resultLabel: string
) {
  const report = execution.report
  if (typeof report === 'object' && report !== null && !Array.isArray(report)) {
    const verdict = (report as Record<string, unknown>).overall_verdict
    if (typeof verdict === 'string' && verdict.trim()) return verdict.trim()
  }
  return execution.title_cn?.trim() || resultLabel
}

function RunResultList(props: {
  detail: ChannelModelDetectionRunDetail
}) {
  if (props.detail.executions.length === 0) {
    return (
      <div className='text-muted-foreground rounded-md border border-dashed px-2.5 py-2 text-xs'>
        当前轮次还没有目标结果
      </div>
    )
  }

  return (
    <div
      className='flex min-w-0 flex-col gap-1.5'
      data-slot='model-detection-history-results'
    >
      {props.detail.executions.map((execution) => {
        const tone = channelModelDetectionResultTone({
          claimedModel: execution.claimed_model,
          status: execution.status,
          outcomeCode: execution.outcome_code,
          errorCode: execution.error_code || execution.final_error_code,
          fingerprintModel: execution.fingerprint_model,
          fingerprintClaimMismatch: execution.fingerprint_claim_mismatch,
        })
        const errorMessage =
          execution.error_message || execution.error_code || execution.final_error_code
        const fingerprintModel = execution.fingerprint_model?.trim()
        const resultLabel = channelModelDetectionResultLabel({
          status: execution.status,
          outcomeCode: execution.outcome_code,
          errorCode: execution.error_code || execution.final_error_code,
          title: execution.title_cn,
        })
        const overallVerdict = executionOverallVerdict(execution, resultLabel)

        return (
          <div
            key={execution.target_key}
            className='bg-muted/25 min-w-0 rounded-md border px-2.5 py-2'
          >
            <div className='flex min-w-0 items-start justify-between gap-2'>
              <div className='min-w-0'>
                <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
                  <span className='truncate text-xs font-medium'>
                    {execution.claimed_model}
                  </span>
                  <Badge variant={resultBadgeVariant(tone)}>
                    {resultLabel}
                  </Badge>
                </div>
                <div className='text-muted-foreground mt-1 truncate text-[11px]'>
                  请求模型 {execution.request_model}
                </div>
              </div>
              <span className='text-muted-foreground shrink-0 text-[11px] tabular-nums'>
                {execution.progress.logical_completed} / {execution.progress.planned}
              </span>
            </div>
            <div className='text-muted-foreground mt-1.5 flex min-w-0 flex-wrap gap-x-2 gap-y-0.5 text-[11px]'>
              <span className='font-medium text-foreground'>最终结果：{overallVerdict}</span>
              <span aria-hidden='true'>·</span>
              <span>Juice {verdictLabel(execution.juice_verdict_state, 'juice')}</span>
              <span aria-hidden='true'>·</span>
              <span>
                指纹 {verdictLabel(execution.fingerprint_verdict_state, 'fingerprint')}
              </span>
              {fingerprintModel ? (
                <>
                  <span aria-hidden='true'>·</span>
                  <span className='truncate'>识别 {fingerprintModel}</span>
                </>
              ) : null}
            </div>
            {execution.subtitle_cn || errorMessage ? (
              <div
                className={`${errorMessage ? 'text-destructive' : 'text-muted-foreground'} mt-1 truncate text-[11px]`}
                title={errorMessage || execution.subtitle_cn}
              >
                {errorMessage || execution.subtitle_cn}
              </div>
            ) : null}
          </div>
        )
      })}
    </div>
  )
}

function CostSummary(props: { cost: ChannelModelDetectionCost }) {
  const cost = props.cost
  let settledCost = '尚无已结算渠道成本'
  if (cost.settled_request_count > 0) {
    settledCost =
      cost.settled_cost_cny == null
        ? '已结算渠道成本金额不可用'
        : `已结算渠道成本 ¥${cost.settled_cost_cny}`
  }

  const unresolvedCost =
    cost.unresolved_request_count > 0 || cost.unresolved_cost_unknown_count > 0
      ? '等待可核验 Usage，不计入已结算成本'
      : '无待核实请求'

  return (
    <details className='border-t pt-1.5 text-[11px]' aria-label='检测成本'>
      <summary className='text-muted-foreground cursor-pointer select-none'>
        成本明细 · 等价已结算额度 {formatQuota(cost.settled_quota)} ·{' '}
        {settledCost}
      </summary>
      <dl className='mt-2 grid min-w-0 grid-cols-1 gap-2 text-xs sm:grid-cols-3'>
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
            待核实请求数 {cost.unresolved_request_count}
          </dd>
        </div>
      </dl>
    </details>
  )
}

function RunItem(props: {
  run: ChannelModelDetectionRunSummary
  onOpenRun?: (run: ChannelModelDetectionRunSummary) => void
  onLoadRunDetail?: (runId: string) => Promise<ChannelModelDetectionRunDetail>
}) {
  const run = props.run
  const [resultsOpen, setResultsOpen] = useState(false)
  const [detail, setDetail] = useState<ChannelModelDetectionRunDetail | null>(null)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [loadingDetail, setLoadingDetail] = useState(false)
  const status = RUN_STATUS[run.status]
  const progressValue = run.progress.planned
    ? Math.min(
        100,
        (run.progress.logical_completed / run.progress.planned) * 100
      )
    : 0
  const safeError = errorSummary(run.error_message)
  let settledCost = '暂未结算'
  if (run.cost.status === 'not_started') {
    settledCost = '尚未请求'
  } else if (run.cost.status === 'pending') {
    settledCost = '结算中'
  } else if (run.cost.settled_request_count > 0) {
    settledCost =
      run.cost.settled_cost_cny == null
        ? '金额待确认'
        : `¥${run.cost.settled_cost_cny}`
  }
  const unresolvedCount = Math.max(
    run.cost.unresolved_request_count,
    run.cost.unresolved_cost_unknown_count
  )
  const latestTimestamp =
    run.finished_at || run.updated_at || run.started_at || run.queued_at

  const toggleResults = () => {
    if (resultsOpen) {
      setResultsOpen(false)
      return
    }
    if (!props.onLoadRunDetail) {
      props.onOpenRun?.(run)
      return
    }
    setResultsOpen(true)
    if (detail || loadingDetail) return
    setLoadingDetail(true)
    setDetailError(null)
    void props
      .onLoadRunDetail(run.run_id)
      .then((nextDetail) => setDetail(nextDetail))
      .catch((error: unknown) => {
        setDetailError(channelModelDetectionRequestErrorMessage(error))
      })
      .finally(() => setLoadingDetail(false))
  }

  let resultContent = null
  if (loadingDetail) {
    resultContent = (
      <div
        className='text-muted-foreground rounded-md border border-dashed px-2.5 py-2 text-xs'
        aria-label='正在加载检测结果'
      >
        正在加载检测结果…
      </div>
    )
  } else if (detailError) {
    resultContent = (
      <div className='text-destructive rounded-md border border-dashed px-2.5 py-2 text-xs'>
        结果加载失败：{detailError}
      </div>
    )
  } else if (detail) {
    resultContent = <RunResultList detail={detail} />
  }

  return (
    <article
      className='min-w-0 rounded-lg border px-3 py-2.5 sm:p-3'
      data-slot='model-detection-history-run'
      data-run-id={run.run_id}
    >
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <span
              aria-hidden='true'
              className={`size-1.5 shrink-0 rounded-full ${statusDotClass(run.status)}`}
            />
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
        <div className='flex shrink-0 items-center gap-1'>
          {(props.onLoadRunDetail || props.onOpenRun) && (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-7 gap-1 px-2 text-xs'
              onClick={toggleResults}
              aria-expanded={resultsOpen}
              aria-label={`查看轮次 ${run.run_id} 检测结果`}
              title='查看检测结果'
            >
              <HugeiconsIcon
                icon={resultsOpen ? ArrowUp01Icon : ArrowDown01Icon}
              />
              <span>{resultsOpen ? '收起结果' : '查看结果'}</span>
            </Button>
          )}
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
      </div>

      <div className='mt-2 min-w-0'>
        <div className='text-muted-foreground mb-1 flex min-w-0 flex-wrap justify-between gap-x-3 gap-y-1 text-[11px] tabular-nums'>
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

      <dl
        className='bg-muted/35 mt-2 grid min-w-0 grid-cols-3 gap-2 rounded-md px-2.5 py-1.5 text-xs'
        data-slot='model-detection-history-metrics'
      >
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[10px]'>成功</dt>
          <dd className='mt-0.5 font-medium tabular-nums'>
            {run.progress.successful}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[10px]'>异常</dt>
          <dd className='mt-0.5 font-medium tabular-nums'>
            {run.progress.errors}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[10px]'>实际结算</dt>
          <dd className='mt-0.5 truncate font-medium tabular-nums'>
            {settledCost}
          </dd>
        </div>
      </dl>

      <div className='text-muted-foreground mt-2 flex min-w-0 flex-wrap gap-x-2 gap-y-0.5 text-[11px] tabular-nums'>
        <span>更新 {formatTimestampToDate(latestTimestamp)}</span>
        <span aria-hidden='true'>·</span>
        <span>HTTP {run.progress.http_attempts}</span>
        {run.progress.retries > 0 && (
          <>
            <span aria-hidden='true'>·</span>
            <span>重试 {run.progress.retries}</span>
          </>
        )}
        {unresolvedCount > 0 && (
          <>
            <span aria-hidden='true'>·</span>
            <span>待核实 {unresolvedCount}</span>
          </>
        )}
      </div>

      {(run.error_code || safeError) && (
        <div
          className='text-destructive mt-1.5 min-w-0 truncate text-[11px]'
          title={safeError}
        >
          {run.error_code && <span>错误代码 {run.error_code}</span>}
          {run.error_code && safeError && <span> · </span>}
          {safeError && <span>{safeError}</span>}
        </div>
      )}

      {resultsOpen ? (
        <div className='mt-2 border-t pt-2'>{resultContent}</div>
      ) : null}

      <div className='mt-2'>
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
      className='flex min-w-0 flex-col gap-2 p-3 sm:p-4'
      aria-label='正在加载检测历史'
    >
      {[0, 1, 2].map((index) => (
        <div key={index} className='rounded-lg border p-3'>
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
          <RunItem
            key={run.run_id}
            run={run}
            onOpenRun={props.onOpenRun}
            onLoadRunDetail={props.onLoadRunDetail}
          />
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
