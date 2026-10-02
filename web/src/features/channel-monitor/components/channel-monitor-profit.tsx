import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TableCell } from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { useChannelMonitorTodayProfit } from '../hooks/use-channel-monitor-profit'
import { formatProfitMoney, formatProfitRate } from '../lib/profit-format'
import type { ChannelMonitorAnalyticsSummary } from '../types-analytics'

export function ChannelMonitorProfitOverview(props: {
  summary?: ChannelMonitorAnalyticsSummary
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
            className='text-base font-normal'
          />
        </span>
      </div>
      <span className='text-muted-foreground text-xs font-normal'>
        扣费 {formatProfitMoney(props.summary?.income_nano_cny)} · 利润率{' '}
        {formatProfitRate(props.summary)}
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

export function ChannelMonitorProfitValue({
  summary,
  className,
}: {
  summary?: ChannelMonitorAnalyticsSummary
  className?: string
}) {
  const confirmed = summary?.profit_confirmed === true
  return (
    <span
      className={cn(
        'font-mono tabular-nums',
        className,
        confirmed && (summary?.profit_nano_cny ?? 0) < 0 && 'text-destructive'
      )}
    >
      {formatProfitMoney(confirmed ? summary.profit_nano_cny : undefined)}
      {summary && !confirmed ? (
        <span className='text-muted-foreground block font-sans text-xs'>
          利润待确认
        </span>
      ) : null}
    </span>
  )
}

export function ChannelMonitorProfitCells({
  summary,
}: {
  summary: ChannelMonitorAnalyticsSummary
}) {
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
        <ChannelMonitorProfitValue summary={summary} />
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatProfitRate(summary)}
      </TableCell>
    </>
  )
}

export function ChannelMonitorProfitCell(props: {
  channelId: number
  channelName: string
  summary?: ChannelMonitorAnalyticsSummary
  needsQuery?: boolean
  onOpen: () => void
}) {
  const query = useChannelMonitorTodayProfit(
    props.channelId,
    props.needsQuery === true
  )
  const summary = props.summary ?? query.data?.data.scope_summary
  return (
    <div className='flex flex-col items-start'>
      <Button
        variant='ghost'
        size='sm'
        className='h-auto flex-col items-start px-2 py-1'
        aria-label={`查看渠道 ${props.channelName} 的利润`}
        onClick={props.onOpen}
      >
        <ChannelMonitorProfitValue summary={summary} />
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
