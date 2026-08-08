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
import { ArrowDown01Icon, Calculator01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Badge } from '@/components/ui/badge'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

import type {
  ChannelMonitorSmartScheduleEconomicRole,
  ChannelMonitorSmartScheduleScoreComponent,
  ChannelMonitorSmartScheduleScoreDetails,
  ChannelMonitorSmartScheduleScoreMetricInput,
} from '../types'

type ScoreMetricKey = 'cost_ratio' | 'first_token_ms' | 'tps'

const NORMALIZATION_EPSILON = 1e-9

const SCORE_METRICS: readonly {
  key: ScoreMetricKey
  label: string
}[] = [
  { key: 'cost_ratio', label: '成本倍率' },
  { key: 'first_token_ms', label: '首字' },
  { key: 'tps', label: 'TPS' },
]

const STRATEGY_LABELS: Record<
  ChannelMonitorSmartScheduleScoreDetails['strategy'],
  string
> = {
  ratio: '成本倍率综合',
  first_token: '首字优先',
  tps: 'TPS 优先',
  smart: '智能综合',
}

function formatScore(value: number | null | undefined, digits = 2) {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${(value * 100).toFixed(digits)} 分`
}

function formatPercent(value: number | null | undefined, digits = 1) {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${value.toFixed(digits)}%`
}

function formatPoints(value: number | null | undefined, digits = 2) {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${value.toFixed(digits)} 分`
}

function formatMetricValue(key: ScoreMetricKey, value: number | null) {
  if (value == null || !Number.isFinite(value)) return '-'
  if (key === 'cost_ratio') return `x${value.toFixed(4)}`
  if (key === 'first_token_ms') return `${value.toFixed(0)} ms`
  return `${value.toFixed(2)} token/s`
}

function formatEconomicRatio(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '-'
  return value.toFixed(6)
}

function getEconomicRoleLabel(role: ChannelMonitorSmartScheduleEconomicRole) {
  if (role === 'break_even_fallback') return '保本兜底'
  if (role === 'normal') return '正常盈利'
  return '无法判断'
}

function formatChannelReference(
  channelId: number,
  channelNameById: ReadonlyMap<number, string> | undefined
) {
  if (channelId <= 0) return '无'
  const channelName = channelNameById?.get(channelId)
  return channelName ? `${channelName}（ID ${channelId}）` : `渠道 ${channelId}`
}

function getUnavailableReason(
  key: ScoreMetricKey,
  input: ChannelMonitorSmartScheduleScoreMetricInput,
  component: ChannelMonitorSmartScheduleScoreComponent,
  minimumSamples: number,
  comparableCount: number,
  minimumComparableChannels: number
) {
  if (component.configured_weight_percent <= 0) return '配置权重为 0%'
  if (input.value == null) return '没有可用数据'
  if (key !== 'cost_ratio' && input.sample_count < minimumSamples) {
    return `样本 ${input.sample_count}/${minimumSamples}`
  }
  if (component.comparison_state === 'insufficient') {
    return `可比渠道不足（${comparableCount}/${minimumComparableChannels}），暂不比较`
  }
  return '本轮未计入'
}

function ScoreMetricRow(props: {
  metricKey: ScoreMetricKey
  label: string
  details: ChannelMonitorSmartScheduleScoreDetails
}) {
  const input = props.details.inputs[props.metricKey]
  const cohort = props.details.cohort[props.metricKey]
  const component = props.details.components[props.metricKey]
  const contribution = component.available
    ? (component.normalized_score ?? 0) * component.effective_weight_percent
    : null
  let componentStatus = '未计入'
  if (component.available) {
    componentStatus = '已计入'
  } else if (component.comparison_state === 'insufficient') {
    componentStatus = '不可比较'
  }
  let calculation = `未计入：${getUnavailableReason(
    props.metricKey,
    input,
    component,
    props.details.minimum_samples,
    cohort.available_count,
    props.details.minimum_comparable_channels
  )}`
  if (
    component.available &&
    input.value != null &&
    cohort.minimum != null &&
    cohort.maximum != null
  ) {
    const current = formatMetricValue(props.metricKey, input.value)
    const minimum = formatMetricValue(props.metricKey, cohort.minimum)
    const maximum = formatMetricValue(props.metricKey, cohort.maximum)
    let normalization = ''
    if (Math.abs(cohort.maximum - cohort.minimum) <= NORMALIZATION_EPSILON) {
      normalization = `同池可用值相同，统一按 ${formatScore(component.normalized_score)}`
    } else if (props.metricKey === 'tps') {
      normalization = `(${current} - ${minimum}) / (${maximum} - ${minimum}) = ${formatScore(component.normalized_score)}`
    } else {
      normalization = `(${maximum} - ${current}) / (${maximum} - ${minimum}) = ${formatScore(component.normalized_score)}`
    }
    calculation = `归一化：${normalization}；业务贡献：${formatScore(component.normalized_score)} x ${formatPercent(component.effective_weight_percent)} = ${formatPoints(contribution)}`
  }

  return (
    <div
      role='row'
      className='grid min-w-0 gap-x-3 gap-y-1 border-t px-3 py-2 text-xs sm:grid-cols-[7rem_minmax(8rem,1.2fr)_minmax(8rem,1fr)_5rem_minmax(7rem,auto)_5rem] sm:items-center'
    >
      <div
        role='cell'
        className='flex flex-wrap items-center gap-2 font-medium'
      >
        <span
          className='shrink-0 whitespace-nowrap'
          data-score-metric-label={props.metricKey}
        >
          {props.label}
        </span>
        <Badge
          variant={component.available ? 'secondary' : 'outline'}
          className='shrink-0'
        >
          {componentStatus}
        </Badge>
      </div>
      <div role='cell' className='min-w-0 tabular-nums'>
        <span className='font-mono'>
          {formatMetricValue(props.metricKey, input.value)}
        </span>
        {props.metricKey !== 'cost_ratio' ? (
          <span className='text-muted-foreground ml-2'>
            {input.sample_count} 个样本
          </span>
        ) : null}
      </div>
      <div role='cell' className='font-mono tabular-nums'>
        <span className='text-muted-foreground sm:hidden'>同池范围 </span>
        {formatMetricValue(props.metricKey, cohort.minimum)} -{' '}
        {formatMetricValue(props.metricKey, cohort.maximum)}
        <span className='text-muted-foreground ml-1 font-sans'>
          ({cohort.available_count} 条)
        </span>
      </div>
      <div role='cell' className='font-mono tabular-nums'>
        <span className='text-muted-foreground sm:hidden'>归一化 </span>
        {formatScore(component.normalized_score)}
      </div>
      <div role='cell' className='tabular-nums'>
        <span className='text-muted-foreground sm:hidden'>权重 </span>
        配置 {formatPercent(component.configured_weight_percent)}
        <span className='text-muted-foreground ml-1'>
          / 有效 {formatPercent(component.effective_weight_percent)}
        </span>
      </div>
      <div role='cell' className='font-mono font-medium tabular-nums'>
        <span className='text-muted-foreground sm:hidden'>贡献 </span>
        {formatPoints(contribution)}
      </div>
      <p className='text-muted-foreground min-w-0 break-words sm:col-span-6'>
        {calculation}
      </p>
    </div>
  )
}

type ChannelMonitorSmartScheduleScoreDetailsProps = {
  details: ChannelMonitorSmartScheduleScoreDetails | null | undefined
  className?: string
  snapshotLabel?: string
  inputLabel?: string
  showDecision?: boolean
  defaultOpen?: boolean
  channelNameById?: ReadonlyMap<number, string>
}

export function ChannelMonitorSmartScheduleScoreDetails(
  props: ChannelMonitorSmartScheduleScoreDetailsProps
) {
  if (!props.details) return null

  const details = props.details
  const actualTopLayerChannelIds =
    details.decision.actual_top_layer_channel_ids ?? []
  const actualTopLayerChannels = actualTopLayerChannelIds
    .map((channelId) =>
      formatChannelReference(channelId, props.channelNameById)
    )
    .join('、')
  let temporaryTrafficLabel = ''
  if (details.decision.temporary_traffic_kind === 'insufficient_samples') {
    temporaryTrafficLabel = '样本不足补量'
  } else if (details.decision.temporary_traffic_kind === 'priority_sampling') {
    temporaryTrafficLabel = '低优先级轮转'
  } else if (details.decision.temporary_traffic_kind === 'adaptive_sampling') {
    temporaryTrafficLabel = '健康应急采样'
  }
  let stabilityState = '未启用'
  if (details.stability.enabled) stabilityState = '未达到可用条件'
  if (details.stability.applied) {
    stabilityState = `有效权重 ${formatPercent(details.stability.effective_weight_percent)}`
  }
  const businessContributions = SCORE_METRICS.flatMap((metric) => {
    const component = details.components[metric.key]
    if (!component.available || component.normalized_score == null) return []
    return [
      `${metric.label} ${formatPoints(
        component.normalized_score * component.effective_weight_percent
      )}`,
    ]
  })
  const businessCalculation = `${businessContributions.join(' + ') || '无可用业务指标'} = ${formatScore(details.business_score)}`
  let finalCalculation = `稳定性未计入，最终得分 = 业务得分 ${formatScore(details.business_score)}`
  if (details.stability.applied) {
    const stabilityWeight = details.stability.effective_weight_percent
    finalCalculation = `业务得分 ${formatScore(details.business_score)} x ${formatPercent(100 - stabilityWeight)} + 稳定性 ${formatScore(details.stability.raw_score)} x ${formatPercent(stabilityWeight)} = ${formatScore(details.final_score)}`
  }
  let comparisonStateLabel = ''
  let comparisonStateVariant: 'outline' | 'warning' = 'outline'
  if (details.comparison_state === 'insufficient') {
    comparisonStateLabel = '评分状态：不可比较'
    comparisonStateVariant = 'warning'
  } else if (details.comparison_state === 'comparable') {
    comparisonStateLabel = '评分状态：可比较'
  }
  let healthStateLabel = '未知'
  let healthStateVariant: 'outline' | 'warning' = 'outline'
  if (details.health?.state === 'high_risk') {
    healthStateLabel = '高风险'
    healthStateVariant = 'warning'
  } else if (details.health?.state === 'pressure') {
    healthStateLabel = '降压'
    healthStateVariant = 'warning'
  } else if (details.health?.state === 'observation') {
    healthStateLabel = '观察'
  } else if (details.health?.state === 'healthy') {
    healthStateLabel = '健康'
  }

  return (
    <Collapsible
      defaultOpen={props.defaultOpen}
      className={cn(
        'group/score-details border-border/70 border-t',
        props.className
      )}
    >
      <CollapsibleTrigger className='hover:bg-muted/40 focus-visible:ring-ring/50 flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs transition-colors outline-none focus-visible:ring-3'>
        <span className='flex min-w-0 items-center gap-2'>
          <HugeiconsIcon icon={Calculator01Icon} aria-hidden='true' />
          <span className='font-medium'>评分计算</span>
          <span className='text-muted-foreground truncate'>
            {props.snapshotLabel ?? '执行时评分快照'}
          </span>
        </span>
        <span className='flex shrink-0 items-center gap-2 font-mono tabular-nums'>
          {formatScore(details.final_score)}
          <HugeiconsIcon
            icon={ArrowDown01Icon}
            className='text-muted-foreground transition-transform group-data-[panel-open]/score-details:rotate-180'
            aria-hidden='true'
          />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='bg-muted/15 border-t'>
          <div className='flex flex-wrap items-center gap-2 px-3 py-2 text-xs'>
            <Badge variant='outline'>{STRATEGY_LABELS[details.strategy]}</Badge>
            <Badge variant='secondary'>渠道 + 模型共享样本</Badge>
            {details.sample_group_count > 0 ? (
              <span className='text-muted-foreground'>
                业务样本覆盖 {details.sample_group_count} 个分组
              </span>
            ) : null}
            <span className='text-muted-foreground'>
              性能与稳定性指标最低 {details.minimum_samples} 个样本
            </span>
            <span className='text-muted-foreground'>
              最少 {details.minimum_comparable_channels} 个可比渠道
            </span>
            {comparisonStateLabel ? (
              <Badge variant={comparisonStateVariant}>
                {comparisonStateLabel}
              </Badge>
            ) : null}
            <span className='text-muted-foreground'>
              {details.decision.apply_mode === 'priority_weight'
                ? '优先级 + 权重'
                : '仅权重'}
            </span>
            {details.cohort.priority != null ? (
              <span className='text-muted-foreground'>
                归一化候选层 P{details.cohort.priority}
              </span>
            ) : null}
            {details.decision.force_reset ? (
              <Badge variant='warning'>强制重算</Badge>
            ) : null}
            {details.health ? (
              <Badge variant={healthStateVariant}>
                健康状态：{healthStateLabel}
              </Badge>
            ) : null}
          </div>
          <p className='text-muted-foreground border-t px-3 py-2 text-xs'>
            成本倍率和首字越低越好，TPS
            越高越好；本轮只使用达到样本门槛的指标，并把它们的配置权重按比例重新分配到
            100%。
          </p>

          {details.economics ? (
            <div className='grid gap-2 border-t px-3 py-2 text-xs sm:grid-cols-3'>
              <div>
                <span className='text-muted-foreground block'>成本倍率</span>
                <strong className='font-mono tabular-nums'>
                  {formatEconomicRatio(details.economics.cost_ratio)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>分组倍率</span>
                <strong className='font-mono tabular-nums'>
                  {formatEconomicRatio(details.economics.group_ratio)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>倍率差</span>
                <strong className='font-mono tabular-nums'>
                  {formatEconomicRatio(details.economics.gross_margin)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>经济身份</span>
                <Badge
                  variant={
                    details.economics.role === 'break_even_fallback'
                      ? 'warning'
                      : 'outline'
                  }
                  className='mt-0.5'
                >
                  {getEconomicRoleLabel(details.economics.role)}
                </Badge>
              </div>
              {details.economics.role === 'break_even_fallback' ? (
                <p className='text-muted-foreground sm:col-span-4'>
                  成本倍率与分组倍率相等，自动调度将该路由放入 P1
                  保本兜底层；管理员手动固定时仍按固定结果执行。
                </p>
              ) : null}
            </div>
          ) : null}

          {details.health ? (
            <div className='grid gap-2 border-t px-3 py-2 text-xs sm:grid-cols-3'>
              <div>
                <span className='text-muted-foreground block'>健康压力</span>
                <strong className='font-mono tabular-nums'>
                  {formatPercent(details.health.pressure * 100)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>错误压力</span>
                <strong className='font-mono tabular-nums'>
                  {formatPercent(details.health.error_pressure * 100)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>首字压力</span>
                <strong className='font-mono tabular-nums'>
                  {formatPercent(details.health.latency_pressure * 100)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>
                  窗口内风险请求
                </span>
                <strong className='font-mono tabular-nums'>
                  {formatPercent(details.health.risk_request_percent)}
                </strong>
              </div>
              <div>
                <span className='text-muted-foreground block'>
                  窗口内健康请求
                </span>
                <strong className='font-mono tabular-nums'>
                  {formatPercent(details.health.healthy_request_percent)}
                </strong>
              </div>
              <p className='text-muted-foreground sm:col-span-3'>
                最近 {details.health.window_seconds} 秒统计了{' '}
                {details.health.sample_count}{' '}
                个等价样本（业务请求、手动测试和定时探测）；风险包含非 429
                错误和达到首字告警阈值的成功请求，
                无首字数据的成功请求按健康处理。首字和 TPS
                只有达到最少可比渠道数后才参与相对比较。
              </p>
            </div>
          ) : null}

          <div role='table' aria-label='评分输入与分项计算'>
            <div
              role='row'
              className='text-muted-foreground hidden grid-cols-[7rem_minmax(8rem,1.2fr)_minmax(8rem,1fr)_5rem_minmax(7rem,auto)_5rem] gap-3 border-t px-3 py-1.5 text-[11px] sm:grid'
            >
              <div role='columnheader'>指标</div>
              <div role='columnheader'>{props.inputLabel ?? '执行时输入'}</div>
              <div role='columnheader'>同池归一化范围</div>
              <div role='columnheader'>得分</div>
              <div role='columnheader'>配置 / 有效权重</div>
              <div role='columnheader'>业务贡献</div>
            </div>
            {SCORE_METRICS.map((metric) => (
              <ScoreMetricRow
                key={metric.key}
                metricKey={metric.key}
                label={metric.label}
                details={details}
              />
            ))}
          </div>

          <div className='grid gap-2 border-t px-3 py-2 text-xs sm:grid-cols-3'>
            <div>
              <span className='text-muted-foreground block'>业务得分</span>
              <strong className='font-mono tabular-nums'>
                {formatScore(details.business_score)}
              </strong>
              <span className='text-muted-foreground mt-1 block break-words'>
                {businessCalculation}
              </span>
            </div>
            <div>
              <span className='text-muted-foreground block'>稳定性数据</span>
              <strong className='font-mono tabular-nums'>
                {formatScore(details.stability.raw_score)}
              </strong>
              <span className='text-muted-foreground ml-2'>
                {details.inputs.stability.sample_count} 个样本 · 配置权重{' '}
                {formatPercent(details.stability.configured_weight_percent)} ·{' '}
                {stabilityState}
              </span>
            </div>
            <div>
              <span className='text-muted-foreground block'>最终得分</span>
              <strong className='font-mono tabular-nums'>
                {formatScore(details.final_score)}
              </strong>
              <span className='text-muted-foreground ml-2'>
                {details.stability.applied
                  ? `业务贡献 ${formatScore(details.stability.business_contribution)} + 稳定性贡献 ${formatScore(details.stability.contribution)}`
                  : '由业务得分直接得到'}
              </span>
            </div>
            <p className='text-muted-foreground break-words sm:col-span-3'>
              计算：{finalCalculation}
            </p>
          </div>

          {props.showDecision !== false ? (
            <div className='border-t px-3 py-2 text-xs'>
              <div className='flex flex-wrap gap-x-4 gap-y-1 tabular-nums'>
                <span>
                  <span className='text-muted-foreground'>原主渠道 </span>
                  {formatChannelReference(
                    details.decision.current_primary_channel_id,
                    props.channelNameById
                  )}
                </span>
                <span>
                  <span className='text-muted-foreground'>评分第一 </span>
                  {formatChannelReference(
                    details.decision.raw_winner_channel_id,
                    props.channelNameById
                  )}
                </span>
                <span>
                  <span className='text-muted-foreground'>实际主渠道 </span>
                  {formatChannelReference(
                    details.decision.actual_primary_channel_id,
                    props.channelNameById
                  )}
                </span>
                {details.decision.manual_primary_channel_id > 0 ? (
                  <span>
                    <span className='text-muted-foreground'>管理员固定 </span>
                    {formatChannelReference(
                      details.decision.manual_primary_channel_id,
                      props.channelNameById
                    )}
                  </span>
                ) : null}
                <span>
                  <span className='text-muted-foreground'>切换分差 </span>
                  {formatPercent(details.decision.switch_threshold_percent)}
                </span>
                <span>
                  <span className='text-muted-foreground'>主渠道目标流量 </span>
                  {formatPercent(details.decision.primary_traffic_percent)}
                </span>
                {details.decision.base_rank > 0 ? (
                  <span>
                    <span className='text-muted-foreground'>基础排名 </span>第{' '}
                    {details.decision.base_rank} 名 · P
                    {details.decision.base_priority} / W
                    {details.decision.base_weight}
                  </span>
                ) : null}
                <span>
                  <span className='text-muted-foreground'>当前应用 </span>P
                  {details.decision.applied_priority} / W
                  {details.decision.applied_weight}
                </span>
                {details.decision.actual_highest_priority >= 0 ? (
                  <span>
                    <span className='text-muted-foreground'>实际最高层 </span>P
                    {details.decision.actual_highest_priority} ·{' '}
                    {actualTopLayerChannels || '渠道 -'}
                  </span>
                ) : null}
                {temporaryTrafficLabel ? (
                  <span>
                    <span className='text-muted-foreground'>临时流量 </span>
                    {temporaryTrafficLabel}{' '}
                    {formatPercent(
                      details.decision.temporary_traffic_target_percent
                    )}
                  </span>
                ) : null}
              </div>
              <div className='text-muted-foreground mt-2 grid gap-1 break-words'>
                <p>
                  <span className='text-foreground font-medium'>
                    选择原因：
                  </span>
                  {details.decision.selection_reason ||
                    details.decision.reason ||
                    '未记录选择原因'}
                </p>
                <p>
                  <span className='text-foreground font-medium'>
                    调整原因：
                  </span>
                  {details.decision.adjustment_reason || '未记录调整原因'}
                </p>
              </div>
            </div>
          ) : null}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
