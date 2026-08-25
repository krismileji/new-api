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
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import {
  channelModelDetectionClaimedModelLabel,
  channelModelDetectionPresetLabel,
  channelModelDetectionPresetSourceLabel,
  isChannelModelDetectionStrongFingerprintConflict,
  isKnownChannelModelDetectionOutcome,
} from '../lib/model-detection'
import type {
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
  match: number | null
  threshold: number | null
}

type ReportCompatibility = {
  compatible: boolean
  severity: 'none' | 'warning' | 'incompatible'
  messages: string[]
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

const MODEL_MATCH_ORDER = [
  { key: 'sol', label: 'Sol' },
  { key: 'terra', label: 'Terra' },
  { key: 'luna', label: 'Luna' },
] as const

const FINGERPRINT_REFERENCE_ROWS = [
  {
    preset: '低',
    model: 'Sol',
    simulatedAverage: '91.297%',
    lowerBound99: '54.645%',
    threshold: '>54%',
  },
  {
    preset: '低',
    model: 'Terra',
    simulatedAverage: '92.842%',
    lowerBound99: '58.805%',
    threshold: '>58%',
  },
  {
    preset: '低',
    model: 'Luna',
    simulatedAverage: '98.406%',
    lowerBound99: '77.505%',
    threshold: '>77%',
  },
  {
    preset: '中',
    model: 'Sol',
    simulatedAverage: '93.658%',
    lowerBound99: '82.995%',
    threshold: '>82%',
  },
  {
    preset: '中',
    model: 'Terra',
    simulatedAverage: '94.532%',
    lowerBound99: '84.835%',
    threshold: '>84%',
  },
  {
    preset: '中',
    model: 'Luna',
    simulatedAverage: '99.452%',
    lowerBound99: '97.135%',
    threshold: '>97%',
  },
  {
    preset: '高',
    model: 'Sol',
    simulatedAverage: '99.214%',
    lowerBound99: '98.645%',
    threshold: '>98%',
  },
  {
    preset: '高',
    model: 'Terra',
    simulatedAverage: '98.529%',
    lowerBound99: '97.505%',
    threshold: '>97%',
  },
  {
    preset: '高',
    model: 'Luna',
    simulatedAverage: '99.877%',
    lowerBound99: '99.365%',
    threshold: '>99%',
  },
] as const

const EFFORT_LABELS: Record<string, string> = {
  none: '无',
  minimal: '极低',
  low: '低',
  medium: '中',
  high: '高',
  max: '最大',
  xhigh: '超高',
}

const PROBE_LABELS: Record<string, string> = {
  rand_country: '固定随机国家',
  rand_bird: '固定随机鸟',
  b80_letter_count: 'b80 字符计数',
  juice_coverage: 'Juice 覆盖检测',
  output_luna_48: 'Luna 48 输出控制',
  output_terra_32: 'Terra 32 输出控制',
}

const PROFILE_LABELS: Record<string, string> = {
  'normal+no_history': '普通请求 · 无历史',
  'normal+fixed_32k_history': '普通请求 · 固定 32K 历史',
  'native_codex+no_history': '原生 Codex · 无历史',
  'native_codex+fixed_32k_history': '原生 Codex · 固定 32K 历史',
}

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

function booleanValue(value: unknown) {
  return typeof value === 'boolean' ? value : null
}

function arrayValue(value: unknown) {
  return Array.isArray(value) ? value : []
}

function displayBoolean(value: unknown) {
  const parsed = booleanValue(value)
  if (parsed == null) return '未提供'
  return parsed ? '是' : '否'
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

function reportCompatibility(
  execution: ChannelModelDetectionExecutionDetail,
  report: Record<string, unknown>
): ReportCompatibility {
  const messages: string[] = []
  let compatible = true
  let severity: ReportCompatibility['severity'] = 'none'

  const reportScoring = stringValue(report.scoring_version)
  const storedScoring = execution.scoring_version.trim()
  if (reportScoring && storedScoring && reportScoring !== storedScoring) {
    compatible = false
    severity = 'incompatible'
    messages.push(
      `报告内评分版本 ${reportScoring} 与执行记录 ${storedScoring} 不一致。`
    )
  }

  const candidate = recordValue(report.candidate_configuration_without_key)
  const reportClaimedModel =
    stringValue(report.claimed_model) ||
    stringValue(candidate?.claimed_model) ||
    stringValue(candidate?.model)
  const reportRequestModel =
    stringValue(report.request_model) ||
    stringValue(candidate?.request_model) ||
    stringValue(candidate?.model)
  if (
    reportClaimedModel &&
    reportClaimedModel !== execution.claimed_model.trim()
  ) {
    compatible = false
    severity = 'incompatible'
    messages.push(
      `报告申报型号 ${reportClaimedModel} 与执行快照 ${execution.claimed_model} 不一致。`
    )
  }
  if (
    reportRequestModel &&
    reportRequestModel !== execution.request_model.trim()
  ) {
    compatible = false
    severity = 'incompatible'
    messages.push(
      `报告请求模型 ${reportRequestModel} 与执行快照 ${execution.request_model} 不一致，检测器版本可能不支持独立请求模型。`
    )
  }

  return { compatible, severity, messages }
}

function outcomePresentation(
  execution: ChannelModelDetectionExecutionDetail,
  fingerprintModel: string,
  fingerprintClaimMismatch: boolean,
  compatibility: ReportCompatibility
): OutcomePresentation {
  if (!compatibility.compatible) {
    return {
      level: 'unknown',
      label: '版本不兼容',
      title: '检测报告版本不兼容',
      description:
        '主系统已保留原始报告，但不会把不兼容版本的结论解释为正常或异常。',
      variant: 'warning',
      icon: InformationCircleIcon,
    }
  }
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

function formatPercentage(value: number | null, digits = 3) {
  if (value == null) return '未提供'
  return `${(value * 100).toFixed(digits)}%`
}

function formatDecimal(value: number | null, digits = 3) {
  if (value == null) return '未提供'
  return value.toFixed(digits)
}

function formatOptionalCount(value: unknown) {
  const parsed = numberValue(value)
  return parsed == null ? '未提供' : formatCount(parsed)
}

function modelLabel(value: string) {
  return channelModelDetectionClaimedModelLabel(value)
}

function probeLabel(value: string) {
  return PROBE_LABELS[value] || value.replaceAll('_', ' ') || '未知探针'
}

function profileLabel(value: string) {
  return PROFILE_LABELS[value] || value.replaceAll('+', ' · ') || '未知请求方式'
}

function trustScopeLabel(value: string) {
  if (value === 'official_preset') return '官方预设'
  if (value === 'advisory_only') return '仅供参考'
  return value || '未提供'
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
  const possibleModels = reportArray(report, 'possible_models')
  const rows =
    possibleModels.length > 0
      ? possibleModels
      : arrayValue(report.fingerprint_details)
  for (const value of rows) {
    const row = recordValue(value)
    if (!row) continue
    const model = stringValue(row.model)
    const label = stringValue(row.label_cn)
    const key = modelMatchKey(`${model} ${label}`)
    if (!key) continue
    const match = numberValue(row.match)
    matches.set(key, {
      match,
      threshold: numberValue(row.threshold),
    })
  }
  return matches
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
  report: Record<string, unknown>
  compatibility: ReportCompatibility
}) {
  const execution = props.execution
  const presentation = props.presentation
  const detectorTitle = stringValue(props.report.title_cn)
  const detectorDescription =
    stringValue(props.report.subtitle_cn) ||
    stringValue(props.report.quality_note)
  const title = props.compatibility.compatible
    ? detectorTitle || execution.title_cn || presentation.title
    : presentation.title
  const description = props.compatibility.compatible
    ? detectorDescription || execution.subtitle_cn || presentation.description
    : presentation.description
  const fingerprintModel =
    execution.fingerprint_model || stringValue(props.report.fingerprint_model)
  const fingerprintClaimMismatch =
    execution.fingerprint_claim_mismatch === true ||
    props.report.fingerprint_claim_mismatch === true
  const customPreset = props.report.custom_preset === true
  const customChanges = arrayValue(props.report.custom_changes)
    .map(compactValue)
    .filter(Boolean)
  const unknownCode =
    Boolean(execution.outcome_code) &&
    !isKnownChannelModelDetectionOutcome(execution.outcome_code)

  return (
    <div className='min-w-0 py-4'>
      <span className='text-muted-foreground text-xs'>综合结论</span>
      <div className='mt-2 flex min-w-0 items-start gap-3'>
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
          <p className='text-muted-foreground mt-2 text-sm break-words'>
            申报模型：{modelLabel(execution.claimed_model)}；实际请求模型：
            {execution.request_model || execution.claimed_model || '-'}
            {fingerprintClaimMismatch && fingerprintModel
              ? `；行为指纹强烈指向 ${modelLabel(fingerprintModel)}，与申报不一致。`
              : ''}
          </p>
          {description !== presentation.description ? (
            <p className='text-muted-foreground mt-2 text-xs break-words'>
              系统归纳：{presentation.description}
            </p>
          ) : null}
        </div>
      </div>

      {customPreset ? (
        <Alert className='mt-3'>
          <HugeiconsIcon icon={InformationCircleIcon} />
          <AlertTitle>自定义档位测试结果仅供参考</AlertTitle>
          <AlertDescription className='break-words'>
            修改项目：{customChanges.join('、') || '自定义探针'}。
            匹配度只能用于参考，不能据此判定是否通过。
          </AlertDescription>
        </Alert>
      ) : null}

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

function VersionCompatibilityNotice(props: {
  compatibility: ReportCompatibility
}) {
  if (props.compatibility.messages.length === 0) return null
  const incompatible = props.compatibility.severity === 'incompatible'
  return (
    <div className='py-4'>
      <Alert variant={incompatible ? 'destructive' : 'default'}>
        <HugeiconsIcon icon={InformationCircleIcon} />
        <AlertTitle>
          {incompatible ? '报告版本不兼容' : '报告版本提示'}
        </AlertTitle>
        <AlertDescription>
          <ul className='list-disc space-y-1 pl-4'>
            {props.compatibility.messages.map((message) => (
              <li key={message}>{message}</li>
            ))}
          </ul>
        </AlertDescription>
      </Alert>
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
  const officialGrade = props.report.official_grade
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
        <DetailItem label='官方档级'>
          {displayBoolean(officialGrade)}
        </DetailItem>
        <DetailItem label='信任范围'>{trustScopeLabel(trustScope)}</DetailItem>
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

function ModelMatchSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
  report: Record<string, unknown>
}) {
  const matches = modelMatches(props.report)
  const fingerprint = recordValue(props.report.fingerprint_summary)
  const reasons = arrayValue(fingerprint?.fingerprint_unclear_reasons_cn)
  const fingerprintState =
    props.execution.fingerprint_verdict_state ||
    stringValue(props.report.fingerprint_verdict_state)
  const fingerprintModel =
    props.execution.fingerprint_model ||
    stringValue(props.report.fingerprint_model)
  const strongMatch = fingerprintState === 'strong_match' && fingerprintModel
  return (
    <ReportSection
      title='指纹匹配度'
      description='匹配度表示本批答案分布与可信基线的相对接近程度，不是真实路由概率或混用比例。'
    >
      <div className='mb-3 flex min-w-0 justify-end'>
        <Badge variant={strongMatch ? 'secondary' : 'warning'}>
          {strongMatch
            ? `强烈指向 ${modelLabel(fingerprintModel)}`
            : '证据不明确'}
        </Badge>
      </div>
      <div
        className='min-w-0 border-y'
        role='table'
        aria-label='Sol、Terra、Luna 指纹匹配度'
      >
        {MODEL_MATCH_ORDER.map((model) => {
          const match = matches.get(model.key)
          const percentage =
            match?.match == null
              ? 0
              : Math.max(0, Math.min(100, match.match * 100))
          return (
            <div
              key={model.key}
              className='grid min-w-0 grid-cols-1 gap-2 border-b py-2.5 text-sm last:border-b-0 sm:grid-cols-[5rem_minmax(8rem,1fr)_6rem_9rem] sm:items-center sm:gap-3'
              role='row'
              data-model-match={model.label}
            >
              <span className='font-medium' role='cell'>
                {model.label}
              </span>
              <div className='min-w-0' role='cell'>
                <Progress
                  value={percentage}
                  aria-label={`${model.label} 指纹匹配度 ${formatPercentage(match?.match ?? null)}`}
                  className='min-w-0'
                />
              </div>
              <span className='min-w-0 break-words tabular-nums' role='cell'>
                {formatPercentage(match?.match ?? null)}
              </span>
              <span className='min-w-0 break-words tabular-nums' role='cell'>
                {match?.threshold == null
                  ? '当前模式仅参考'
                  : `强指向线 >${formatPercentage(match.threshold, 0)}`}
              </span>
            </div>
          )
        })}
      </div>

      <Accordion defaultValue={[]} className='mt-3 min-w-0 border'>
        <AccordionItem value='fingerprint-reference' className='px-3'>
          <AccordionTrigger
            className='min-w-0 py-3'
            data-report-section='fingerprint-reference'
          >
            <span className='min-w-0 break-words'>现有验证参考与适用边界</span>
          </AccordionTrigger>
          <AccordionContent className='min-w-0 pb-3'>
            <p className='text-muted-foreground text-sm break-words'>
              指纹匹配度只表示这批固定答案的分布更像哪一个可信模型，不是真实路由概率，也不是账号有多少比例用了该模型。低档每题
              3 次，波动最大；中档每题 10 次更稳；高档比较四种请求格式，最稳定。
            </p>
            <Table className='mt-3 min-w-[36rem]'>
              <TableHeader>
                <TableRow>
                  <TableHead>档位</TableHead>
                  <TableHead>真实模型</TableHead>
                  <TableHead>历史模拟平均</TableHead>
                  <TableHead>约 99% 高于</TableHead>
                  <TableHead>正式线</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {FINGERPRINT_REFERENCE_ROWS.map((row) => (
                  <TableRow key={`${row.preset}-${row.model}`}>
                    <TableCell>{row.preset}</TableCell>
                    <TableCell>{row.model}</TableCell>
                    <TableCell>{row.simulatedAverage}</TableCell>
                    <TableCell>{row.lowerBound99}</TableCell>
                    <TableCell>{row.threshold}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className='text-muted-foreground mt-3 text-sm break-words'>
              阈值来自 Stage C 中与三个探针相关的 3840 条记录：低档模拟 1000
              万轮，中档 500 万轮，高档 100 万轮。低档仍有约 0.07% 至 0.26%
              的错误强指向；中高档在这批模拟中为 0，但不代表现实中永远零误报。
            </p>
            <p className='text-muted-foreground mt-3 text-sm break-words'>
              独立本地 Plus 正式池验证了 Sol 方向；Terra/Luna
              没有完整同契约独立池。官方风控、限流、临时路由和请求契约变化都可能改变结果。
            </p>
            {reasons.length > 0 ? (
              <div className='mt-3 min-w-0'>
                <h5 className='text-sm font-medium'>本次证据不明确原因</h5>
                <ul className='text-muted-foreground mt-1 list-disc space-y-1 pl-5 text-sm'>
                  {reasons.map((reason, index) => (
                    <li
                      key={listItemKey('fingerprint-reason', reason, index)}
                      className='break-words'
                    >
                      {compactValue(reason)}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </ReportSection>
  )
}

function effortOrder(value: string) {
  const order = ['low', 'medium', 'high', 'max', 'xhigh']
  const index = order.indexOf(value)
  return index === -1 ? order.length : index
}

function JuiceSummary(props: { report: Record<string, unknown> }) {
  const summary = recordValue(props.report.juice_summary)
  const perEffort = recordValue(summary?.per_effort)
  if (!perEffort) return null
  const efforts = Object.entries(perEffort)
    .filter((entry): entry is [string, Record<string, unknown>] =>
      isRecord(entry[1])
    )
    .sort(([left], [right]) => effortOrder(left) - effortOrder(right))
  if (efforts.length === 0) return null

  return (
    <ReportSection title='Juice 结果'>
      <Table className='min-w-[52rem]'>
        <TableHeader>
          <TableRow>
            <TableHead>思考档</TableHead>
            <TableHead>尝试</TableHead>
            <TableHead>有效</TableHead>
            <TableHead>申报型号命中</TableHead>
            <TableHead>型号不一致</TableHead>
            <TableHead>未知输出</TableHead>
            <TableHead>网络错误</TableHead>
            <TableHead>共享值命中</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {efforts.map(([effort, value]) => (
            <TableRow key={effort}>
              <TableCell>{EFFORT_LABELS[effort] || effort}</TableCell>
              <TableCell>{formatOptionalCount(value.attempted)}</TableCell>
              <TableCell>
                {formatOptionalCount(value.valid_completed)}
              </TableCell>
              <TableCell>
                {formatOptionalCount(value.current_success)}
              </TableCell>
              <TableCell>{formatOptionalCount(value.mixed)}</TableCell>
              <TableCell>{formatOptionalCount(value.unsuccessful)}</TableCell>
              <TableCell>{formatOptionalCount(value.network_error)}</TableCell>
              <TableCell>
                {formatOptionalCount(value.shared_current_success)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ReportSection>
  )
}

function DeterministicSummary(props: { report: Record<string, unknown> }) {
  const output = recordValue(props.report.output_integrity_summary)
  const coverage = recordValue(props.report.coverage_summary)
  if (!output && !coverage) return null
  const outputAlarm =
    booleanValue(output?.hard_anomaly) === true ||
    booleanValue(output?.sticky_hard_anomaly) === true
  const coverageAlarm =
    booleanValue(coverage?.hard_anomaly) === true ||
    booleanValue(coverage?.sticky_hard_anomaly) === true
  const outputStickyHistory =
    booleanValue(output?.sticky_hard_anomaly) === true &&
    booleanValue(output?.hard_anomaly) !== true
  const coverageStickyHistory =
    booleanValue(coverage?.sticky_hard_anomaly) === true &&
    booleanValue(coverage?.hard_anomaly) !== true

  return (
    <ReportSection title='输出完整性与覆盖检测'>
      <div className='min-w-0 border-y'>
        <div className='border-b py-3 last:border-b-0'>
          <h5 className='text-sm font-medium'>32/48 输出完整性</h5>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            成功响应 {formatOptionalCount(output?.requests)} 条，精确返回{' '}
            {formatOptionalCount(output?.exact)} 条，格式无效{' '}
            {formatOptionalCount(output?.invalid)} 条。
            {outputAlarm
              ? `检测到 40 或 40 开头的输出改写${outputStickyHistory ? '（来自本会话历史粘性事件）' : ''}。`
              : '没有检测到 40 或 40 开头的输出改写。'}
          </p>
        </div>
        <div className='py-3'>
          <h5 className='text-sm font-medium'>Juice 显式覆盖检测</h5>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            成功响应 {formatOptionalCount(coverage?.requests)} 条。
            {coverageAlarm
              ? `检测到显式定义可能被隐藏提示覆盖${coverageStickyHistory ? '（来自本会话历史粘性事件）' : ''}。`
              : '没有检测到明确的隐藏覆盖。'}
          </p>
        </div>
      </div>
    </ReportSection>
  )
}

function answerCountsText(value: unknown) {
  const counts = recordValue(value)
  if (!counts) return '无'
  const labels: Record<string, string> = {
    __INVALID_OUTPUT__: '格式无效',
    __OTHER__: '其他合法答案',
  }
  const entries = Object.entries(counts)
    .map(([key, count]) => [key, numberValue(count)] as const)
    .filter(
      (entry): entry is readonly [string, number] =>
        entry[1] != null && entry[1] > 0
    )
    .sort((left, right) => right[1] - left[1])
    .slice(0, 5)
  if (entries.length === 0) return '无'
  return entries
    .map(([key, count]) => `${labels[key] || key} ${formatCount(count)}`)
    .join('；')
}

function contributionDirection(
  value: Record<string, unknown>,
  familyContributions: Record<string, unknown> | null
) {
  const weight = numberValue(value.weight)
  if (weight == null || weight <= 0) return '不参与匹配'
  const probe = stringValue(value.probe_id)
  const family = recordValue(familyContributions?.[probe])
  const scores =
    recordValue(value.average_log_likelihood) ||
    recordValue(family?.model_contributions)
  if (!scores) return '方向不明确'
  const ranked = Object.entries(scores)
    .map(([model, score]) => [model, numberValue(score)] as const)
    .filter((entry): entry is readonly [string, number] => entry[1] != null)
    .sort((left, right) => right[1] - left[1])
  if (ranked.length === 0) return '方向不明确'
  return `更支持 ${modelLabel(ranked[0][0])}`
}

function FingerprintProbeSummary(props: { report: Record<string, unknown> }) {
  const fingerprint = recordValue(props.report.fingerprint_summary)
  const cellDetails = recordValue(fingerprint?.cell_details)
  const familyContributions = recordValue(fingerprint?.family_contributions)
  const cells = Object.entries(cellDetails ?? {}).filter(
    (entry): entry is [string, Record<string, unknown>] => isRecord(entry[1])
  )
  const referenceResults = arrayValue(
    props.report.reference_fingerprint_results
  ).filter(isRecord)
  if (cells.length === 0 && referenceResults.length === 0) return null

  return (
    <ReportSection title='行为指纹探针'>
      {cells.length > 0 ? (
        <Table className='min-w-[70rem]'>
          <TableHeader>
            <TableRow>
              <TableHead>探针</TableHead>
              <TableHead>请求方式</TableHead>
              <TableHead>完成/计划</TableHead>
              <TableHead>主要答案</TableHead>
              <TableHead>贡献方向</TableHead>
              <TableHead>模型差异 S</TableHead>
              <TableHead>时间漂移 D</TableHead>
              <TableHead>权重 w</TableHead>
              <TableHead>90% 门禁</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cells.map(([cellKey, cell]) => {
              const probe = stringValue(cell.probe_id)
              const profile = stringValue(cell.profile)
              return (
                <TableRow key={cellKey}>
                  <TableCell>{probeLabel(probe)}</TableCell>
                  <TableCell>{profileLabel(profile)}</TableCell>
                  <TableCell>
                    {formatOptionalCount(cell.sample_count)} /{' '}
                    {formatOptionalCount(cell.planned_samples)}
                  </TableCell>
                  <TableCell className='max-w-64 break-words whitespace-normal'>
                    {answerCountsText(cell.counts)}
                  </TableCell>
                  <TableCell>
                    {contributionDirection(cell, familyContributions)}
                  </TableCell>
                  <TableCell>
                    {formatDecimal(numberValue(cell.between_model_jsd))}
                  </TableCell>
                  <TableCell>
                    {formatDecimal(numberValue(cell.within_model_jsd))}
                  </TableCell>
                  <TableCell>
                    {formatDecimal(numberValue(cell.weight))}
                  </TableCell>
                  <TableCell>
                    {booleanValue(cell.complete) === true ? '达到' : '未达到'}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      ) : null}

      {referenceResults.length > 0 ? (
        <div className='mt-3 min-w-0 border-y'>
          {referenceResults.map((result, index) => {
            const probe = stringValue(result.probe_id)
            const matches = recordValue(result.fingerprint_match)
            const matchText = Object.entries(matches ?? {})
              .map(([model, value]) => {
                return `${modelLabel(model)} ${formatPercentage(numberValue(value))}`
              })
              .join('；')
            return (
              <div
                key={listItemKey('reference-fingerprint', result, index)}
                className='min-w-0 border-b py-3 last:border-b-0'
              >
                <h5 className='text-sm font-medium break-words'>
                  {probeLabel(probe)}（自定义参考）
                </h5>
                <p className='text-muted-foreground mt-1 text-sm break-words'>
                  {matchText || '没有可计算的参考匹配度'}
                </p>
                <p className='text-muted-foreground mt-1 text-xs break-words'>
                  自定义探针不参与正式强指向，每个探针单独显示。
                </p>
              </div>
            )
          })}
        </div>
      ) : null}
    </ReportSection>
  )
}

function ProfileSummary(props: { report: Record<string, unknown> }) {
  const summary = recordValue(props.report.profile_summary)
  if (!summary) return null
  const profiles = Object.entries(summary).filter(
    (entry): entry is [string, Record<string, unknown>] => isRecord(entry[1])
  )
  if (profiles.length === 0) return null

  return (
    <ReportSection title='请求格式对比'>
      <Table className='min-w-[34rem]'>
        <TableHeader>
          <TableRow>
            <TableHead>请求方式</TableHead>
            <TableHead>任务</TableHead>
            <TableHead>成功</TableHead>
            <TableHead>错误</TableHead>
            <TableHead>取消</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {profiles.map(([profile, value]) => (
            <TableRow key={profile}>
              <TableCell>{profileLabel(profile)}</TableCell>
              <TableCell>{formatOptionalCount(value.logical_tasks)}</TableCell>
              <TableCell>{formatOptionalCount(value.successful)}</TableCell>
              <TableCell>{formatOptionalCount(value.final_errors)}</TableCell>
              <TableCell>{formatOptionalCount(value.cancelled)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ReportSection>
  )
}

function ProgressAndUsage(props: {
  execution: ChannelModelDetectionExecutionDetail
  report: Record<string, unknown>
}) {
  const execution = props.execution
  const network = recordValue(props.report.network_summary)
  const logicalTasks = numberValue(network?.logical_tasks)
  const logicalCompleted = numberValue(network?.logical_completed)
  const successful = numberValue(network?.successful)
  const finalErrors = numberValue(network?.final_errors)
  const cancelled = numberValue(network?.cancelled)
  const httpAttempts = numberValue(network?.http_attempts)
  const retries = numberValue(network?.retries)
  return (
    <ReportSection title='线路质量'>
      <dl className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4'>
        <DetailItem label='逻辑任务'>
          {formatCount(logicalTasks ?? execution.progress.planned)}
        </DetailItem>
        <DetailItem label='逻辑完成'>
          {formatCount(
            logicalCompleted ?? execution.progress.logical_completed
          )}
        </DetailItem>
        <DetailItem label='成功'>
          {formatCount(successful ?? execution.progress.successful)}
        </DetailItem>
        <DetailItem label='最终错误'>
          {formatCount(finalErrors ?? execution.progress.errors)}
        </DetailItem>
        <DetailItem label='取消'>
          {formatCount(cancelled ?? execution.progress.cancelled)}
        </DetailItem>
        <DetailItem label='HTTP 尝试'>
          {formatCount(httpAttempts ?? execution.progress.http_attempts)}
        </DetailItem>
        <DetailItem label='重试'>
          {formatCount(retries ?? execution.progress.retries)}
        </DetailItem>
      </dl>

      <div className='mt-4 min-w-0 border-t pt-4'>
        <h5 className='mb-3 text-sm font-medium'>调用 Usage</h5>
        {execution.usage_available ? (
          <dl className='grid min-w-0 grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-3'>
            <DetailItem label='输入 Token'>
              {formatCount(execution.input_tokens)}
            </DetailItem>
            <DetailItem label='输出 Token'>
              {formatCount(execution.output_tokens)}
            </DetailItem>
            <DetailItem label='总 Token'>
              {formatCount(execution.total_tokens)}
            </DetailItem>
          </dl>
        ) : (
          <p className='text-muted-foreground text-sm'>Usage 暂不可用</p>
        )}
      </div>
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
  const incompleteCells = arrayValue(item.incomplete_cells)
  const structuredIncompleteCells = incompleteCells.filter(isRecord)
  const unstructuredIncompleteCells = incompleteCells.filter(
    (value) => !isRecord(value)
  )
  const details = [
    { label: '证据', value: item.evidence },
    {
      label: '未完成探针格',
      value:
        unstructuredIncompleteCells.length > 0
          ? unstructuredIncompleteCells
          : null,
    },
    {
      label: '缺少当前成功 effort',
      value:
        arrayValue(item.missing_current_success_efforts).length > 0
          ? item.missing_current_success_efforts
          : null,
    },
    {
      label: '有效 effort 不足',
      value:
        arrayValue(item.insufficient_valid_efforts).length > 0
          ? item.insufficient_valid_efforts
          : numberValue(item.insufficient_valid_efforts),
    },
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
      {structuredIncompleteCells.length > 0 ? (
        <ul className='text-muted-foreground mt-2 list-disc space-y-1 pl-5 text-xs'>
          {structuredIncompleteCells.map((cell, index) => {
            const [probe, profile] = stringValue(cell.cell).split('|')
            const planned = numberValue(cell.planned)
            const completed = numberValue(cell.completed)
            const minimum = numberValue(cell.minimum)
            const missing =
              completed == null || minimum == null
                ? null
                : Math.max(0, minimum - completed)
            return (
              <li
                key={listItemKey('incomplete-cell', cell, index)}
                className='break-words'
              >
                {probeLabel(probe)} · {profileLabel(profile)}：计划{' '}
                {planned == null ? '未提供' : formatCount(planned)}，完成{' '}
                {completed == null ? '未提供' : formatCount(completed)}
                ，至少需要 {minimum == null ? '未提供' : formatCount(minimum)}
                ，缺少 {missing == null ? '未提供' : formatCount(missing)}
              </li>
            )
          })}
        </ul>
      ) : null}
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

function NetworkErrorSummary(props: { item: unknown; index: number }) {
  const item = recordValue(props.item)
  if (!item) return null
  const probe = probeLabel(stringValue(item.probe_id))
  const category = stringValue(item.category_cn) || '线路错误'
  const status = numberValue(item.http_status)
  const attempt = numberValue(item.attempt)
  const rawMessage = redactSensitiveText(stringValue(item.safe_message))
  const upstreamHTTPError = rawMessage.match(
    /^upstream_http_error \(HTTP (\d+)\)$/
  )
  let message = rawMessage
  if (upstreamHTTPError) {
    message = `上游返回 HTTP 错误（HTTP ${upstreamHTTPError[1]}）`
  } else if (rawMessage === 'truncated_or_invalid_stream') {
    message = '流式响应不完整或格式错误'
  } else if (rawMessage === 'connection_or_transport_error') {
    message = '网络连接或传输失败'
  } else if (rawMessage === 'timeout') {
    message = '等待上游响应超时'
  }

  return (
    <li className='min-w-0 border-b py-2.5 last:border-b-0'>
      <div className='flex min-w-0 flex-wrap items-center gap-2'>
        <span className='text-sm font-medium break-words'>
          {probe} · {category}
        </span>
        <Badge variant='outline'>线路错误</Badge>
      </div>
      <p className='text-muted-foreground mt-1 text-xs break-words'>
        HTTP {status == null ? '未提供' : status}，第{' '}
        {attempt == null ? props.index + 1 : attempt} 次尝试。
        {message ? ` ${message}` : ''}
      </p>
      <p className='text-muted-foreground mt-1 text-xs break-words'>
        常见原因包括地址或权限配置错误、上游限流、线路中断、响应流未完整结束。
      </p>
    </li>
  )
}

function FailureAndErrorSummary(props: {
  execution: ChannelModelDetectionExecutionDetail
  report: Record<string, unknown>
}) {
  const failedItems = reportArray(props.report, 'failed_items')
  const networkErrors = arrayValue(props.report.network_error_details)
  const commonCauses = arrayValue(props.report.common_causes)
  const errorCode =
    props.execution.final_error_code || props.execution.error_code
  const errorMessage = redactSensitiveText(props.execution.error_message)

  return (
    <ReportSection title='未通过或未完成项目'>
      {failedItems.length > 0 || networkErrors.length > 0 ? (
        <ul className='min-w-0 border-y'>
          {failedItems.map((item, index) => (
            <FailedItemSummary
              key={listItemKey(props.execution.target_key, item, index)}
              item={item}
              index={index}
            />
          ))}
          {networkErrors.map((item, index) => (
            <NetworkErrorSummary
              key={listItemKey('network-error', item, index)}
              item={item}
              index={index}
            />
          ))}
        </ul>
      ) : (
        <p className='text-muted-foreground text-sm'>没有未通过或未完成项目</p>
      )}

      <TextList title='常见原因' values={commonCauses} />

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
  report: Record<string, unknown>
}) {
  const execution = props.execution
  const schemaVersion =
    numberValue(props.report.schema_version) ?? execution.schema_version
  const scoringVersion =
    stringValue(props.report.scoring_version) || execution.scoring_version
  const baselineID =
    stringValue(props.report.baseline_id) || execution.baseline_id
  const baselineSHA256 =
    stringValue(props.report.baseline_sha256) || execution.baseline_sha256
  const buildHash = stringValue(props.report.build_hash) || execution.build_hash
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
          {schemaVersion > 0 ? schemaVersion : '-'}
        </DetailItem>
        <DetailItem label='Scoring 版本'>
          <span className='break-all'>{scoringVersion || '-'}</span>
        </DetailItem>
        <DetailItem label='Baseline ID'>
          <span className='break-all'>{baselineID || '-'}</span>
        </DetailItem>
        <DetailItem label='Baseline SHA-256'>
          <span className='break-all'>{baselineSHA256 || '-'}</span>
        </DetailItem>
        <DetailItem label='Build 哈希'>
          <span className='break-all'>{buildHash || '-'}</span>
        </DetailItem>
        <DetailItem label='报告 SHA-256'>
          <span className='break-all'>{execution.report_sha256 || '-'}</span>
        </DetailItem>
      </dl>
    </ReportSection>
  )
}

function TechnicalSummary(props: { report: unknown; targetKey: string }) {
  const report = reportRoot(props.report)
  const limitations = arrayValue(report.limitations)
  return (
    <Accordion defaultValue={[]} className='min-w-0 border-t'>
      <AccordionItem value={`technical-json-${props.targetKey}`}>
        <AccordionTrigger
          className='min-w-0 py-3'
          data-report-section='model-detection-technical'
        >
          <span className='min-w-0 break-words'>方法、限制和 JSON 摘要</span>
        </AccordionTrigger>
        <AccordionContent className='min-w-0'>
          <TextList title='限制与适用边界' values={limitations} />
          <h5 className='mt-3 mb-2 text-xs font-medium'>JSON 摘要（已脱敏）</h5>
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
  const compatibility = reportCompatibility(execution, report)
  const presentation = outcomePresentation(
    execution,
    fingerprintModel,
    fingerprintClaimMismatch,
    compatibility
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
      <OutcomeSummary
        execution={execution}
        presentation={presentation}
        report={report}
        compatibility={compatibility}
      />
      {compatibility.messages.length > 0 ? (
        <>
          <Separator />
          <VersionCompatibilityNotice compatibility={compatibility} />
        </>
      ) : null}
      <Separator />
      <ModelMatchSummary execution={execution} report={report} />
      <Separator />
      <JuiceSummary report={report} />
      <Separator />
      <DeterministicSummary report={report} />
      <Separator />
      <ProgressAndUsage execution={execution} report={report} />
      <Separator />
      <FailureAndErrorSummary execution={execution} report={report} />
      <Separator />
      <FingerprintProbeSummary report={report} />
      <Separator />
      <ProfileSummary report={report} />
      <Separator />
      <EvidenceSummary execution={execution} report={report} />
      <Separator />
      <MetadataSummary execution={execution} report={report} />
      <TechnicalSummary
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
      {props.executions.length === 1 ? (
        <ExecutionReport
          key={props.executions[0].target_key}
          execution={props.executions[0]}
        />
      ) : (
        <Tabs
          defaultValue={props.executions[0].target_key}
          className='min-w-0 gap-0'
          orientation='horizontal'
        >
          <TabsList className='no-scrollbar flex h-auto w-full flex-nowrap justify-start overflow-x-auto rounded-none border-b bg-transparent p-1'>
            {props.executions.map((execution) => (
              <TabsTrigger
                key={execution.target_key}
                value={execution.target_key}
                className='min-w-32 max-w-64 flex-none px-3 py-2 text-xs'
                aria-label={`切换到 ${execution.request_model} 检测结果`}
                title={execution.request_model}
              >
                <span className='truncate'>{execution.request_model}</span>
              </TabsTrigger>
            ))}
          </TabsList>
          {props.executions.map((execution) => (
            <TabsContent
              key={execution.target_key}
              value={execution.target_key}
              className='min-w-0'
            >
              <ExecutionReport execution={execution} />
            </TabsContent>
          ))}
        </Tabs>
      )}
    </div>
  )
}
