import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TableCell } from '@/components/ui/table'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import { useChannelMonitorTodayProfit } from '../hooks/use-channel-monitor-profit'
import {
  formatProfitMoney,
  formatProfitRate,
  getProfitDisplayStatus,
} from '../lib/profit-format'
import type {
  ChannelMonitorAnalyticsResponse,
  ChannelMonitorAnalyticsSummary,
} from '../types-analytics'

export function ChannelMonitorProfitOverview(props: {
  summary?: ChannelMonitorAnalyticsSummary
  coverage?: ChannelMonitorAnalyticsResponse['coverage']
  loading: boolean
  failed: boolean
}) {
  if (props.loading && !props.summary) {
    return <Skeleton className='h-12 w-48' aria-label='成本与利润加载中' />
  }
  return (
    <div className='flex min-w-0 flex-col items-start gap-0.5'>
      <div className='flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5'>
        <span>{formatProfitMoney(props.summary?.cost_nano_cny)}</span>
        <span className='inline-flex items-baseline gap-1 text-base font-normal'>
          <span className='text-muted-foreground text-xs'>利润</span>
          <ChannelMonitorProfitValue
            summary={props.summary}
            coverage={props.coverage}
            className='text-base font-normal'
          />
        </span>
      </div>
      <span className='text-muted-foreground text-xs font-normal'>
        扣费 {formatProfitMoney(props.summary?.income_nano_cny)} · 利润率{' '}
        {formatProfitRate(props.summary, props.coverage)}
      </span>
      {props.failed ? (
        <span
          role='status'
          className='text-muted-foreground text-xs font-normal'
        >
          {props.summary ? '刷新失败，显示上次核对结果' : '成本与利润加载失败'}
        </span>
      ) : null}
    </div>
  )
}

export function ChannelMonitorProfitValue(props: {
  summary?: ChannelMonitorAnalyticsSummary
  coverage?: ChannelMonitorAnalyticsResponse['coverage']
  dayStart?: number
  className?: string
}) {
  const status = getProfitDisplayStatus(
    props.summary,
    props.coverage,
    props.dayStart
  )
  const available = status === 'confirmed' || status === 'estimated'
  return (
    <span
      className={cn(
        'font-mono tabular-nums',
        props.className,
        status === 'confirmed' &&
          (props.summary?.profit_nano_cny ?? 0) < 0 &&
          'text-destructive'
      )}
    >
      {formatProfitMoney(
        available ? props.summary?.profit_nano_cny : undefined
      )}
      {status === 'estimated' || status === 'history_unavailable' ? (
        <span className='text-muted-foreground block font-sans text-xs'>
          {status === 'estimated' ? '暂估' : '历史收入缺失'}
        </span>
      ) : null}
    </span>
  )
}

export function ChannelMonitorProfitCells(props: {
  summary: ChannelMonitorAnalyticsSummary
  coverage?: ChannelMonitorAnalyticsResponse['coverage']
  dayStart?: number
}) {
  const summary = props.summary
  return (
    <>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatProfitMoney(summary.income_nano_cny)}
        <span className='text-muted-foreground block text-xs'>
          钱包 {formatProfitMoney(summary.wallet_income_nano_cny)}
        </span>
        <span className='text-muted-foreground block text-xs'>
          订阅 {formatProfitMoney(summary.subscription_income_nano_cny)}
        </span>
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatProfitMoney(summary.cost_nano_cny)}
        <span className='text-muted-foreground block text-xs'>
          探测 {formatProfitMoney(summary.probe_cost_nano_cny)}
        </span>
        <span className='text-muted-foreground block text-xs'>
          检测 {formatProfitMoney(summary.model_detection_cost_nano_cny)}
        </span>
      </TableCell>
      <TableCell className='text-right'>
        <ChannelMonitorProfitValue
          summary={summary}
          coverage={props.coverage}
          dayStart={props.dayStart}
        />
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatProfitRate(summary, props.coverage, props.dayStart)}
      </TableCell>
    </>
  )
}

export function ChannelMonitorProfitCell(props: {
  channelId: number
  channelName: string
  summary?: ChannelMonitorAnalyticsSummary
  coverage?: ChannelMonitorAnalyticsResponse['coverage']
  needsQuery?: boolean
  onOpen: () => void
}) {
  const query = useChannelMonitorTodayProfit(
    props.channelId,
    props.needsQuery === true
  )
  const summary = props.summary ?? query.data?.data.scope_summary
  const coverage = props.summary ? props.coverage : query.data?.data.coverage
  return (
    <div className='flex flex-col items-start'>
      <Button
        variant='ghost'
        size='sm'
        className='h-auto flex-col items-start px-2 py-1'
        aria-label={`查看渠道 ${props.channelName} 的利润`}
        onClick={props.onOpen}
      >
        <ChannelMonitorProfitValue summary={summary} coverage={coverage} />
        <span className='text-muted-foreground text-xs'>
          扣费 {formatProfitMoney(summary?.income_nano_cny)}
        </span>
      </Button>
      {!props.summary && props.needsQuery && query.isError ? (
        <>
          <span role='status' className='text-muted-foreground px-2 text-xs'>
            {summary ? '刷新失败，显示上次核对结果' : '利润加载失败'}
          </span>
          <Button
            variant='ghost'
            size='sm'
            aria-label={`重试渠道 ${props.channelName} 的利润查询`}
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            重试
          </Button>
        </>
      ) : null}
    </div>
  )
}

export function ChannelMonitorProfitNotice(props: {
  summary?: ChannelMonitorAnalyticsSummary
  coverage?: ChannelMonitorAnalyticsResponse['coverage']
}) {
  if (!props.summary && !props.coverage) return null
  const reasons = props.coverage?.reasons ?? []
  const pendingIncomeCount = props.summary?.pending_income_count ?? 0
  const unresolvedCount = props.summary?.unresolved_count ?? 0
  const parts: string[] = []
  if (reasons.includes('income_history_unavailable')) parts.push('历史收入缺失')
  if (reasons.includes('profit_history_expired')) {
    parts.push('所选日期超出保留范围')
  }
  if (reasons.includes('cost_projection_pending')) parts.push('成本入账中')
  if (pendingIncomeCount > 0) {
    parts.push(
      `扣费/退款待确认 ${formatNumber(pendingIncomeCount, 'zh-CN')} 笔`
    )
  }
  if (unresolvedCount > 0) {
    parts.push(`未解析成本 ${formatNumber(unresolvedCount, 'zh-CN')} 笔`)
  }
  if (reasons.includes('income_recording_gap')) {
    parts.push('收入或成本记录有缺口')
  }
  if (reasons.includes('profit_cost_not_durable')) {
    parts.push('可靠成本记录已关闭')
  }
  if (reasons.includes('profit_cost_queue_unavailable')) {
    parts.push('成本队列暂不可用')
  }
  if (reasons.includes('cost_attribution_incomplete')) {
    parts.push('成本归属待核对')
  }
  if (props.coverage?.status === 'unavailable') parts.push('统计暂不可用')
  if (parts.length === 0) {
    if (getProfitDisplayStatus(props.summary, props.coverage) === 'confirmed') {
      return null
    }
    parts.push('统计尚未完整')
  }
  return (
    <p role='status' className='text-muted-foreground text-xs'>
      {parts.join(' · ')}
    </p>
  )
}
