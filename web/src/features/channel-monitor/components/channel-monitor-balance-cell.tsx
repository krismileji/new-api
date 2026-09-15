import { Badge } from '@/components/ui/badge'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

import type { ChannelMonitorBalanceEstimate } from '../types'

const balanceFormatter = new Intl.NumberFormat(undefined, {
  minimumFractionDigits: 0,
  maximumFractionDigits: 6,
})

type ChannelMonitorBalanceCellProps = {
  balance: number
  enabled: boolean
  warning?: number | null
  error?: string
  estimate?: ChannelMonitorBalanceEstimate
}

export function ChannelMonitorBalanceCell(
  props: ChannelMonitorBalanceCellProps
) {
  const warning =
    props.enabled && props.warning != null && props.balance < props.warning
  const estimate = props.estimate

  return (
    <div className='flex flex-col items-start gap-1'>
      <div className='flex items-center gap-1.5 whitespace-nowrap'>
        <span className='text-muted-foreground text-xs'>上游</span>
        <span
          aria-label='上游余额'
          className={cn(
            'font-mono font-semibold',
            !props.enabled && 'text-muted-foreground',
            warning && 'text-destructive'
          )}
        >
          {balanceFormatter.format(props.balance)}
        </span>
        {warning ? <Badge variant='destructive'>低于预警值</Badge> : null}
      </div>
      {props.error ? (
        <span className='text-warning text-xs' title={props.error}>
          更新失败
        </span>
      ) : null}
      {props.enabled && !estimate?.available ? (
        <span
          className='text-muted-foreground text-xs'
          title={estimate?.reason}
        >
          预估不可用
        </span>
      ) : null}
      {props.enabled && estimate?.available ? (
        <Collapsible className='max-w-72 text-xs'>
          <CollapsibleTrigger className='cursor-pointer rounded-sm whitespace-nowrap underline-offset-4 hover:underline focus-visible:outline-2'>
            估算可用{' '}
            <span aria-label='估算可用余额' className='font-mono font-medium'>
              {balanceFormatter.format(estimate.estimated_balance)}
            </span>
          </CollapsibleTrigger>
          <CollapsibleContent className='text-muted-foreground mt-1 space-y-1 whitespace-normal'>
            <p>
              已完成消费：
              {balanceFormatter.format(estimate.completed_consumption)}
            </p>
            <p>
              进行中预估：
              {balanceFormatter.format(estimate.in_flight_consumption)}（
              {estimate.in_flight_count} 笔）
            </p>
            <p>
              按近期均值 {estimate.average_count} 笔，按请求预算{' '}
              {estimate.budget_count} 笔
            </p>
            {estimate.last_estimate_model ? (
              <p className='break-all'>
                最近一次预估参考：{estimate.last_estimate_model}，
                {estimate.last_sample_count} 笔样本
                <br />
                {estimate.last_estimate_source === 'unknown'
                  ? '单笔费用未确认'
                  : `${estimate.last_estimate_source === 'average' ? '平均单笔费用' : '单笔请求预算'}：${balanceFormatter.format(estimate.last_estimate_amount)}`}
              </p>
            ) : null}
            {estimate.uncertain_consumption > 0 ? (
              <p>
                查询期间待确认：
                {balanceFormatter.format(estimate.uncertain_consumption)}
              </p>
            ) : null}
            {estimate.unknown_count > 0 ? (
              <p>未确认请求：{estimate.unknown_count} 笔</p>
            ) : null}
            <p>
              均值取同渠道、同模型近 30 分钟最多 100 笔费用，至少 5 笔后使用。
            </p>
            <p>预估仅用于余额监控；请求完成后校正，页面刷新时读取最新值。</p>
            {estimate.reason ? <p>{estimate.reason}</p> : null}
          </CollapsibleContent>
        </Collapsible>
      ) : null}
      {props.enabled && estimate?.available && !estimate.complete ? (
        <Badge variant='outline'>预估不完整</Badge>
      ) : null}
    </div>
  )
}
