import { Button } from '@/components/ui/button'
import { TableCell } from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { useChannelMonitorTodayProfit } from '../hooks/use-channel-monitor-profit'
import { formatProfitMoney, formatProfitRate } from '../lib/profit-format'
import type { ChannelMonitorAnalyticsSummary } from '../types-analytics'

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
  )
}
