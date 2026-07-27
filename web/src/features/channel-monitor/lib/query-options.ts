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
import type { ChannelMonitorPerformanceRangeMinutes } from '../types'

const CHANNEL_MONITOR_PERFORMANCE_STALE_TIME = 60_000
const CHANNEL_MONITOR_REFETCH_INTERVAL = 60_000

export const CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY = [
  'channel-monitor',
  'smart-schedule',
  'routes',
] as const

export function getChannelMonitorOverviewQueryOptions() {
  return queryOptions({
    queryKey: ['channel-monitor'],
    queryFn: getChannelMonitorOverview,
    refetchInterval: CHANNEL_MONITOR_REFETCH_INTERVAL,
  })
}

export function getChannelMonitorPerformanceQueryOptions(
  minutes: ChannelMonitorPerformanceRangeMinutes
) {
  return queryOptions({
    queryKey: ['channel-monitor-performance', minutes],
    queryFn: () => getChannelMonitorPerformance(minutes),
    staleTime: CHANNEL_MONITOR_PERFORMANCE_STALE_TIME,
    refetchInterval: CHANNEL_MONITOR_REFETCH_INTERVAL,
  })
}

export function getChannelMonitorSmartScheduleQueryOptions() {
  return queryOptions({
    queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
    queryFn: getChannelMonitorSmartScheduleRoutes,
    staleTime: CHANNEL_MONITOR_PERFORMANCE_STALE_TIME,
    refetchInterval: CHANNEL_MONITOR_REFETCH_INTERVAL,
  })
}
