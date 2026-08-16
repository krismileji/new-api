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
  CheckmarkCircle02Icon,
  InformationCircleIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ComponentProps, ReactNode } from 'react'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

import {
  channelModelDetectionClaimedModelLabel,
  channelModelDetectionPresetLabel,
  channelModelDetectionPresetSourceLabel,
  isChannelModelDetectionStrongFingerprintConflict,
  isKnownChannelModelDetectionOutcome,
} from '../lib/model-detection'
import type {
  ChannelModelDetectionCost,
  ChannelModelDetectionExecutionDetail,
  ChannelModelDetectionExecutionStatus,
  ChannelModelDetectionKnownOutcomeCode,
} from '../types-model-detection'

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>['variant']>

export type { ChannelModelDetectionExecutionDetail } from '../types-model-detection'

export type ChannelModelDetectionReportProps = {
  executions: ChannelModelDetectionExecutionDetail[]
}

type OutcomeLevel = 'normal' | 'attention' | 'anomaly' | 'unknown'

type OutcomePresentation = {
  level: OutcomeLevel
  label: string
  title: string
  description: string
  variant: BadgeVariant
  icon: ComponentProps<typeof HugeiconsIcon>['icon']
}

type ModelMatch = {
  model: string
  label: string
  match: unknown
  score: number | null
  threshold: number | null
}

const OUTCOME_PRESENTATIONS: Record<
  ChannelModelDetectionKnownOutcomeCode,
  OutcomePresentation
> = {
  juice_pass_fingerprint_strong: {
    level: 'normal',
    label: '正常',
    title: 'Juice 通过，指纹明确',
    description: 'Juice 与申报型号一致，行为指纹也明确指向目标型号。',
    variant: 'secondary',
    icon: CheckmarkCircle02Icon,
  },
  juice_pass_fingerprint_unclear: {
    level: 'normal',
    label: '正常或信息不足',
    title: 'Juice 通过，指纹不明确',
    description:
      'Juice 与申报型号一致，但行为指纹证据不足；低档检测出现此结果属于预期。',
    variant: 'outline',
    icon: CheckmarkCircle02Icon,
  },
  juice_mismatch_fingerprint_strong: {
    level: 'anomaly',
    label: '模型异常',
    title: 'Juice 与申报型号不一致，指纹明确',
    description: 'Juice 证据与申报型号冲突，行为指纹还明确指向其他型号。',
    variant: 'destructive',
    icon: Alert02Icon,
  },
  juice_mismatch_fingerprint_unclear: {
    level: 'anomaly',
    label: '模型异常',
    title: 'Juice 与申报型号不一致，指纹不明确',
    description: 'Juice 证据与申报型号冲突，即使行为指纹尚不明确也属于硬异常。',
    variant: 'destructive',
    icon: Alert02Icon,
  },
  juice_insufficient_fingerprint_strong: {
    level: 'attention',
    label: '需关注',
    title: 'Juice 证据不足，指纹明确',
    description:
      'Juice 证据不足，但行为指纹已强烈指向某个型号，需要进一步复核。',
    variant: 'warning',
    icon: Alert02Icon,
  },
  juice_insufficient_fingerprint_unclear: {
    level: 'attention',
    label: '证据不足',
    title: 'Juice 与指纹证据均不足',
    description: '当前检测无法形成可靠型号判断，不应自动归类为模型异常。',
    variant: 'warning',
    icon: InformationCircleIcon,
  },
  possible_non_gpt: {
    level: 'anomaly',
    label: '模型异常',
    title: '可能不是 GPT',
    description: '现有证据显示上游可能不是 GPT 系列模型，需要立即复核渠道。',
    variant: 'destructive',
    icon: Alert02Icon,
  },
}

const FINGERPRINT_CONFLICT_PRESENTATION: OutcomePresentation = {
  level: 'anomaly',
  label: '模型异常',
  title: '行为指纹与申报型号冲突',
  description:
    '行为指纹与申报型号冲突：检测器原始结论标记为 Juice 通过，但指纹强烈指向其他型号；系统已按冲突证据归类为模型异常。',
  variant: 'destructive',
  icon: Alert02Icon,
}

const EXECUTION_STATUS: Record<
  ChannelModelDetectionExecutionStatus,
  { label: string; variant: BadgeVariant }
> = {
  pending: { label: '等待执行', variant: 'outline' },
  submitting: { label: '提交中', variant: 'secondary' },
  running: { label: '检测中', variant: 'secondary' },
  completed: { label: '已完成', variant: 'secondary' },
  failed: { label: '执行失败', variant: 'destructive' },
  canceled: { label: '已取消', variant: 'outline' },
  skipped: { label: '已跳过', variant: 'outline' },
}

const COST_STATUS: Record<
  ChannelModelDetectionCost['status'],
  { label: string; variant: BadgeVariant }
> = {
  pending: { label: '结算中', variant: 'outline' },
  not_started: { label: '未发出', variant: 'outline' },
  settled: { label: '已结算', variant: 'secondary' },
  unresolved: { label: '待核实', variant: 'warning' },
  partial: { label: '部分待核实', variant: 'warning' },
}

const MODEL_MATCH_ORDER = [
  { key: 'sol', label: 'Sol' },
  { key: 'terra', label: 'Terra' },
  { key: 'luna', label: 'Luna' },
] as const

const SENSITIVE_KEY_MARKERS = [
  'apikey',
  'accesstoken',
  'sessiontoken',
  'proxytoken',
  'tasktoken',
  'bearertoken',
  'authorization',
  'credential',
  'password',
  'secret',
  'taskbearer',
  'detectorurl',
  'detectoraddress',
] as const

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function recordValue(value: unknown) {
  return isRecord(value) ? value : null
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function numberValue(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value !== 'string' || value.trim() === '') return null
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

function arrayValue(value: unknown) {
  return Array.isArray(value) ? value : []
}

function normalizeSensitiveKey(key: string) {
  return key.toLowerCase().replaceAll(/[^a-z0-9]/g, '')
}

function isSensitiveReportKey(key: string) {
  const normalized = normalizeSensitiveKey(key)
  if (normalized === 'sessionid') return false
  if (
    normalized === 'key' ||
    normalized === 'token' ||
    normalized === 'authorization' ||
    normalized === 'secret' ||
    normalized === 'password'
  ) {
    return true
  }
  return SENSITIVE_KEY_MARKERS.some((marker) => normalized.includes(marker))
}

function redactSensitiveText(value: string) {
  return value
    .replaceAll(/Bearer\s+[^\s,;"']+/gi, 'Bearer [已隐藏]')
    .replaceAll(/\b(?:sk|pk|rk)-[a-z0-9_-]{8,}\b/gi, '[已隐藏]')
    .replaceAll(
      /((?:api[-_ ]?key|access[-_ ]?token|session[-_ ]?token|proxy[-_ ]?token|task[-_ ]?token|secret|password|authorization)\s*[:=]\s*)[^\s,;"']+/gi,
      '$1[已隐藏]'
    )
    .replaceAll(
      /((?:detector[-_ ]?(?:url|address))\s*[:=]\s*)https?:\/\/[^\s,;"']+/gi,
      '$1[已隐藏]'
    )
}

function redactReportSecrets(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactReportSecrets)
  if (!isRecord(value)) {
    return typeof value === 'string' ? redactSensitiveText(value) : value
  }

  const result: Record<string, unknown> = {}
  for (const [key, item] of Object.entries(value)) {
    result[key] = isSensitiveReportKey(key)
      ? '[已隐藏]'
      : redactReportSecrets(item)
  }
  return result
}

function safeJson(value: unknown) {
  try {
    return JSON.stringify(value, null, 2) ?? 'null'
  } catch {
    return '无法序列化技术 JSON'
  }
}

function compactValue(value: unknown) {
  if (value == null || value === '') return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return safeJson(value).replaceAll(/\s+/g, ' ')
}

function listItemKey(prefix: string, value: unknown, index: number) {
  return `${prefix}-${compactValue(value)}-${index}`
}

function reportRoot(value: unknown) {
  const root = recordValue(value)
  if (!root) return {}
  const nestedReport = recordValue(root.report)
  return nestedReport ?? root
}

function reportArray(report: Record<string, unknown>, key: string) {
  const direct = arrayValue(report[key])
  if (direct.length > 0) return direct
  const fingerprintDetails = recordValue(report.fingerprint_details)
  return arrayValue(fingerprintDetails?.[key])
}

function outcomePresentation(
  execution: ChannelModelDetectionExecutionDetail,
  fingerprintModel: string,
  fingerprintClaimMismatch: boolean
): OutcomePresentation {
  const outcome = execution.outcome_code
  if (!outcome) {
    return {
      level: 'unknown',
      label: '尚无结论',
      title: '尚未生成检测结论',
      description: '当前执行尚未产生可展示的模型检测结论。',
      variant: 'outline',
      icon: InformationCircleIcon,
    }
  }
  if (!isKnownChannelModelDetectionOutcome(outcome)) {
    return {
      level: 'unknown',
      label: '未知结论',
      title: '检测器返回了新的结论代码',
      description:
        '请升级主系统适配后再判断；当前结论不会自动归类为正常、需关注或模型异常。',
      variant: 'warning',
      icon: InformationCircleIcon,
    }
  }
  if (
    isChannelModelDetectionStrongFingerprintConflict({
      claimedModel: execution.claimed_model,
      outcomeCode: outcome,
      fingerprintModel,
      fingerprintClaimMismatch,
    })
  ) {
    return FINGERPRINT_CONFLICT_PRESENTATION
  }
  return OUTCOME_PRESENTATIONS[outcome]
}

function formatCount(value: number) {
  return value.toLocaleString('zh-CN')
}

function formatMetric(value: number | null) {
  if (value == null) return '未提供'
  if (value >= 0 && value <= 1) {
    return `${(value * 100).toFixed(1)}%`
  }
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 4 })
}

function modelMatchKey(value: string) {
  const normalized = value.toLowerCase()
  for (const model of MODEL_MATCH_ORDER) {
    if (normalized.includes(model.key)) return model.key
  }
  return ''
}

function modelMatches(report: Record<string, unknown>) {
  const matches = new Map<string, ModelMatch>()
  for (const value of reportArray(report, 'possible_models')) {
    const row = recordValue(value)
    if (!row) continue
    const model = stringValue(row.model)
    const label = stringValue(row.label_cn)
    const key = modelMatchKey(`${model} ${label}`)
    if (!key) continue
    const match = row.match
    const matchNumber = numberValue(match)
    const score = numberValue(row.score) ?? matchNumber
    matches.set(key, {
      model,
      label,
      match,
      score,
      threshold: numberValue(row.threshold),
    })
  }
  return matches
}

function matchResult(value: unknown) {
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (typeof value === 'string' && numberValue(value) == null) {
    return value || '未提供'
  }
  return '未提供'
}

function DetailItem(props: { label: string; children: ReactNode }) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd className='mt-1 min-w-0 text-sm break-words'>{props.children}</dd>
    </div>
  )
}

function ReportSection(props: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section className='min-w-0 py-4'>
      <div className='mb-3 min-w-0'>
        <h4 className='text-sm font-medium'>{props.title}</h4>
        {props.description ? (
          <p className='text-muted-foreground mt-1 text-xs break-words'>
            {props.description}
          </p>
        ) : null}
      </div>
      {props.children}
    </section>
  )
}

function OutcomeSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
  presentation: OutcomePresentation
}) {
  const execution = props.execution
  const presentation = props.presentation
  const title = execution.title_cn || presentation.title
  const description = execution.subtitle_cn || presentation.description
  const unknownCode =
    Boolean(execution.outcome_code) &&
    !isKnownChannelModelDetectionOutcome(execution.outcome_code)

  return (
    <div className='min-w-0 py-4'>
      <div className='flex min-w-0 items-start gap-3'>
        <HugeiconsIcon
          icon={presentation.icon}
          className={cn(
            'mt-0.5 size-5 shrink-0',
            presentation.level === 'anomaly' && 'text-destructive',
            presentation.level === 'attention' && 'text-warning',
            presentation.level === 'normal' && 'text-success',
            presentation.level === 'unknown' && 'text-muted-foreground'
          )}
          aria-hidden='true'
        />
        <div className='min-w-0 flex-1'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <Badge
              variant={presentation.variant}
              data-outcome-level={presentation.level}
            >
              {presentation.label}
            </Badge>
            <code className='text-muted-foreground min-w-0 text-xs break-all'>
              {execution.outcome_code || 'pending'}
            </code>
          </div>
          <h3 className='mt-2 text-base font-semibold break-words'>{title}</h3>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            {description}
          </p>
          {execution.subtitle_cn ? (
            <p className='text-muted-foreground mt-2 text-xs break-words'>
              系统归纳：{presentation.description}
            </p>
          ) : null}
        </div>
      </div>

      {unknownCode ? (
        <Alert className='mt-3'>
          <HugeiconsIcon icon={InformationCircleIcon} />
          <AlertTitle>需要升级主系统适配</AlertTitle>
          <AlertDescription className='break-words'>
            未知结论代码已原样保留，不会被自动解释为正常或异常。
          </AlertDescription>
        </Alert>
      ) : null}
    </div>
  )
}

function EvidenceSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
  report: Record<string, unknown>
}) {
  const fingerprintDetails = recordValue(props.report.fingerprint_details)
  const juiceState =
    props.execution.juice_verdict_state ||
    stringValue(props.report.juice_verdict_state)
  const fingerprintState =
    props.execution.fingerprint_verdict_state ||
    stringValue(props.report.fingerprint_verdict_state)
  const fingerprintModel =
    props.execution.fingerprint_model ||
    stringValue(props.report.fingerprint_model)
  const overallVerdict = stringValue(props.report.overall_verdict)
  const officialGrade = stringValue(props.report.official_grade)
  const trustScope = stringValue(props.report.trust_scope)
  const mismatch = props.report.fingerprint_claim_mismatch
  let mismatchLabel = '未提供'
  if (props.execution.fingerprint_claim_mismatch === true) {
    mismatchLabel = '是'
  } else if (typeof mismatch === 'boolean') {
    mismatchLabel = mismatch ? '是' : '否'
  }

  return (
    <ReportSection title='检测证据'>
      <dl className='grid min-w-0 grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2 lg:grid-cols-3'>
        <DetailItem label='Juice 状态'>{juiceState || '未提供'}</DetailItem>
        <DetailItem label='行为指纹状态'>
          {fingerprintState || '未提供'}
        </DetailItem>
        <DetailItem label='强指向型号'>
          {fingerprintModel
            ? channelModelDetectionClaimedModelLabel(fingerprintModel)
            : '未提供'}
        </DetailItem>
        <DetailItem label='总体判定'>{overallVerdict || '未提供'}</DetailItem>
        <DetailItem label='官方档级'>{officialGrade || '未提供'}</DetailItem>
        <DetailItem label='信任范围'>{trustScope || '未提供'}</DetailItem>
        <DetailItem label='指纹与申报冲突'>{mismatchLabel}</DetailItem>
        {fingerprintDetails ? (
          <DetailItem label='指纹详情摘要'>
            {stringValue(fingerprintDetails.summary) || '见下方匹配度'}
          </DetailItem>
        ) : null}
      </dl>
    </ReportSection>
  )
}

function ModelMatchSummary(props: { report: Record<string, unknown> }) {
  const matches = modelMatches(props.report)
  return (
    <ReportSection
      title='型号匹配度'
      description='匹配度与正式阈值均直接来自当前检测报告。'
    >
      <div
        className='min-w-0 border-y'
        role='table'
        aria-label='Sol、Terra、Luna 型号匹配度'
      >
        {MODEL_MATCH_ORDER.map((model) => {
          const match = matches.get(model.key)
          return (
            <div
              key={model.key}
              className='grid min-w-0 grid-cols-1 gap-1 border-b py-2.5 text-sm last:border-b-0 sm:grid-cols-[5rem_repeat(3,minmax(0,1fr))] sm:gap-3'
              role='row'
              data-model-match={model.label}
            >
              <span className='font-medium' role='cell'>
                {model.label}
              </span>
              <span className='min-w-0 break-words tabular-nums' role='cell'>
                匹配度 {formatMetric(match?.score ?? null)}
              </span>
              <span className='min-w-0 break-words tabular-nums' role='cell'>
                正式阈值 {formatMetric(match?.threshold ?? null)}
              </span>
              <span className='min-w-0 break-words' role='cell'>
                命中 {matchResult(match?.match)}
              </span>
            </div>
          )
        })}
      </div>
    </ReportSection>
  )
}

function ProgressAndUsage(props: {
  execution: ChannelModelDetectionExecutionDetail
}) {
  const execution = props.execution
  return (
    <ReportSection title='请求与 Usage'>
      <dl className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4'>
        <DetailItem label='逻辑请求'>
          {formatCount(execution.progress.logical_completed)} /{' '}
          {formatCount(execution.progress.planned)}
        </DetailItem>
        <DetailItem label='成功'>
          {formatCount(execution.progress.successful)}
        </DetailItem>
        <DetailItem label='错误'>
          {formatCount(execution.progress.errors)}
        </DetailItem>
        <DetailItem label='取消'>
          {formatCount(execution.progress.cancelled)}
        </DetailItem>
        <DetailItem label='HTTP 尝试'>
          {formatCount(execution.progress.http_attempts)}
        </DetailItem>
        <DetailItem label='重试'>
          {formatCount(execution.progress.retries)}
        </DetailItem>
        {execution.usage_available ? (
          <>
            <DetailItem label='输入 Token'>
              {formatCount(execution.input_tokens)}
            </DetailItem>
            <DetailItem label='输出 Token'>
              {formatCount(execution.output_tokens)}
            </DetailItem>
            <DetailItem label='总 Token'>
              {formatCount(execution.total_tokens)}
            </DetailItem>
          </>
        ) : (
          <div className='col-span-2 min-w-0 sm:col-span-4'>
            <span className='text-muted-foreground text-sm'>
              Usage 暂不可用
            </span>
          </div>
        )}
      </dl>
    </ReportSection>
  )
}

function moneyText(value: string | null) {
  if (value == null || value.trim() === '') return '暂无法估算'
  return `¥${value}`
}

function settledCostText(cost: ChannelModelDetectionCost) {
  if (cost.settled_request_count === 0) return '尚无已结算请求'
  return moneyText(cost.settled_cost_cny)
}

function unresolvedCostText(cost: ChannelModelDetectionCost) {
  if (
    cost.unresolved_request_count === 0 &&
    cost.unresolved_cost_unknown_count === 0
  ) {
    return '无待核实请求'
  }
  return moneyText(cost.unresolved_cost_cny)
}

function CostSummary(props: { cost: ChannelModelDetectionCost }) {
  const cost = props.cost
  const status = COST_STATUS[cost.status]
  const realRequests =
    cost.settled_request_count + cost.unresolved_request_count

  return (
    <ReportSection
      title='渠道成本详情'
      description='运行前估算、已结算成本和待核实预计成本分别展示，不合并为总实付。'
    >
      <div className='mb-3 flex min-w-0 flex-wrap items-center gap-2'>
        <Badge variant={status.variant}>{status.label}</Badge>
        <span className='text-muted-foreground text-xs'>
          仅统计渠道上游 API，不表示用户余额扣款
        </span>
      </div>

      {cost.status === 'not_started' ? (
        <Alert className='mb-3'>
          <HugeiconsIcon icon={InformationCircleIcon} />
          <AlertTitle>尚未发出上游请求</AlertTitle>
          <AlertDescription>
            当前目标没有跨过上游传输边界，因此没有产生渠道请求成本。
          </AlertDescription>
        </Alert>
      ) : null}

      <dl className='grid min-w-0 grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2 lg:grid-cols-4'>
        <DetailItem label='运行前预计渠道成本'>
          {moneyText(cost.estimated_cost_cny)}
        </DetailItem>
        <DetailItem label='运行前预计等价额度'>
          {cost.estimated_quota == null
            ? '暂无法估算'
            : formatCount(cost.estimated_quota)}
        </DetailItem>
        <DetailItem label='运行前估算未知数'>
          {formatCount(cost.cost_estimate_unknown_count)}
        </DetailItem>
        <DetailItem label='等价已结算额度'>
          {formatCount(cost.settled_quota)}
        </DetailItem>
        <DetailItem label='计价基数'>
          {formatCount(cost.cost_basis_quota)}
        </DetailItem>
        <DetailItem label='已结算渠道成本'>{settledCostText(cost)}</DetailItem>
        <DetailItem label='待核实预计成本'>
          {unresolvedCostText(cost)}
        </DetailItem>
        <DetailItem label='无法估算请求数'>
          {formatCount(cost.unresolved_cost_unknown_count)}
        </DetailItem>
        <DetailItem label='真实上游请求数'>
          {formatCount(realRequests)}
        </DetailItem>
        <DetailItem label='已结算请求数'>
          {formatCount(cost.settled_request_count)}
        </DetailItem>
        <DetailItem label='待核实请求数'>
          {formatCount(cost.unresolved_request_count)}
        </DetailItem>
      </dl>
    </ReportSection>
  )
}

function FailedItemSummary(props: { item: unknown; index: number }) {
  const item = recordValue(props.item)
  if (!item) {
    return (
      <li className='min-w-0 border-b py-2.5 text-sm break-words last:border-b-0'>
        {compactValue(props.item) || `失败项目 ${props.index + 1}`}
      </li>
    )
  }

  const reason =
    stringValue(item.reason_cn) ||
    stringValue(item.reason_code) ||
    `失败项目 ${props.index + 1}`
  const details = [
    { label: '证据', value: item.evidence },
    { label: '未完成探针格', value: item.incomplete_cells },
    {
      label: '缺少当前成功 effort',
      value: item.missing_current_success_efforts,
    },
    { label: '有效 effort 不足', value: item.insufficient_valid_efforts },
  ].filter((detail) => compactValue(detail.value) !== '')

  return (
    <li className='min-w-0 border-b py-2.5 last:border-b-0'>
      <div className='flex min-w-0 flex-wrap items-center gap-2'>
        <span className='text-sm font-medium break-words'>{reason}</span>
        {stringValue(item.layer) ? (
          <Badge variant='outline'>{stringValue(item.layer)}</Badge>
        ) : null}
        {stringValue(item.reason_code) ? (
          <code className='text-muted-foreground text-xs break-all'>
            {stringValue(item.reason_code)}
          </code>
        ) : null}
      </div>
      {details.map((detail) => (
        <p
          key={detail.label}
          className='text-muted-foreground mt-1 min-w-0 text-xs break-words'
        >
          {detail.label}：{compactValue(detail.value)}
        </p>
      ))}
    </li>
  )
}

function TextList(props: { title: string; values: unknown[] }) {
  if (props.values.length === 0) return null
  return (
    <div className='mt-3 min-w-0'>
      <h5 className='text-xs font-medium'>{props.title}</h5>
      <ul className='text-muted-foreground mt-1 flex min-w-0 list-disc flex-col gap-1 pl-5 text-xs'>
        {props.values.map((value, index) => (
          <li
            key={listItemKey(props.title, value, index)}
            className='min-w-0 break-words'
          >
            {compactValue(value)}
          </li>
        ))}
      </ul>
    </div>
  )
}

function FailureAndErrorSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
  report: Record<string, unknown>
}) {
  const failedItems = reportArray(props.report, 'failed_items')
  const commonCauses = arrayValue(props.report.common_causes)
  const limitations = arrayValue(props.report.limitations)
  const errorCode =
    props.execution.final_error_code || props.execution.error_code
  const errorMessage = redactSensitiveText(props.execution.error_message)

  return (
    <ReportSection title='失败项目与错误摘要'>
      {failedItems.length > 0 ? (
        <ul className='min-w-0 border-y'>
          {failedItems.map((item, index) => (
            <FailedItemSummary
              key={listItemKey(props.execution.target_key, item, index)}
              item={item}
              index={index}
            />
          ))}
        </ul>
      ) : (
        <p className='text-muted-foreground text-sm'>报告未提供失败项目</p>
      )}

      <TextList title='常见原因' values={commonCauses} />
      <TextList title='报告限制' values={limitations} />

      {errorCode || errorMessage ? (
        <Alert className='mt-3'>
          <HugeiconsIcon icon={InformationCircleIcon} />
          <AlertTitle>执行错误摘要（不代表模型结论）</AlertTitle>
          <AlertDescription className='break-words'>
            {errorCode ? <code className='break-all'>{errorCode}</code> : null}
            {errorCode && errorMessage ? ' · ' : null}
            {errorMessage}
          </AlertDescription>
        </Alert>
      ) : null}
    </ReportSection>
  )
}

function MetadataSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
}) {
  const execution = props.execution
  return (
    <ReportSection title='报告身份与版本'>
      <dl className='grid min-w-0 grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2 lg:grid-cols-3'>
        <DetailItem label='官方 session'>
          <span className='break-all'>
            {execution.official_session_id || '-'}
          </span>
        </DetailItem>
        <DetailItem label='official 标记'>
          {execution.official ? '是（官方报告）' : '否（非官方报告）'}
        </DetailItem>
        <DetailItem label='配置哈希'>
          <span className='break-all'>{execution.config_hash || '-'}</span>
        </DetailItem>
        <DetailItem label='Schema 版本'>
          {execution.schema_version > 0 ? execution.schema_version : '-'}
        </DetailItem>
        <DetailItem label='Scoring 版本'>
          <span className='break-all'>{execution.scoring_version || '-'}</span>
        </DetailItem>
        <DetailItem label='Baseline ID'>
          <span className='break-all'>{execution.baseline_id || '-'}</span>
        </DetailItem>
        <DetailItem label='Baseline SHA-256'>
          <span className='break-all'>{execution.baseline_sha256 || '-'}</span>
        </DetailItem>
        <DetailItem label='Build 哈希'>
          <span className='break-all'>{execution.build_hash || '-'}</span>
        </DetailItem>
        <DetailItem label='报告 SHA-256'>
          <span className='break-all'>{execution.report_sha256 || '-'}</span>
        </DetailItem>
      </dl>
    </ReportSection>
  )
}

function TechnicalJson(props: { report: unknown; targetKey: string }) {
  return (
    <Accordion defaultValue={[]} className='min-w-0 border-t'>
      <AccordionItem value={`technical-json-${props.targetKey}`}>
        <AccordionTrigger className='min-w-0 py-3'>
          <span className='min-w-0 break-words'>技术 JSON（已脱敏）</span>
        </AccordionTrigger>
        <AccordionContent className='min-w-0'>
          <pre
            className='bg-muted/40 max-w-full min-w-0 rounded-md p-3 text-xs break-all whitespace-pre-wrap'
            data-slot='model-detection-technical-json'
          >
            {safeJson(props.report)}
          </pre>
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}

function ExecutionReport(props: {
  execution: ChannelModelDetectionExecutionDetail
}) {
  const execution = props.execution
  const sanitizedReport = redactReportSecrets(execution.report)
  const report = reportRoot(sanitizedReport)
  const fingerprintModel =
    execution.fingerprint_model || stringValue(report.fingerprint_model)
  const reportMismatch = report.fingerprint_claim_mismatch
  const fingerprintClaimMismatch =
    execution.fingerprint_claim_mismatch === true || reportMismatch === true
  const presentation = outcomePresentation(
    execution,
    fingerprintModel,
    fingerprintClaimMismatch
  )
  const status = EXECUTION_STATUS[execution.status]

  return (
    <article
      className='min-w-0 overflow-x-hidden border-b px-3 last:border-b-0 sm:px-4'
      data-slot='model-detection-execution-report'
      data-target-key={execution.target_key}
      data-outcome-level={presentation.level}
    >
      <header className='flex min-w-0 flex-col gap-2 py-4 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0'>
          <h2 className='min-w-0 font-semibold break-words'>
            {execution.request_model}
          </h2>
          <p className='text-muted-foreground mt-1 text-xs break-words'>
            申报型号{' '}
            {channelModelDetectionClaimedModelLabel(execution.claimed_model)} ·{' '}
            {channelModelDetectionPresetSourceLabel(execution.preset_source)} ·{' '}
            {channelModelDetectionPresetLabel(execution.preset)}
          </p>
        </div>
        <Badge variant={status.variant}>{status.label}</Badge>
      </header>

      <Separator />
      <OutcomeSummary execution={execution} presentation={presentation} />
      <Separator />
      <EvidenceSummary execution={execution} report={report} />
      <Separator />
      <ModelMatchSummary report={report} />
      <Separator />
      <ProgressAndUsage execution={execution} />
      <Separator />
      <CostSummary cost={execution.cost} />
      <Separator />
      <FailureAndErrorSummary execution={execution} report={report} />
      <Separator />
      <MetadataSummary execution={execution} />
      <TechnicalJson
        report={sanitizedReport}
        targetKey={execution.target_key}
      />
    </article>
  )
}

export function ChannelModelDetectionReport(
  props: ChannelModelDetectionReportProps
) {
  if (props.executions.length === 0) {
    return (
      <Empty className='min-h-56' data-slot='model-detection-report'>
        <EmptyHeader>
          <EmptyTitle>暂无目标报告</EmptyTitle>
          <EmptyDescription>
            当前轮次还没有可展示的目标执行详情
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div
      className='min-w-0 overflow-x-hidden'
      data-slot='model-detection-report'
    >
      {props.executions.map((execution) => (
        <ExecutionReport key={execution.target_key} execution={execution} />
      ))}
    </div>
  )
}
