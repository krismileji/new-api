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

import type {
  ChannelMonitorSmartScheduleGroupPolicy,
  ChannelMonitorSmartScheduleRoute,
} from '../types'

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
  explorationCount: number
}

export type ChannelMonitorSmartScheduleChannelSummary = {
  channelId: number
  channelName: string
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
  explorationCount: number
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
  explorationCount: number
  failedCount: number
  topPriority: number | null
  candidateCount: number
}

export type ChannelMonitorSmartSchedulePoolStatus =
  | '稳定性降级'
  | '稳定性试放'
  | '探索采样'
  | '最近失败'
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
  healthyPoolCount: number
  degradedCount: number
  probingCount: number
  explorationCount: number
  failedCount: number
}

export type ChannelMonitorSmartScheduleRouteRole =
  | 'primary'
  | 'candidate'
  | 'standby'
  | 'excluded'
  | 'unavailable'

export type ChannelMonitorSmartScheduleRouteDisplayStatus =
  | 'degraded'
  | 'probing'
  | 'exploring'
  | 'failed'
  | ChannelMonitorSmartScheduleRouteRole

export type ChannelMonitorSmartScheduleRoutePlacement = {
  role: ChannelMonitorSmartScheduleRouteRole
  estimatedShare: number | null
  topPriority: number | null
  candidateCount: number
}

const SMART_SCHEDULE_ROUTE_STATUS_ORDER: Record<
  ChannelMonitorSmartScheduleRouteDisplayStatus,
  number
> = {
  degraded: 0,
  probing: 1,
  exploring: 2,
  failed: 3,
  primary: 4,
  candidate: 5,
  standby: 6,
  unavailable: 7,
  excluded: 8,
}

const EMPTY_GROUP_RATIOS: Readonly<Record<string, number>> = {}

export function compareChannelMonitorSmartScheduleGroupsByRatio(
  first: string,
  second: string,
  groupRatios: Readonly<Record<string, number>>
) {
  const ratioOrder = (groupRatios[first] ?? 1) - (groupRatios[second] ?? 1)
  return ratioOrder || first.localeCompare(second)
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
  return route.state.participation_set && !route.state.excluded
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

export function filterChannelMonitorSmartScheduleRoutes(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  enabled: boolean,
  groupPolicies: readonly Pick<
    ChannelMonitorSmartScheduleGroupPolicy,
    'group' | 'models'
  >[]
) {
  if (!enabled) return []

  const policyByGroup = new Map(
    groupPolicies.map((policy) => [policy.group, policy.models])
  )
  return routes.filter((route) => {
    const models = policyByGroup.get(route.group)
    return (
      models !== undefined &&
      (models.length === 0 || models.includes(route.model))
    )
  })
}

export function summarizeChannelMonitorSmartScheduleChannel(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  groupRatios: Readonly<Record<string, number>> = EMPTY_GROUP_RATIOS
): ChannelMonitorSmartScheduleChannelSummary | null {
  const firstRoute = routes[0]
  if (!firstRoute) return null

  const groupMap = new Map<string, ChannelMonitorSmartScheduleGroupSummary>()
  let participatingCount = 0
  let activeCount = 0
  let degradedCount = 0
  let probingCount = 0
  let explorationCount = 0
  let failedCount = 0
  let lastScheduleTime = 0

  for (const route of routes) {
    const participates = channelMonitorSmartScheduleRouteParticipates(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    if (participates) participatingCount += 1
    if (active) activeCount += 1
    if (route.state.stability_state === 'degraded') degradedCount += 1
    if (route.state.stability_state === 'probing') probingCount += 1
    if (route.state.exploration_active) explorationCount += 1
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
        explorationCount: route.state.exploration_active ? 1 : 0,
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
    if (route.state.exploration_active) existing.explorationCount += 1
  }

  return {
    channelId: firstRoute.channel_id,
    channelName: firstRoute.channel_name,
    routeCount: routes.length,
    participatingCount,
    activeCount,
    degradedCount,
    probingCount,
    explorationCount,
    failedCount,
    lastScheduleTime,
    groups: [...groupMap.values()].sort((first, second) =>
      compareChannelMonitorSmartScheduleGroupsByRatio(
        first.group,
        second.group,
        groupRatios
      )
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

export function placeChannelMonitorSmartScheduleRoutes(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
) {
  const routesByPool = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
  for (const route of routes) {
    const poolKey = `${route.group}\u0000${route.model}`
    const poolRoutes = routesByPool.get(poolKey)
    if (poolRoutes) poolRoutes.push(route)
    else routesByPool.set(poolKey, [route])
  }

  const placements = new Map<
    string,
    ChannelMonitorSmartScheduleRoutePlacement
  >()
  for (const poolRoutes of routesByPool.values()) {
    const activeRoutes = poolRoutes.filter(
      channelMonitorSmartScheduleRouteIsActive
    )
    const topPriority = activeRoutes.reduce<number | null>(
      (current, route) =>
        current == null ? route.priority : Math.max(current, route.priority),
      null
    )
    const candidates =
      topPriority == null
        ? []
        : activeRoutes.filter((route) => route.priority === topPriority)
    const totalWeight = candidates.reduce(
      (total, route) => total + Math.max(0, route.weight),
      0
    )
    const shares = new Map<number, number>()
    for (const route of candidates) {
      const share =
        totalWeight > 0
          ? Math.max(0, route.weight) / totalWeight
          : 1 / candidates.length
      shares.set(route.channel_id, share)
    }
    const largestShare = Math.max(0, ...shares.values())
    const largestShareCount = [...shares.values()].filter(
      (share) => Math.abs(share - largestShare) < Number.EPSILON
    ).length

    for (const route of poolRoutes) {
      let role: ChannelMonitorSmartScheduleRouteRole = 'standby'
      let estimatedShare: number | null = null
      if (!channelMonitorSmartScheduleRouteParticipates(route)) {
        role = 'excluded'
      } else if (!channelMonitorSmartScheduleRouteIsActive(route)) {
        role = 'unavailable'
      } else if (route.priority === topPriority) {
        estimatedShare = shares.get(route.channel_id) ?? 0
        role =
          candidates.length === 1 ||
          (estimatedShare === largestShare && largestShareCount === 1)
            ? 'primary'
            : 'candidate'
      }
      placements.set(channelMonitorSmartScheduleRouteKey(route), {
        role,
        estimatedShare,
        topPriority,
        candidateCount: candidates.length,
      })
    }
  }
  return placements
}

export function getChannelMonitorSmartScheduleRouteDisplayStatus(
  route: ChannelMonitorSmartScheduleRoute,
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
): ChannelMonitorSmartScheduleRouteDisplayStatus {
  if (route.state.stability_state === 'degraded') return 'degraded'
  if (route.state.stability_state === 'probing') return 'probing'
  if (route.state.exploration_active) return 'exploring'
  if (route.state.last_schedule_status === 'failed') return 'failed'
  return placement?.role ?? 'unavailable'
}

function compareChannelMonitorSmartScheduleRouteAttention(
  first: ChannelMonitorSmartScheduleRoute,
  second: ChannelMonitorSmartScheduleRoute,
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>
) {
  const firstStatus = getChannelMonitorSmartScheduleRouteDisplayStatus(
    first,
    placements.get(channelMonitorSmartScheduleRouteKey(first))
  )
  const secondStatus = getChannelMonitorSmartScheduleRouteDisplayStatus(
    second,
    placements.get(channelMonitorSmartScheduleRouteKey(second))
  )
  const statusOrder =
    SMART_SCHEDULE_ROUTE_STATUS_ORDER[firstStatus] -
    SMART_SCHEDULE_ROUTE_STATUS_ORDER[secondStatus]
  if (statusOrder !== 0) return statusOrder
  const priorityOrder = second.priority - first.priority
  if (priorityOrder !== 0) return priorityOrder
  const weightOrder = second.weight - first.weight
  return weightOrder
}

export function compareChannelMonitorSmartScheduleRoutesByAttention(
  first: ChannelMonitorSmartScheduleRoute,
  second: ChannelMonitorSmartScheduleRoute,
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>,
  groupRatios: Readonly<Record<string, number>> = EMPTY_GROUP_RATIOS
) {
  const attentionOrder = compareChannelMonitorSmartScheduleRouteAttention(
    first,
    second,
    placements
  )
  if (attentionOrder !== 0) return attentionOrder
  const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
    first.group,
    second.group,
    groupRatios
  )
  if (groupOrder !== 0) return groupOrder
  const modelOrder = first.model.localeCompare(second.model)
  if (modelOrder !== 0) return modelOrder
  const channelOrder = first.channel_name.localeCompare(second.channel_name)
  return channelOrder || first.channel_id - second.channel_id
}

export function compareChannelMonitorSmartScheduleRoutesByPool(
  first: ChannelMonitorSmartScheduleRoute,
  second: ChannelMonitorSmartScheduleRoute,
  placements: ReadonlyMap<string, ChannelMonitorSmartScheduleRoutePlacement>,
  groupRatios: Readonly<Record<string, number>> = EMPTY_GROUP_RATIOS
) {
  const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
    first.group,
    second.group,
    groupRatios
  )
  if (groupOrder !== 0) return groupOrder
  const modelOrder = first.model.localeCompare(second.model)
  if (modelOrder !== 0) return modelOrder
  const attentionOrder = compareChannelMonitorSmartScheduleRouteAttention(
    first,
    second,
    placements
  )
  if (attentionOrder !== 0) return attentionOrder
  const channelOrder = first.channel_name.localeCompare(second.channel_name)
  return channelOrder || first.channel_id - second.channel_id
}

export function summarizeChannelMonitorSmartSchedulePools(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  groupRatios: Readonly<Record<string, number>> = EMPTY_GROUP_RATIOS
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
        explorationCount: route.state.exploration_active ? 1 : 0,
        failedCount: route.state.last_schedule_status === 'failed' ? 1 : 0,
        topPriority: active ? route.priority : null,
        candidateCount: active ? 1 : 0,
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
    if (route.state.exploration_active) existing.explorationCount += 1
    if (route.state.last_schedule_status === 'failed') existing.failedCount += 1
    if (active) {
      if (
        existing.topPriority == null ||
        route.priority > existing.topPriority
      ) {
        existing.topPriority = route.priority
        existing.candidateCount = 1
      } else if (route.priority === existing.topPriority) {
        existing.candidateCount += 1
      }
    }
  }
  return [...poolMap.values()].sort((first, second) => {
    const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
      first.group,
      second.group,
      groupRatios
    )
    return groupOrder || first.model.localeCompare(second.model)
  })
}

export function getChannelMonitorSmartSchedulePoolStatus(pool: {
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
  explorationCount: number
  failedCount?: number
}): ChannelMonitorSmartSchedulePoolStatus {
  if (pool.degradedCount > 0) return '稳定性降级'
  if (pool.probingCount > 0) return '稳定性试放'
  if (pool.explorationCount > 0) return '探索采样'
  if ((pool.failedCount ?? 0) > 0) return '最近失败'
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
  let explorationCount = 0
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
    if (route.state.exploration_active) explorationCount += 1
    if (route.state.last_schedule_status === 'failed') failedCount += 1
  }
  const pools = summarizeChannelMonitorSmartSchedulePools(routes)
  return {
    routeCount: routes.length,
    participatingCount,
    activeCount,
    channelCount: channels.size,
    groupCount: groups.size,
    poolCount: pools.length,
    healthyPoolCount: pools.filter(
      (pool) =>
        pool.activeCount > 0 &&
        pool.degradedCount === 0 &&
        pool.probingCount === 0 &&
        pool.explorationCount === 0 &&
        pool.failedCount === 0
    ).length,
    degradedCount,
    probingCount,
    explorationCount,
    failedCount,
  }
}

export function isChannelMonitorSmartScheduleResultStale(
  generatedAt: number,
  intervalMinutes: number,
  nowSeconds = Date.now() / 1000
) {
  if (generatedAt <= 0) return false
  return nowSeconds - generatedAt > Math.max(120, intervalMinutes * 120)
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
