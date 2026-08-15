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
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
} as const

export const CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY = [
  'channel-monitor',
  'smart-schedule',
  'routes',
] as const

export const CHANNEL_MONITOR_SMART_SCHEDULE_EXECUTIONS_QUERY_KEY = [
  'channel-monitor-smart-schedule-executions',
] as const

export const CHANNEL_MONITOR_TASK_HISTORY_QUERY_KEY = [
  'channel-monitor-task-history',
] as const

const CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_KEYS = [
  ['channel-monitor'],
  ['channel-monitor-performance'],
  ['channel-monitor-smart-schedule-executions'],
  ['channel-monitor-task-history'],
  ['channel-monitor-success-detail'],
  ['channel-monitor-history'],
  ['channel-monitor-available-groups'],
] as const

export async function refetchChannelMonitorQueries(queryClient: QueryClient) {
  await Promise.all(
    CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_KEYS.map((queryKey) =>
      queryClient.refetchQueries({ queryKey, type: 'all' })
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
