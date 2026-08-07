/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { QueryClient } from '@tanstack/react-query'

import type {
  ChannelMonitorApiResponse,
  ChannelMonitorPerformanceResult,
} from '../../types'
import {
  getChannelMonitorOverviewQueryOptions,
  getChannelMonitorPerformanceQueryOptions,
  getChannelMonitorSmartScheduleQueryOptions,
} from '../query-options'

describe('channel monitor query policy', () => {
  test('refreshes overview and performance data every minute', () => {
    const overviewOptions = getChannelMonitorOverviewQueryOptions()
    const performanceOptions = getChannelMonitorPerformanceQueryOptions(
      15,
      'manual'
    )

    assert.equal(overviewOptions.refetchInterval, 60_000)
    assert.equal(performanceOptions.refetchInterval, 60_000)
  })

  test('deduplicates fresh reads while preserving an explicit refresh', async () => {
    let requestCount = 0
    const options = getChannelMonitorPerformanceQueryOptions(15, 'manual')
    const response: ChannelMonitorApiResponse<ChannelMonitorPerformanceResult> =
      {
        success: true,
        message: '',
        data: {
          range_minutes: 15,
          range_source: 'manual',
          generated_at: 1,
          metric_coverage: {
            aggregation_enabled: true,
            aggregated_from: 0,
            aggregated_through: 0,
            window_start: 0,
            window_complete: false,
          },
          items: [],
          success_metrics_available: true,
          success_items: [],
          group_success_items: [],
        },
      }
    const queryFn: NonNullable<typeof options.queryFn> = async () => {
      requestCount += 1
      return response
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await queryClient.fetchQuery({ ...options, queryFn })
    await queryClient.fetchQuery({ ...options, queryFn })
    assert.equal(requestCount, 1)

    await queryClient.refetchQueries({ queryKey: options.queryKey })
    assert.equal(requestCount, 2)
    assert.equal(options.staleTime, 60_000)
    assert.equal(options.refetchInterval, 60_000)
  })

  test('separates manual and smart schedule results with the same range', () => {
    const manual = getChannelMonitorPerformanceQueryOptions(60, 'manual')
    const smart = getChannelMonitorPerformanceQueryOptions(60, 'smart_schedule')

    assert.notDeepEqual(manual.queryKey, smart.queryKey)
  })

  test('separates lightweight schedule summaries from metric details', () => {
    const summary = getChannelMonitorSmartScheduleQueryOptions(false)
    const metrics = getChannelMonitorSmartScheduleQueryOptions(true)

    assert.notDeepEqual(summary.queryKey, metrics.queryKey)
    assert.equal(summary.refetchInterval, 60_000)
    assert.equal(metrics.refetchInterval, 60_000)
  })

  test('revalidates schedule routes after returning from channel configuration', () => {
    const options = getChannelMonitorSmartScheduleQueryOptions()

    assert.equal(options.refetchOnMount, 'always')
    assert.equal(options.refetchOnWindowFocus, 'always')
  })
})
