import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'

import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import type { ChannelMonitorAnalyticsResponse } from '../types-analytics'
import { ChannelMonitorAnalyticsCoverage } from './channel-monitor-analytics-coverage'
import { ChannelMonitorProfitNotice } from './channel-monitor-profit'

export function ChannelMonitorProfitStatus(props: {
  response: ChannelMonitorAnalyticsResponse
  today: string
  refreshing: boolean
  onRefresh: () => Promise<void>
  onSelectCompleteDates: (from: string) => void
}) {
  const summary = props.response.scope_summary
  const reasons = props.response.coverage.reasons
  const historyMissing = reasons.includes('income_history_unavailable')
  const startedAt = summary.income_started_at
  let firstCompleteDate: string | undefined
  if (startedAt) {
    const startedDate = formatChannelMonitorBeijingDate(
      new Date(startedAt * 1000)
    )
    const midnight = new Date(`${startedDate}T00:00:00+08:00`).getTime()
    firstCompleteDate = formatChannelMonitorBeijingDate(
      new Date(midnight + (startedAt * 1000 > midnight ? 86_400_000 : 0))
    )
  }
  const earliestDate = formatChannelMonitorBeijingDate(
    new Date(
      new Date(`${props.today}T00:00:00+08:00`).getTime() - 89 * 86_400_000
    )
  )
  const selectableDate =
    firstCompleteDate && firstCompleteDate > earliestDate
      ? firstCompleteDate
      : earliestDate

  return (
    <section aria-label='利润统计核对' className='space-y-2'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <ChannelMonitorProfitNotice
          summary={summary}
          coverage={props.response.coverage}
        />
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={props.refreshing}
            onClick={() => void props.onRefresh()}
          >
            {props.refreshing ? '正在核对…' : '刷新核对'}
          </Button>
          {historyMissing &&
          firstCompleteDate &&
          firstCompleteDate <= props.today ? (
            <Button
              variant='outline'
              size='sm'
              onClick={() => props.onSelectCompleteDates(selectableDate)}
            >
              仅看完整日期
            </Button>
          ) : null}
        </div>
      </div>
      <Collapsible>
        <CollapsibleTrigger className='cursor-pointer rounded-sm text-xs underline-offset-4 hover:underline focus-visible:outline-2'>
          统计说明
        </CollapsibleTrigger>
        <CollapsibleContent className='text-muted-foreground mt-2 space-y-2 text-xs'>
          <p>
            账面毛利 = 已确认用户扣费 − 已结算渠道成本（含探测和模型检测）。
            待确认收入与未解析成本暂未计入，补账后利润和利润率可能变化，暂估负数不代表已确认亏损。
          </p>
          <p>
            平台按 1:1 记账：用户扣费 7，收入也记 7，不乘美元展示汇率。
            订阅为名义消耗；退款、补扣修正原记录。
          </p>
          <ChannelMonitorAnalyticsCoverage
            coverage={props.response.coverage}
            scope='利润'
          />
          {historyMissing ? (
            <>
              <p>
                收入只记录启用后的扣费，成本可能包含启用前的支出，两者不能直接比较。
              </p>
              <p>历史缺失不会通过等待或刷新补齐。</p>
              {firstCompleteDate ? (
                <p>
                  首个完整统计日为 {firstCompleteDate}
                  （北京时间）；当日仍需等待收入和成本正常入账。
                </p>
              ) : null}
            </>
          ) : null}
          {reasons.includes('income_recording_gap') ? (
            <p>
              收入或成本曾写入失败。请核对服务端结算错误及缺失记录，重启不会消除这个缺口。
            </p>
          ) : null}
          {reasons.includes('profit_cost_not_durable') ? (
            <p>
              请启用可靠成本记录并重启相关节点，再核对停用期间的记录是否完整。
            </p>
          ) : null}
          <p>收入和成本完整入账后，自动取消「暂估」标记。</p>
        </CollapsibleContent>
      </Collapsible>
    </section>
  )
}
