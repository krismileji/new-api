import { formatUseTime } from '@/lib/format'

import type { ChannelMonitorAnalyticsSummary } from '../types-analytics'
import {
  ChannelMonitorFirstTokenValue,
  ChannelMonitorTPSValue,
} from './channel-monitor-performance-value'

export function ChannelMonitorAnalyticsPerformanceMeasurement(props: {
  metric: 'first_token' | 'tps'
  summary: ChannelMonitorAnalyticsSummary
}) {
  const firstToken = props.metric === 'first_token'
  const samples = firstToken
    ? (props.summary.first_token_sample_count ?? 0)
    : (props.summary.tps_sample_count ?? 0)
  const value = firstToken
    ? props.summary.average_first_token_ms
    : props.summary.average_tps
  const measured = samples > 0 && value != null && Number.isFinite(value)
  let measurement = <span className='text-muted-foreground'>-</span>
  if (measured) {
    measurement = firstToken ? (
      <ChannelMonitorFirstTokenValue value={value} />
    ) : (
      <ChannelMonitorTPSValue value={value} />
    )
  }
  return (
    <>
      {measurement}
      <span className='text-muted-foreground block text-xs font-normal'>
        {measured ? `${samples.toLocaleString()} 个有效样本` : '暂无有效样本'}
      </span>
    </>
  )
}

export function ChannelMonitorAnalyticsPerformanceOutput(props: {
  summary: ChannelMonitorAnalyticsSummary
}) {
  const tokens = props.summary.tps_output_tokens ?? 0
  const duration = props.summary.tps_generation_duration_ms ?? 0
  return (
    <>
      <span>{tokens.toLocaleString()} Token</span>
      <span className='text-muted-foreground block text-xs font-normal'>
        累计生成 {formatUseTime(duration / 1000)}
      </span>
    </>
  )
}
