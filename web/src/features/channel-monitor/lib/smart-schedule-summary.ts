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
  ChannelMonitorSmartScheduleScoreDetails,
} from '../types'
import { compareChannelMonitorSmartScheduleModels } from './smart-schedule-model-order'

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
  insufficientSampleCount: number
  prioritySamplingCount: number
}

export type ChannelMonitorSmartScheduleChannelSummary = {
  channelId: number
  channelName: string
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
  prioritySamplingCount: number
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
  insufficientSampleCount: number
  prioritySamplingCount: number
  failedCount: number
  breakEvenFallbackCount: number
  breakEvenFallbackFixedCount: number
  breakEvenFallbackTakingOver: boolean
  topPriority: number | null
  candidateCount: number
  scoringWinnerChannelId: number
  actualPrimaryChannelId: number
  actualHighestPriority: number | null
  actualTopLayerChannelIds: number[]
}

export type ChannelMonitorSmartScheduleDisplayOption = {
  value: string
  label: string
  group: string
  model: string
}

export type ChannelMonitorSmartSchedulePoolStatus =
  | '稳定性降级'
  | '稳定性试放'
  | '样本不足补量'
  | '低优先级轮转'
  | '保本兜底接管'
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
  insufficientSampleCount: number
  prioritySamplingCount: number
  failedCount: number
}

export type ChannelMonitorSmartScheduleRouteRole =
  | 'primary'
  | 'candidate'
  | 'backup'
  | 'excluded'
  | 'unavailable'

export type ChannelMonitorSmartScheduleRouteDisplayStatus =
  | 'degraded'
  | 'probing'
  | 'insufficient_samples'
  | 'priority_sampling'
  | 'failed'
  | ChannelMonitorSmartScheduleRouteRole

export type ChannelMonitorSmartScheduleRoutePlacement = {
  role: ChannelMonitorSmartScheduleRouteRole
  estimatedShare: number | null
  topPriority: number | null
  candidateCount: number
  actualPrimaryChannelId: number
  scoringWinnerChannelId: number
  actualHighestPriority: number | null
  actualTopLayerChannelIds: number[]
  isActualPrimary: boolean
  isScoringWinner: boolean
  isActualTopLayer: boolean
}

const SMART_SCHEDULE_ROUTE_STATUS_ORDER: Record<
  ChannelMonitorSmartScheduleRouteDisplayStatus,
  number
> = {
  degraded: 0,
  probing: 1,
  insufficient_samples: 2,
  priority_sampling: 3,
  failed: 4,
  primary: 5,
  candidate: 6,
  backup: 7,
  unavailable: 8,
  excluded: 9,
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

export function channelMonitorSmartScheduleRouteIsAvailable(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.enabled && route.channel_status === CHANNEL_STATUS.ENABLED
}

export function channelMonitorSmartScheduleRouteIsActive(
  route: ChannelMonitorSmartScheduleRoute
) {
  return (
    channelMonitorSmartScheduleRouteParticipates(route) &&
    channelMonitorSmartScheduleRouteIsAvailable(route)
  )
}

export function channelMonitorSmartScheduleRouteIsBreakEvenFallback(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.economic_role === 'break_even_fallback'
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
  let insufficientSampleCount = 0
  let prioritySamplingCount = 0
  let failedCount = 0
  let lastScheduleTime = 0

  for (const route of routes) {
    const participates = channelMonitorSmartScheduleRouteParticipates(route)
    const available = channelMonitorSmartScheduleRouteIsAvailable(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    if (participates) participatingCount += 1
    if (active) activeCount += 1
    if (available && route.state.stability_state === 'degraded') {
      degradedCount += 1
    }
    if (available && route.state.stability_state === 'probing') {
      probingCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'insufficient_samples'
    ) {
      insufficientSampleCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'priority_sampling'
    ) {
      prioritySamplingCount += 1
    }
    if (available && route.state.last_schedule_status === 'failed') {
      failedCount += 1
    }
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
        degradedCount:
          available && route.state.stability_state === 'degraded' ? 1 : 0,
        probingCount:
          available && route.state.stability_state === 'probing' ? 1 : 0,
        insufficientSampleCount:
          available &&
          route.state.temporary_traffic_kind === 'insufficient_samples'
            ? 1
            : 0,
        prioritySamplingCount:
          available &&
          route.state.temporary_traffic_kind === 'priority_sampling'
            ? 1
            : 0,
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
    if (available && route.state.stability_state === 'degraded') {
      existing.degradedCount += 1
    }
    if (available && route.state.stability_state === 'probing') {
      existing.probingCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'insufficient_samples'
    ) {
      existing.insufficientSampleCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'priority_sampling'
    ) {
      existing.prioritySamplingCount += 1
    }
  }

  return {
    channelId: firstRoute.channel_id,
    channelName: firstRoute.channel_name,
    routeCount: routes.length,
    participatingCount,
    activeCount,
    degradedCount,
    probingCount,
    insufficientSampleCount,
    prioritySamplingCount,
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

type ChannelMonitorSmartSchedulePoolRoutingSnapshot = {
  decision: ChannelMonitorSmartScheduleScoreDetails['decision'] | undefined
  actualPrimaryChannelId: number
  actualHighestPriority: number | null
  actualTopLayerChannelIds: number[]
}

function getChannelMonitorSmartSchedulePoolRoutingSnapshot(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
): ChannelMonitorSmartSchedulePoolRoutingSnapshot {
  let decision: ChannelMonitorSmartScheduleScoreDetails['decision'] | undefined
  let decisionTime = -1
  for (const route of routes) {
    const candidate = route.state.last_schedule_score_details?.decision
    if (candidate && route.state.last_schedule_time >= decisionTime) {
      decision = candidate
      decisionTime = route.state.last_schedule_time
    }
  }

  const routableRoutes = routes.filter(
    channelMonitorSmartScheduleRouteIsAvailable
  )
  const actualHighestPriority = routableRoutes.reduce<number | null>(
    (current, route) =>
      current == null ? route.priority : Math.max(current, route.priority),
    null
  )
  const actualTopLayerRoutes =
    actualHighestPriority == null
      ? []
      : routableRoutes.filter(
          (route) => route.priority === actualHighestPriority
        )
  const actualTopLayerChannelIds = actualTopLayerRoutes
    .map((route) => route.channel_id)
    .sort((first, second) => first - second)
  const recordedTopLayer = [
    ...new Set(
      (decision?.actual_top_layer_channel_ids ?? []).filter(
        (channelId) => channelId > 0
      )
    ),
  ].sort((first, second) => first - second)
  const decisionMatchesCurrentRouting =
    decision?.actual_highest_priority === actualHighestPriority &&
    recordedTopLayer.length === actualTopLayerChannelIds.length &&
    recordedTopLayer.every(
      (channelId, index) => channelId === actualTopLayerChannelIds[index]
    )
  const largestWeight = Math.max(
    0,
    ...actualTopLayerRoutes.map((route) => Math.max(0, route.weight))
  )
  const largestWeightCount = actualTopLayerRoutes.filter(
    (route) => Math.max(0, route.weight) === largestWeight
  ).length
  const weightedPrimary = actualTopLayerRoutes.find(
    (route) =>
      actualTopLayerRoutes.length === 1 ||
      (Math.max(0, route.weight) === largestWeight && largestWeightCount === 1)
  )
  let actualPrimaryChannelId = weightedPrimary?.channel_id ?? 0
  if (actualPrimaryChannelId === 0 && decisionMatchesCurrentRouting) {
    const recordedPrimaryChannelId = decision?.actual_primary_channel_id ?? 0
    if (
      actualTopLayerRoutes.some(
        (route) => route.channel_id === recordedPrimaryChannelId
      )
    ) {
      actualPrimaryChannelId = recordedPrimaryChannelId
    }
  }

  return {
    decision,
    actualPrimaryChannelId,
    actualHighestPriority,
    actualTopLayerChannelIds,
  }
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
    const snapshot =
      getChannelMonitorSmartSchedulePoolRoutingSnapshot(poolRoutes)
    const actualTopLayerChannelIds = snapshot.actualTopLayerChannelIds
    const actualTopLayerChannelIdSet = new Set(actualTopLayerChannelIds)
    const candidates = poolRoutes.filter(
      (route) =>
        actualTopLayerChannelIdSet.has(route.channel_id) &&
        channelMonitorSmartScheduleRouteIsAvailable(route)
    )
    const scoringWinnerChannelId = snapshot.decision?.raw_winner_channel_id ?? 0
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
    const actualPrimaryChannelId = snapshot.actualPrimaryChannelId

    for (const route of poolRoutes) {
      let role: ChannelMonitorSmartScheduleRouteRole = 'backup'
      let estimatedShare: number | null = null
      const routable = channelMonitorSmartScheduleRouteIsAvailable(route)
      const isActualTopLayer = actualTopLayerChannelIdSet.has(route.channel_id)
      if (!routable) {
        role = 'unavailable'
      } else if (isActualTopLayer) {
        estimatedShare = shares.get(route.channel_id) ?? 0
        role =
          route.channel_id === actualPrimaryChannelId ? 'primary' : 'candidate'
      } else if (!channelMonitorSmartScheduleRouteParticipates(route)) {
        role = 'excluded'
      }
      placements.set(channelMonitorSmartScheduleRouteKey(route), {
        role,
        estimatedShare,
        topPriority: snapshot.actualHighestPriority,
        candidateCount: actualTopLayerChannelIds.length,
        actualPrimaryChannelId,
        scoringWinnerChannelId,
        actualHighestPriority: snapshot.actualHighestPriority,
        actualTopLayerChannelIds,
        isActualPrimary: route.channel_id === actualPrimaryChannelId,
        isScoringWinner: route.channel_id === scoringWinnerChannelId,
        isActualTopLayer,
      })
    }
  }
  return placements
}

export function getChannelMonitorSmartScheduleRouteDisplayStatus(
  route: ChannelMonitorSmartScheduleRoute,
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
): ChannelMonitorSmartScheduleRouteDisplayStatus {
  if (!channelMonitorSmartScheduleRouteIsAvailable(route)) return 'unavailable'
  if (route.state.stability_state === 'degraded') return 'degraded'
  if (route.state.stability_state === 'probing') return 'probing'
  if (route.state.temporary_traffic_kind === 'insufficient_samples') {
    return 'insufficient_samples'
  }
  if (route.state.temporary_traffic_kind === 'priority_sampling') {
    return 'priority_sampling'
  }
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
  const routesByPool = new Map<string, ChannelMonitorSmartScheduleRoute[]>()
  for (const route of routes) {
    const key = `${route.group}\u0000${route.model}`
    const poolRoutes = routesByPool.get(key)
    if (poolRoutes) poolRoutes.push(route)
    else routesByPool.set(key, [route])
    const participates = channelMonitorSmartScheduleRouteParticipates(route)
    const available = channelMonitorSmartScheduleRouteIsAvailable(route)
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
        degradedCount:
          available && route.state.stability_state === 'degraded' ? 1 : 0,
        probingCount:
          available && route.state.stability_state === 'probing' ? 1 : 0,
        insufficientSampleCount:
          available &&
          route.state.temporary_traffic_kind === 'insufficient_samples'
            ? 1
            : 0,
        prioritySamplingCount:
          available &&
          route.state.temporary_traffic_kind === 'priority_sampling'
            ? 1
            : 0,
        failedCount:
          available && route.state.last_schedule_status === 'failed' ? 1 : 0,
        breakEvenFallbackCount:
          route.economic_role === 'break_even_fallback' ? 1 : 0,
        breakEvenFallbackFixedCount:
          route.economic_role === 'break_even_fallback' &&
          route.state.manual_primary_until > 0
            ? 1
            : 0,
        breakEvenFallbackTakingOver: false,
        topPriority: active ? route.priority : null,
        candidateCount: active ? 1 : 0,
        scoringWinnerChannelId: 0,
        actualPrimaryChannelId: 0,
        actualHighestPriority: null,
        actualTopLayerChannelIds: [],
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
    if (available && route.state.stability_state === 'degraded') {
      existing.degradedCount += 1
    }
    if (available && route.state.stability_state === 'probing') {
      existing.probingCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'insufficient_samples'
    ) {
      existing.insufficientSampleCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'priority_sampling'
    ) {
      existing.prioritySamplingCount += 1
    }
    if (available && route.state.last_schedule_status === 'failed') {
      existing.failedCount += 1
    }
    if (route.economic_role === 'break_even_fallback') {
      existing.breakEvenFallbackCount += 1
      if (route.state.manual_primary_until > 0) {
        existing.breakEvenFallbackFixedCount += 1
      }
    }
  }
  const summaries = [...poolMap.entries()].map(([key, summary]) => {
    const poolRoutes = routesByPool.get(key) ?? []
    const snapshot =
      getChannelMonitorSmartSchedulePoolRoutingSnapshot(poolRoutes)
    const topLayerChannelIdSet = new Set(snapshot.actualTopLayerChannelIds)
    const topLayerRoutes = poolRoutes.filter((route) =>
      topLayerChannelIdSet.has(route.channel_id)
    )
    return {
      ...summary,
      topPriority: snapshot.actualHighestPriority,
      candidateCount: snapshot.actualTopLayerChannelIds.length,
      scoringWinnerChannelId: snapshot.decision?.raw_winner_channel_id ?? 0,
      actualPrimaryChannelId: snapshot.actualPrimaryChannelId,
      actualHighestPriority: snapshot.actualHighestPriority,
      actualTopLayerChannelIds: snapshot.actualTopLayerChannelIds,
      breakEvenFallbackTakingOver:
        topLayerRoutes.length > 0 &&
        topLayerRoutes.every(
          (route) =>
            channelMonitorSmartScheduleRouteIsBreakEvenFallback(route) &&
            route.state.manual_primary_until <= 0
        ),
    }
  })
  return summaries.sort((first, second) => {
    const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
      first.group,
      second.group,
      groupRatios
    )
    return groupOrder || first.model.localeCompare(second.model)
  })
}

export function getChannelMonitorSmartScheduleDisplayOptions(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  groupRatios: Readonly<Record<string, number>> = EMPTY_GROUP_RATIOS,
  modelOrderByGroup?: ReadonlyMap<string, readonly string[]>
): ChannelMonitorSmartScheduleDisplayOption[] {
  return summarizeChannelMonitorSmartSchedulePools(routes, groupRatios)
    .sort((first, second) => {
      const groupOrder = compareChannelMonitorSmartScheduleGroupsByRatio(
        first.group,
        second.group,
        groupRatios
      )
      if (groupOrder !== 0) return groupOrder
      return compareChannelMonitorSmartScheduleModels(
        first.model,
        second.model,
        modelOrderByGroup?.get(first.group)
      )
    })
    .map((pool) => ({
      value: JSON.stringify([pool.group, pool.model]),
      label: `${pool.group} / ${pool.model}`,
      group: pool.group,
      model: pool.model,
    }))
}

export function getChannelMonitorSmartSchedulePoolStatus(pool: {
  routeCount: number
  participatingCount: number
  activeCount: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
  prioritySamplingCount: number
  failedCount?: number
  breakEvenFallbackTakingOver?: boolean
}): ChannelMonitorSmartSchedulePoolStatus {
  if (pool.degradedCount > 0) return '稳定性降级'
  if (pool.probingCount > 0) return '稳定性试放'
  if (pool.insufficientSampleCount > 0) return '样本不足补量'
  if (pool.prioritySamplingCount > 0) return '低优先级轮转'
  if ((pool.failedCount ?? 0) > 0) return '最近失败'
  if (pool.breakEvenFallbackTakingOver) return '保本兜底接管'
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
  let insufficientSampleCount = 0
  let prioritySamplingCount = 0
  let failedCount = 0
  for (const route of routes) {
    channels.add(route.channel_id)
    groups.add(route.group)
    if (channelMonitorSmartScheduleRouteParticipates(route)) {
      participatingCount += 1
    }
    const available = channelMonitorSmartScheduleRouteIsAvailable(route)
    if (channelMonitorSmartScheduleRouteIsActive(route)) activeCount += 1
    if (available && route.state.stability_state === 'degraded') {
      degradedCount += 1
    }
    if (available && route.state.stability_state === 'probing') {
      probingCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'insufficient_samples'
    ) {
      insufficientSampleCount += 1
    }
    if (
      available &&
      route.state.temporary_traffic_kind === 'priority_sampling'
    ) {
      prioritySamplingCount += 1
    }
    if (available && route.state.last_schedule_status === 'failed') {
      failedCount += 1
    }
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
        pool.insufficientSampleCount === 0 &&
        pool.prioritySamplingCount === 0 &&
        pool.failedCount === 0 &&
        !pool.breakEvenFallbackTakingOver
    ).length,
    degradedCount,
    probingCount,
    insufficientSampleCount,
    prioritySamplingCount,
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
