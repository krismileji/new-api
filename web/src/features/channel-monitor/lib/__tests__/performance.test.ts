import assert from 'node:assert/strict'

import { test } from 'vitest'

import { aggregateChannelMonitorPerformanceByChannel } from '../performance'

test('aggregates channel TPS from total output tokens and generation time', () => {
  const result = aggregateChannelMonitorPerformanceByChannel([
    {
      channel_id: 7,
      model_name: 'model-a',
      sample_count: 1,
      first_token_sample_count: 0,
      tps_sample_count: 1,
      tps_output_tokens: 1,
      tps_generation_duration_ms: 10,
      average_first_token_ms: null,
      average_tps: 100,
      latest_first_token_ms: null,
      latest_tps: 100,
      last_used_time: 1,
    },
    {
      channel_id: 7,
      model_name: 'model-b',
      sample_count: 1,
      first_token_sample_count: 0,
      tps_sample_count: 1,
      tps_output_tokens: 100,
      tps_generation_duration_ms: 10_000,
      average_first_token_ms: null,
      average_tps: 10,
      latest_first_token_ms: null,
      latest_tps: 10,
      last_used_time: 2,
    },
  ])

  const channel = result.get(7)
  assert.ok(channel)
  assert.equal(channel.tps_sample_count, 2)
  assert.equal(channel.average_tps, 101 / 10.01)
})
