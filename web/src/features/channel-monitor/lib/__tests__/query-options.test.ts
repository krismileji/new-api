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
  CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS,
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
  getChannelMonitorOverviewQueryOptions,
  getChannelMonitorPerformanceQueryOptions,
  getChannelMonitorSmartScheduleQueryOptions,
  getChannelStatusProbeHistoryLatestExecutionKey,
  isChannelMonitorPerformanceQueryActive,
  refetchChannelMonitorQueries,
} from '../query-options'

describe('channel monitor query policy', () => {
  test('polls active operations and stops after they become terminal', () => {
    assert.equal(
      getChannelMonitorActiveRefetchInterval(true),
      CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS
    )
    assert.equal(getChannelMonitorActiveRefetchInterval(false), false)
  })

  test('keeps overview and performance data manual-refresh only', () => {
    const overviewOptions = getChannelMonitorOverviewQueryOptions()
    const performanceOptions = getChannelMonitorPerformanceQueryOptions(
      15,
      'manual'
    )

    assert.equal(overviewOptions.refetchInterval, false)
    assert.equal(performanceOptions.refetchInterval, false)
    assert.equal(overviewOptions.refetchOnWindowFocus, false)
    assert.equal(performanceOptions.refetchOnWindowFocus, false)
    assert.equal(overviewOptions.refetchOnReconnect, false)
    assert.equal(performanceOptions.refetchOnReconnect, false)
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
          data_cutoff_at: 1,
          processed_at: 1,
          event_watermark: 1,
          queue_depth: 0,
          realtime_degraded: false,
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
    assert.equal(options.staleTime, Number.POSITIVE_INFINITY)
    assert.equal(options.refetchInterval, false)
  })

  test('separates manual and smart schedule results with the same range', () => {
    const manual = getChannelMonitorPerformanceQueryOptions(60, 'manual')
    const smart = getChannelMonitorPerformanceQueryOptions(60, 'smart_schedule')

    assert.notDeepEqual(manual.queryKey, smart.queryKey)
  })

  test('pauses performance polling while the status probe view is active', () => {
    const options = getChannelMonitorPerformanceQueryOptions(
      60,
      'manual',
      false
    )

    assert.equal(options.enabled, false)
    assert.equal(options.refetchInterval, false)
  })

  test('skips manual performance refresh while the status probe view is active', () => {
    assert.equal(isChannelMonitorPerformanceQueryActive('status-probe'), false)
    assert.equal(isChannelMonitorPerformanceQueryActive('channels'), true)
  })

  test('only the first history page follows the latest execution id', () => {
    assert.equal(getChannelStatusProbeHistoryLatestExecutionKey(1, 42), 42)
    assert.equal(getChannelStatusProbeHistoryLatestExecutionKey(2, 42), 0)
  })

  test('shared manual policy disables interval, focus, and reconnect refreshes', () => {
    assert.equal(
      CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.refetchInterval,
      false
    )
    assert.equal(
      CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.refetchOnWindowFocus,
      false
    )
    assert.equal(
      CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.refetchOnReconnect,
      false
    )
  })

  test('manual refresh refetches every channel monitor query prefix', async () => {
    const queryKeys = [
      ['channel-monitor'],
      ['channel-monitor-performance'],
      ['channel-monitor-smart-schedule-executions'],
      ['channel-monitor-task-history'],
      ['channel-monitor-success-detail'],
      ['channel-monitor-history'],
      ['channel-monitor-available-groups'],
    ] as const
    let requestCount = 0
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    await Promise.all(
      queryKeys.map((queryKey) =>
        queryClient.fetchQuery({
          queryKey,
          queryFn: async () => {
            requestCount += 1
            return queryKey[0]
          },
        })
      )
    )

    await refetchChannelMonitorQueries(queryClient)

    assert.equal(requestCount, queryKeys.length * 2)
  })

  test('keeps lightweight schedule summaries and metric details manual-refresh only', () => {
    const summary = getChannelMonitorSmartScheduleQueryOptions(false)
    const metrics = getChannelMonitorSmartScheduleQueryOptions(true)

    assert.notDeepEqual(summary.queryKey, metrics.queryKey)
    assert.equal(summary.refetchInterval, false)
    assert.equal(metrics.refetchInterval, false)
    assert.equal(summary.staleTime, Number.POSITIVE_INFINITY)
    assert.equal(metrics.staleTime, Number.POSITIVE_INFINITY)
    assert.equal(summary.refetchOnWindowFocus, false)
    assert.equal(metrics.refetchOnWindowFocus, false)
  })

  test('does not refresh schedule routes on mount or window focus', () => {
    const options = getChannelMonitorSmartScheduleQueryOptions()

    assert.equal(options.refetchOnMount, false)
    assert.equal(options.refetchOnWindowFocus, false)
  })
})
