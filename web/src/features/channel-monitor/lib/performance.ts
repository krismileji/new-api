import type {
  ChannelMonitorChannelPerformance,
  ChannelMonitorPerformanceMetric,
} from '../types'

export function aggregateChannelMonitorPerformanceByChannel(
  metrics: ChannelMonitorPerformanceMetric[]
): Map<number, ChannelMonitorChannelPerformance> {
  type PerformanceAggregate = {
    sampleCount: number
    firstTokenSampleCount: number
    tpsSampleCount: number
    firstTokenTotalMs: number
    tpsOutputTokens: number
    tpsGenerationDurationMs: number
    lastUsedTime: number
  }

  const aggregates = new Map<number, PerformanceAggregate>()
  for (const metric of metrics) {
    const aggregate = aggregates.get(metric.channel_id) ?? {
      sampleCount: 0,
      firstTokenSampleCount: 0,
      tpsSampleCount: 0,
      firstTokenTotalMs: 0,
      tpsOutputTokens: 0,
      tpsGenerationDurationMs: 0,
      lastUsedTime: 0,
    }
    aggregate.sampleCount += metric.sample_count
    if (
      metric.average_first_token_ms != null &&
      metric.first_token_sample_count > 0
    ) {
      aggregate.firstTokenSampleCount += metric.first_token_sample_count
      aggregate.firstTokenTotalMs +=
        metric.average_first_token_ms * metric.first_token_sample_count
    }
    if (
      metric.tps_sample_count > 0 &&
      metric.tps_output_tokens > 0 &&
      metric.tps_generation_duration_ms > 0
    ) {
      aggregate.tpsSampleCount += metric.tps_sample_count
      aggregate.tpsOutputTokens += metric.tps_output_tokens
      aggregate.tpsGenerationDurationMs += metric.tps_generation_duration_ms
    }
    aggregate.lastUsedTime = Math.max(
      aggregate.lastUsedTime,
      metric.last_used_time
    )
    aggregates.set(metric.channel_id, aggregate)
  }

  const result = new Map<number, ChannelMonitorChannelPerformance>()
  for (const [channelId, aggregate] of aggregates) {
    result.set(channelId, {
      sample_count: aggregate.sampleCount,
      first_token_sample_count: aggregate.firstTokenSampleCount,
      tps_sample_count: aggregate.tpsSampleCount,
      average_first_token_ms:
        aggregate.firstTokenSampleCount > 0
          ? aggregate.firstTokenTotalMs / aggregate.firstTokenSampleCount
          : null,
      average_tps:
        aggregate.tpsOutputTokens > 0 && aggregate.tpsGenerationDurationMs > 0
          ? aggregate.tpsOutputTokens /
            (aggregate.tpsGenerationDurationMs / 1000)
          : null,
      last_used_time: aggregate.lastUsedTime,
    })
  }
  return result
}
