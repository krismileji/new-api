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
import { queryOptions, type QueryClient } from '@tanstack/react-query'

import {
  getChannelMonitorConcurrency,
  getChannelMonitorOverview,
  getChannelMonitorPerformance,
  getChannelMonitorSmartScheduleRoutes,
} from '../api'
import type {
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorPerformanceRangeSource,
} from '../types'

export const CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS = {
  refetchInterval: false,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
} as const

// Only the status-probe and model-detection views use the live page policy.
// Keep this interval here so the two views cannot drift apart.
export const CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS = 1000
export const CHANNEL_MONITOR_MANUAL_REFRESH_COALESCE_MS = 750

export function getChannelMonitorActiveRefetchInterval(active: boolean) {
  return active ? CHANNEL_MONITOR_ACTIVE_REFETCH_INTERVAL_MS : false
}

export function shouldRefreshChannelMonitorViewOnEnter(
  previousView: ChannelMonitorManualRefreshView | null,
  currentView: ChannelMonitorManualRefreshView
) {
  return previousView === null || previousView !== currentView
}

export const CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY = [
  'channel-monitor',
  'smart-schedule',
  'routes',
] as const

export const CHANNEL_MONITOR_CONCURRENCY_QUERY_KEY = [
  'channel-monitor',
  'concurrency',
] as const

export const CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY = [
  'channel-monitor-smart-schedule-executions',
] as const

export const CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY = [
  'channel-monitor-task-history',
] as const

export type ChannelMonitorManualRefreshView =
  | 'channels'
  | 'groups'
  | 'models'
  | 'status-probe'
  | 'model-detection'
  | 'smart-schedule'

export type ChannelMonitorManualRefreshScope = {
  view: ChannelMonitorManualRefreshView
  taskHistoryOpen?: boolean
  smartScheduleHistoryOpen?: boolean
}

export function getChannelMonitorManualRefreshScopeKey(
  scope: ChannelMonitorManualRefreshScope
) {
  return `${scope.view}:${scope.taskHistoryOpen ? 'task-history' : ''}:${scope.smartScheduleHistoryOpen ? 'smart-schedule-history' : ''}`
}

export function shouldCoalesceChannelMonitorManualRefresh(props: {
  currentScope: string
  previousScope: string | null
  currentTime: number
  previousRefreshAt: number
  inFlight: boolean
}) {
  return (
    props.currentScope === props.previousScope &&
    (props.inFlight ||
      props.currentTime - props.previousRefreshAt <
        CHANNEL_MONITOR_MANUAL_REFRESH_COALESCE_MS)
  )
}

type ChannelMonitorRefreshTarget = {
  queryKey: readonly unknown[]
  exact?: boolean
}

function getChannelMonitorManualRefreshTargets(
  scope: ChannelMonitorManualRefreshScope
): ChannelMonitorRefreshTarget[] {
  let targets: ChannelMonitorRefreshTarget[]

  switch (scope.view) {
    case 'channels':
      targets = [
        { queryKey: ['channel-monitor'], exact: true },
        { queryKey: CHANNEL_MONITOR_CONCURRENCY_QUERY_KEY, exact: true },
        { queryKey: ['channel-monitor-performance'] },
        { queryKey: ['channel-monitor', 'cost', 'summary', 2], exact: true },
        { queryKey: ['channel-monitor', 'success', 'today'], exact: true },
        {
          queryKey: [...CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY, 'summary'],
          exact: true,
        },
      ]
      break
    case 'groups':
    case 'models':
      targets = [
        { queryKey: ['channel-monitor'], exact: true },
        { queryKey: ['channel-monitor-performance'] },
        { queryKey: ['channel-monitor', 'cost', 'summary', 2], exact: true },
        { queryKey: ['channel-monitor', 'success', 'today'], exact: true },
      ]
      break
    case 'status-probe':
      targets = [{ queryKey: ['channel-monitor', 'status-probe'] }]
      break
    case 'model-detection':
      // The active overview, history sheet, and run detail queries all share
      // this prefix. Inactive sheets are excluded by the active query filter.
      targets = [
        {
          queryKey: ['channel-monitor', 'model-detection'],
        },
      ]
      break
    case 'smart-schedule':
      targets = [
        { queryKey: ['channel-monitor'], exact: true },
        { queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY },
        { queryKey: ['channel-monitor', 'cost', 'summary', 2], exact: true },
        { queryKey: ['channel-monitor', 'success', 'today'], exact: true },
      ]
      break
  }

  if (scope.taskHistoryOpen) {
    targets.push({ queryKey: CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY })
  }
  if (scope.smartScheduleHistoryOpen) {
    targets.push({
      queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY,
    })
  }

  return targets
}

export async function refetchChannelMonitorQueries(
  queryClient: QueryClient,
  scope: ChannelMonitorManualRefreshScope
) {
  await Promise.all(
    getChannelMonitorManualRefreshTargets(scope).map(({ queryKey, exact }) =>
      queryClient.refetchQueries(
        { queryKey, exact, type: 'active' },
        { cancelRefetch: false }
      )
    )
  )
}

export function getChannelMonitorOverviewQueryOptions() {
  return queryOptions({
    queryKey: ['channel-monitor'],
    queryFn: getChannelMonitorOverview,
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
}

export function getChannelMonitorConcurrencyQueryOptions(enabled = true) {
  return queryOptions({
    queryKey: CHANNEL_MONITOR_CONCURRENCY_QUERY_KEY,
    queryFn: getChannelMonitorConcurrency,
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
}

export function getChannelMonitorPerformanceQueryOptions(
  minutes: ChannelMonitorPerformanceRangeMinutes,
  source: ChannelMonitorPerformanceRangeSource,
  active = true
) {
  return queryOptions({
    queryKey: ['channel-monitor-performance', source, minutes],
    queryFn: () => getChannelMonitorPerformance(minutes),
    enabled: active,
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
}

export function isChannelMonitorPerformanceQueryActive(view: string) {
  return view !== 'status-probe'
}

export function getChannelStatusProbeHistoryLatestExecutionKey(
  page: number,
  latestExecutionId: number
) {
  return page === 1 ? latestExecutionId : 0
}

export function getChannelMonitorSmartScheduleQueryOptions(
  metrics: boolean = true
) {
  return queryOptions({
    queryKey: [
      ...CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
      metrics ? 'metrics' : 'summary',
    ],
    queryFn: () => getChannelMonitorSmartScheduleRoutes(metrics),
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
}
