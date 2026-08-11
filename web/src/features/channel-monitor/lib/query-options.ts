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
import { queryOptions } from '@tanstack/react-query'

import {
  getChannelMonitorOverview,
  getChannelMonitorPerformance,
  getChannelMonitorSmartScheduleRoutes,
} from '../api'
import type {
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorPerformanceRangeSource,
} from '../types'

const CHANNEL_MONITOR_PERFORMANCE_STALE_TIME = 60_000
const CHANNEL_MONITOR_REFETCH_INTERVAL = 60_000
const CHANNEL_STATUS_PROBE_ACTIVE_REFETCH_INTERVAL = 3_000

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

export function getChannelMonitorOverviewQueryOptions() {
  return queryOptions({
    queryKey: ['channel-monitor'],
    queryFn: getChannelMonitorOverview,
    refetchInterval: CHANNEL_MONITOR_REFETCH_INTERVAL,
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
    staleTime: CHANNEL_MONITOR_PERFORMANCE_STALE_TIME,
    refetchInterval: active ? CHANNEL_MONITOR_REFETCH_INTERVAL : false,
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

export function getChannelStatusProbeHistoryRefetchInterval(
  page: number,
  probeActive: boolean,
  visibilityState: DocumentVisibilityState
) {
  if (page !== 1 || !probeActive || visibilityState === 'hidden') return false
  return CHANNEL_STATUS_PROBE_ACTIVE_REFETCH_INTERVAL
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
    staleTime: CHANNEL_MONITOR_PERFORMANCE_STALE_TIME,
    refetchInterval: CHANNEL_MONITOR_REFETCH_INTERVAL,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
  })
}
