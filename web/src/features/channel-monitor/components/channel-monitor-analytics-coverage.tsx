import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

import type { ChannelMonitorAnalyticsResponse } from '../types-analytics'

const coverageReasons: Record<string, string> = {
  cost_attribution_incomplete: '历史或未归属成本明细尚未与渠道日账对平',
  cost_detail_unavailable: '成本明细尚未准备完成',
  cost_projection_pending: '部分已记录成本正在更新到统计中',
  cost_projection_unavailable: '成本统计暂时无法更新',
  daily_checkpoint_missing: '部分日期缺少持久化检查点，无法确认统计完整性',
  daily_legacy_attribution_incomplete:
    '旧统计缺少完整归属，当前只能展示可确认的明细',
  daily_replay_incomplete: '历史数据恢复尚未完整完成',
  data_source_unavailable: '统计数据源暂不可用',
  processing_lag: '统计尚未覆盖完整时间段',
  window_start_uncovered: '所选范围的起始日期尚未被统计覆盖',
  redis_unavailable: '实时统计暂不可用',
  consumer_stopped: '实时统计处理已暂停',
  event_backlog: '部分请求记录仍在处理',
  publish_failed: '部分请求记录未能写入统计',
  samples_dropped: '部分统计样本缺失',
}

export function ChannelMonitorAnalyticsCoverage(props: {
  coverage: ChannelMonitorAnalyticsResponse['coverage'] | undefined
  scope?: string
}) {
  const coverage = props.coverage
  if (!coverage || coverage.status === 'complete') return null
  const unavailable = coverage.status === 'unavailable'
  const reasons = [
    ...new Set(
      coverage.reasons.map(
        (reason) => coverageReasons[reason] ?? '部分统计数据尚未就绪'
      )
    ),
  ]
  return (
    <Alert>
      <AlertTitle>
        {`${props.scope ?? '统计'}${unavailable ? '暂不可用' : '覆盖不完整'}`}
      </AlertTitle>
      <AlertDescription>
        {reasons.join('；') || '统计数据仍在处理'}。
        {unavailable
          ? '当前状态不代表实际调用为零。'
          : '当前仅展示已记录的数据。'}
      </AlertDescription>
    </Alert>
  )
}
