import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import type { ChannelMonitorAnalyticsResponse } from '../types-analytics'
import { ChannelMonitorAnalyticsCoverage } from './channel-monitor-analytics-coverage'

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
      <ChannelMonitorAnalyticsCoverage
        coverage={props.response.coverage}
        scope='利润'
      />
      <Alert>
        <AlertTitle>
          {summary.profit_confirmed === true
            ? '当前范围利润已确认'
            : '当前范围暂不能判断盈亏'}
        </AlertTitle>
        <AlertDescription className='space-y-2'>
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
          <p>
            核对新请求：使用独立 API
            Key，在明细中记录调用前后的用户扣费，完成正常付费调用后点击“刷新核对”，
            将扣费增量与该请求消费日志的最终扣费按平台 1:1 口径核对，不再乘美元展示汇率，避免混入其他请求。探测和模型检测只有成本，不产生用户收入。
          </p>
          <p>
            收入增加仅能验证收入记录正在写入；利润确认还需要成本入账、解析完成且统计范围没有缺口。
          </p>
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
        </AlertDescription>
      </Alert>
    </section>
  )
}
