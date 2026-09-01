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

import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { describe, test } from 'vitest'

import type {
  ChannelMonitorApiResponse,
  ChannelMonitorPerformanceResult,
} from '../../types'
import {
  CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS,
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
  getChannelMonitorConcurrencyQueryOptions,
  getChannelMonitorManualRefreshScopeKey,
  getChannelMonitorOverviewQueryOptions,
  getChannelMonitorPerformanceQueryOptions,
  getChannelMonitorSmartScheduleQueryOptions,
  getChannelStatusProbeHistoryLatestExecutionKey,
  isChannelMonitorPerformanceQueryActive,
  refetchChannelMonitorQueries,
  shouldRefreshChannelMonitorViewOnEnter,
  shouldCoalesceChannelMonitorManualRefresh,
} from '../query-options'

describe('channel monitor query policy', () => {
  test('refreshes the selected view only after switching to it', () => {
    assert.equal(shouldRefreshChannelMonitorViewOnEnter(null, 'channels'), false)
    assert.equal(
      shouldRefreshChannelMonitorViewOnEnter('channels', 'status-probe'),
      true
    )
    assert.equal(
      shouldRefreshChannelMonitorViewOnEnter('channels', 'channels'),
      false
    )
  })

  test('only returns a one-second interval while a monitoring task is active', () => {
    assert.equal(CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS, 1000)
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

  test('keeps concurrency data current and manual-refresh only', () => {
    const enabled = getChannelMonitorConcurrencyQueryOptions()
    const disabled = getChannelMonitorConcurrencyQueryOptions(false)

    assert.equal(enabled.enabled, true)
    assert.equal(enabled.staleTime, 0)
    assert.equal(enabled.refetchInterval, false)
    assert.equal(enabled.refetchOnMount, 'always')
    assert.equal(disabled.enabled, false)
    assert.deepEqual(enabled.queryKey, ['channel-monitor', 'concurrency'])
  })

  test('deduplicates in-flight reads while preserving an explicit refresh', async () => {
    let requestCount = 0
    let resolveRequest: (() => void) | undefined
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
    const queryFn: NonNullable<typeof options.queryFn> = () => {
      requestCount += 1
      if (requestCount > 1) return Promise.resolve(response)
      return new Promise((resolve) => {
        resolveRequest = () => resolve(response)
      })
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    const firstRequest = queryClient.fetchQuery({ ...options, queryFn })
    const secondRequest = queryClient.fetchQuery({ ...options, queryFn })
    await Promise.resolve()
    assert.equal(requestCount, 1)
    resolveRequest?.()
    await Promise.all([firstRequest, secondRequest])

    await queryClient.refetchQueries({ queryKey: options.queryKey })
    assert.equal(requestCount, 2)
    assert.equal(options.staleTime, 0)
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

  test('skips manual performance refresh while a dedicated monitor view is active', () => {
    assert.equal(isChannelMonitorPerformanceQueryActive('status-probe'), false)
    assert.equal(isChannelMonitorPerformanceQueryActive('model-detection'), false)
    assert.equal(isChannelMonitorPerformanceQueryActive('channels'), true)
  })

  test('only the first history page follows the latest execution id', () => {
    assert.equal(getChannelStatusProbeHistoryLatestExecutionKey(1, 42), 42)
    assert.equal(getChannelStatusProbeHistoryLatestExecutionKey(2, 42), 0)
  })

  test('shared manual policy disables retained snapshots and automatic retries', () => {
    assert.equal(CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.gcTime, 0)
    assert.equal(CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.retry, false)
    assert.equal(
      CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.refetchInterval,
      false
    )
    assert.equal(
      CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS.refetchIntervalInBackground,
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

  test('manual refresh only refetches the active view queries', async () => {
    const queryKeys = [
      ['channel-monitor'],
      ['channel-monitor-performance', 'manual', 15],
      ['channel-monitor', 'cost', 'summary', 2],
      ['channel-monitor', 'success', 'today'],
      ['channel-monitor-task-history', 'ratio', 1, 25],
      ['channel-monitor-smart-schedule-executions', 1],
    ] as const
    let requestCount = 0
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    const observers = queryKeys.map(
      (queryKey) =>
        new QueryObserver(queryClient, {
          queryKey,
          queryFn: async () => {
            requestCount += 1
            return queryKey[0]
          },
        })
    )
    const unsubscribers = observers.map((observer) =>
      observer.subscribe(() => undefined)
    )
    await Promise.all(observers.map((observer) => observer.refetch()))

    await refetchChannelMonitorQueries(queryClient, { view: 'groups' })

    assert.equal(requestCount, queryKeys.length + 4)
    unsubscribers.forEach((unsubscribe) => unsubscribe())
  })

  test('manual refresh preserves the active result while fetching new data', async () => {
    const queryKey = ['channel-monitor'] as const
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(queryKey, 'previous result')
    let resolveRequest: ((value: string) => void) | undefined
    const observer = new QueryObserver(queryClient, {
      queryKey,
      queryFn: () =>
        new Promise<string>((resolve) => {
          resolveRequest = resolve
        }),
      staleTime: Infinity,
    })
    const unsubscribe = observer.subscribe(() => undefined)

    try {
      const refresh = refetchChannelMonitorQueries(queryClient, {
        view: 'groups',
      })
      await Promise.resolve()

      assert.equal(observer.getCurrentResult().data, 'previous result')
      assert.equal(observer.getCurrentResult().isLoading, false)
      resolveRequest?.('latest result')
      await refresh
      assert.equal(observer.getCurrentResult().data, 'latest result')
    } finally {
      unsubscribe()
    }
  })

  test('manual refresh includes only the active live view and open history dialog', async () => {
    const queries = [
      {
        name: 'status-overview',
        queryKey: ['channel-monitor', 'status-probe', { model: '' }],
      },
      {
        name: 'status-history',
        queryKey: ['channel-monitor', 'status-probe', 'history', 1],
      },
      {
        name: 'model-overview',
        queryKey: ['channel-monitor', 'model-detection', 'overview'],
      },
      {
        name: 'model-history',
        queryKey: ['channel-monitor', 'model-detection', 'history', 1],
      },
      {
        name: 'task-history',
        queryKey: ['channel-monitor-task-history', 'ratio', 1],
      },
      {
        name: 'schedule-history',
        queryKey: ['channel-monitor-smart-schedule-executions', 1],
      },
      { name: 'overview', queryKey: ['channel-monitor'] },
    ] as const
    const requestCounts = new Map<string, number>(
      queries.map((query) => [query.name, 0] as const)
    )
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    const observers = queries.map(
      (query) =>
        new QueryObserver(queryClient, {
          queryKey: query.queryKey,
          queryFn: async () => {
            const requestCount = requestCounts.get(query.name) ?? 0
            requestCounts.set(query.name, requestCount + 1)
            return query.name
          },
        })
    )
    const unsubscribers = observers.map((observer) =>
      observer.subscribe(() => undefined)
    )

    try {
      await Promise.all(observers.map((observer) => observer.refetch()))
      requestCounts.forEach((_, name) => requestCounts.set(name, 0))

      await refetchChannelMonitorQueries(queryClient, {
        view: 'status-probe',
        taskHistoryOpen: true,
      })
      assert.deepEqual(Object.fromEntries(requestCounts), {
        'status-overview': 1,
        'status-history': 1,
        'model-overview': 0,
        'model-history': 0,
        'task-history': 1,
        'schedule-history': 0,
        overview: 0,
      })

      requestCounts.forEach((_, name) => requestCounts.set(name, 0))
      await refetchChannelMonitorQueries(queryClient, {
        view: 'model-detection',
        smartScheduleHistoryOpen: true,
      })
      assert.deepEqual(Object.fromEntries(requestCounts), {
        'status-overview': 0,
        'status-history': 0,
        'model-overview': 1,
        'model-history': 1,
        'task-history': 0,
        'schedule-history': 1,
        overview: 0,
      })
    } finally {
      unsubscribers.forEach((unsubscribe) => unsubscribe())
    }
  })

  test('keeps manual refresh dedupe scoped to the active view and dialogs', () => {
    const channels = getChannelMonitorManualRefreshScopeKey({
      view: 'channels',
    })
    const groups = getChannelMonitorManualRefreshScopeKey({ view: 'groups' })
    const channelsWithHistory = getChannelMonitorManualRefreshScopeKey({
      view: 'channels',
      taskHistoryOpen: true,
    })

    assert.notEqual(channels, groups)
    assert.notEqual(channels, channelsWithHistory)
  })

  test('coalesces only an in-flight refresh for the same scope', () => {
    const currentScope = getChannelMonitorManualRefreshScopeKey({
      view: 'channels',
    })

    assert.equal(
      shouldCoalesceChannelMonitorManualRefresh({
        currentScope,
        previousScope: currentScope,
        inFlight: true,
      }),
      true
    )
    assert.equal(
      shouldCoalesceChannelMonitorManualRefresh({
        currentScope,
        previousScope: currentScope,
        inFlight: false,
      }),
      false
    )
    assert.equal(
      shouldCoalesceChannelMonitorManualRefresh({
        currentScope,
        previousScope: getChannelMonitorManualRefreshScopeKey({
          view: 'groups',
        }),
        inFlight: true,
      }),
      false
    )
  })

  test('keeps schedule summaries and metric details current and manual-refresh only', () => {
    const summary = getChannelMonitorSmartScheduleQueryOptions(false)
    const metrics = getChannelMonitorSmartScheduleQueryOptions(true)

    assert.notDeepEqual(summary.queryKey, metrics.queryKey)
    assert.equal(summary.refetchInterval, false)
    assert.equal(metrics.refetchInterval, false)
    assert.equal(summary.staleTime, 0)
    assert.equal(metrics.staleTime, 0)
    assert.equal(summary.refetchOnWindowFocus, false)
    assert.equal(metrics.refetchOnWindowFocus, false)
  })

  test('revalidates schedule routes on mount but not window focus', () => {
    const options = getChannelMonitorSmartScheduleQueryOptions()

    assert.equal(options.refetchOnMount, 'always')
    assert.equal(options.refetchOnWindowFocus, false)
  })
})
