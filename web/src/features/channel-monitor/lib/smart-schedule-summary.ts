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
import { CHANNEL_STATUS } from '@/features/channels/constants'

import type { ChannelMonitorSmartScheduleRoute } from '../types'

export type ChannelMonitorSmartScheduleGroupSummary = {
  group: string
  routeCount: number
  participatingCount: number
  activeCount: number
  priorityMin: number
  priorityMax: number
  weightMin: number
  weightMax: number
  degradedCount: number
  probingCount: number
}

export type ChannelMonitorSmartScheduleChannelSummary = {
  channelId: number
  channelName: string
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
  failedCount: number
  lastScheduleTime: number
  groups: ChannelMonitorSmartScheduleGroupSummary[]
}

export type ChannelMonitorSmartSchedulePoolSummary = {
  group: string
  model: string
  routeCount: number
  participatingCount: number
  activeCount: number
  priorityMin: number
  priorityMax: number
  weightMin: number
  weightMax: number
  degradedCount: number
  probingCount: number
}

export type ChannelMonitorSmartSchedulePoolStatus =
  | '低成功率'
  | '稳定性试放'
  | '未参与调度'
  | '当前不可调度'
  | '部分可调度'
  | '部分参与'
  | '正常'

export type ChannelMonitorSmartScheduleOverviewSummary = {
  routeCount: number
  participatingCount: number
  activeCount: number
  channelCount: number
  groupCount: number
  poolCount: number
  degradedCount: number
  probingCount: number
  failedCount: number
}

export function channelMonitorSmartScheduleRouteKey(route: {
  channel_id: number
  group: string
  model: string
}) {
  return `${route.channel_id}\u0000${route.group}\u0000${route.model}`
}

export function channelMonitorSmartScheduleRouteParticipates(
  route: ChannelMonitorSmartScheduleRoute
) {
  return !route.state.excluded
}

export function channelMonitorSmartScheduleRouteIsActive(
  route: ChannelMonitorSmartScheduleRoute
) {
  return (
    channelMonitorSmartScheduleRouteParticipates(route) &&
    route.enabled &&
    route.channel_status === CHANNEL_STATUS.ENABLED
  )
}

export function summarizeChannelMonitorSmartScheduleChannel(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
): ChannelMonitorSmartScheduleChannelSummary | null {
  const firstRoute = routes[0]
  if (!firstRoute) return null

  const groupMap = new Map<string, ChannelMonitorSmartScheduleGroupSummary>()
  let participatingCount = 0
  let activeCount = 0
  let degradedCount = 0
  let probingCount = 0
  let failedCount = 0
  let lastScheduleTime = 0

  for (const route of routes) {
    const participates = channelMonitorSmartScheduleRouteParticipates(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    if (participates) participatingCount += 1
    if (active) activeCount += 1
    if (route.state.stability_state === 'degraded') degradedCount += 1
    if (route.state.stability_state === 'probing') probingCount += 1
    if (route.state.last_schedule_status === 'failed') failedCount += 1
    lastScheduleTime = Math.max(
      lastScheduleTime,
      route.state.last_schedule_time
    )

    const existing = groupMap.get(route.group)
    if (!existing) {
      groupMap.set(route.group, {
        group: route.group,
        routeCount: 1,
        participatingCount: participates ? 1 : 0,
        activeCount: active ? 1 : 0,
        priorityMin: route.priority,
        priorityMax: route.priority,
        weightMin: route.weight,
        weightMax: route.weight,
        degradedCount: route.state.stability_state === 'degraded' ? 1 : 0,
        probingCount: route.state.stability_state === 'probing' ? 1 : 0,
      })
      continue
    }
    existing.routeCount += 1
    if (participates) existing.participatingCount += 1
    if (active) existing.activeCount += 1
    existing.priorityMin = Math.min(existing.priorityMin, route.priority)
    existing.priorityMax = Math.max(existing.priorityMax, route.priority)
    existing.weightMin = Math.min(existing.weightMin, route.weight)
    existing.weightMax = Math.max(existing.weightMax, route.weight)
    if (route.state.stability_state === 'degraded') existing.degradedCount += 1
    if (route.state.stability_state === 'probing') existing.probingCount += 1
  }

  return {
    channelId: firstRoute.channel_id,
    channelName: firstRoute.channel_name,
    routeCount: routes.length,
    participatingCount,
    activeCount,
    degradedCount,
    probingCount,
    failedCount,
    lastScheduleTime,
    groups: [...groupMap.values()].sort((first, second) =>
      first.group.localeCompare(second.group)
    ),
  }
}

export function groupChannelMonitorSmartScheduleRoutesByChannel(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
) {
  const result = new Map<number, ChannelMonitorSmartScheduleRoute[]>()
  for (const route of routes) {
    const channelRoutes = result.get(route.channel_id)
    if (channelRoutes) {
      channelRoutes.push(route)
    } else {
      result.set(route.channel_id, [route])
    }
  }
  return result
}

export function summarizeChannelMonitorSmartSchedulePools(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
) {
  const poolMap = new Map<string, ChannelMonitorSmartSchedulePoolSummary>()
  for (const route of routes) {
    const key = `${route.group}\u0000${route.model}`
    const participates = channelMonitorSmartScheduleRouteParticipates(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    const existing = poolMap.get(key)
    if (!existing) {
      poolMap.set(key, {
        group: route.group,
        model: route.model,
        routeCount: 1,
        participatingCount: participates ? 1 : 0,
        activeCount: active ? 1 : 0,
        priorityMin: route.priority,
        priorityMax: route.priority,
        weightMin: route.weight,
        weightMax: route.weight,
        degradedCount: route.state.stability_state === 'degraded' ? 1 : 0,
        probingCount: route.state.stability_state === 'probing' ? 1 : 0,
      })
      continue
    }
    existing.routeCount += 1
    if (participates) existing.participatingCount += 1
    if (active) existing.activeCount += 1
    existing.priorityMin = Math.min(existing.priorityMin, route.priority)
    existing.priorityMax = Math.max(existing.priorityMax, route.priority)
    existing.weightMin = Math.min(existing.weightMin, route.weight)
    existing.weightMax = Math.max(existing.weightMax, route.weight)
    if (route.state.stability_state === 'degraded') existing.degradedCount += 1
    if (route.state.stability_state === 'probing') existing.probingCount += 1
  }
  return [...poolMap.values()].sort((first, second) => {
    const groupOrder = first.group.localeCompare(second.group)
    return groupOrder || first.model.localeCompare(second.model)
  })
}

export function getChannelMonitorSmartSchedulePoolStatus(pool: {
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
}): ChannelMonitorSmartSchedulePoolStatus {
  if (pool.degradedCount > 0) return '低成功率'
  if (pool.probingCount > 0) return '稳定性试放'
  if (pool.participatingCount === 0) return '未参与调度'
  if (pool.activeCount === 0) return '当前不可调度'
  if (pool.activeCount < pool.participatingCount) return '部分可调度'
  if (pool.participatingCount < pool.routeCount) return '部分参与'
  return '正常'
}

export function summarizeChannelMonitorSmartScheduleOverview(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
): ChannelMonitorSmartScheduleOverviewSummary {
  const channels = new Set<number>()
  const groups = new Set<string>()
  let participatingCount = 0
  let activeCount = 0
  let degradedCount = 0
  let probingCount = 0
  let failedCount = 0
  for (const route of routes) {
    channels.add(route.channel_id)
    groups.add(route.group)
    if (channelMonitorSmartScheduleRouteParticipates(route)) {
      participatingCount += 1
    }
    if (channelMonitorSmartScheduleRouteIsActive(route)) activeCount += 1
    if (route.state.stability_state === 'degraded') degradedCount += 1
    if (route.state.stability_state === 'probing') probingCount += 1
    if (route.state.last_schedule_status === 'failed') failedCount += 1
  }
  return {
    routeCount: routes.length,
    participatingCount,
    activeCount,
    channelCount: channels.size,
    groupCount: groups.size,
    poolCount: summarizeChannelMonitorSmartSchedulePools(routes).length,
    degradedCount,
    probingCount,
    failedCount,
  }
}

export function formatChannelMonitorSmartSchedulePriorityWeightRange(values: {
  priorityMin: number
  priorityMax: number
  weightMin: number
  weightMax: number
}) {
  const priority =
    values.priorityMin === values.priorityMax
      ? `P${values.priorityMin}`
      : `P${values.priorityMin}-${values.priorityMax}`
  const weight =
    values.weightMin === values.weightMax
      ? `W${values.weightMin}`
      : `W${values.weightMin}-${values.weightMax}`
  return `${priority} / ${weight}`
}
