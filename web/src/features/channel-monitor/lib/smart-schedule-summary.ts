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
  pausedCount: number
  priorityMin: number
  priorityMax: number
  weightMin: number
  weightMax: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
}

export type ChannelMonitorSmartScheduleChannelSummary = {
  channelId: number
  channelName: string
  routeCount: number
  participatingCount: number
  activeCount: number
  pausedCount: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
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
  pausedCount: number
  priorityMin: number
  priorityMax: number
  weightMin: number
  weightMax: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
  failedCount: number
  breakEvenFallbackCount: number
  breakEvenFallbackFixedCount: number
  breakEvenFallbackTakingOver: boolean
  topPriority: number | null
  candidateCount: number
  scoringWinnerChannelId: number
  historicalScoringWinnerChannelId: number
  scoringWinnerSource: 'current_window' | 'last_schedule' | 'none'
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
  | '统一采样'
  | '保本兜底接管'
  | '最近失败'
  | '未参与调度'
  | '流量已暂停'
  | '部分流量暂停'
  | '当前不可调度'
  | '部分可调度'
  | '部分参与'
  | '正常'

export type ChannelMonitorSmartScheduleOverviewSummary = {
  routeCount: number
  participatingCount: number
  activeCount: number
  pausedCount: number
  channelCount: number
  groupCount: number
  poolCount: number
  healthyPoolCount: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
  failedCount: number
}

export type ChannelMonitorSmartScheduleRouteRole =
  | 'primary'
  | 'candidate'
  | 'backup'
  | 'rate_limited'
  | 'paused'
  | 'excluded'
  | 'unavailable'

export type ChannelMonitorSmartScheduleRouteDisplayStatus =
  | 'degraded'
  | 'probing'
  | 'insufficient_samples'
  | 'adaptive_sampling'
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
  rate_limited: 0,
  paused: 1,
  degraded: 2,
  probing: 3,
  insufficient_samples: 4,
  adaptive_sampling: 5,
  failed: 6,
  primary: 7,
  candidate: 8,
  backup: 9,
  unavailable: 10,
  excluded: 11,
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

export function channelMonitorSmartScheduleRouteRuntimeState(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.effective_state ?? route.state
}

export function channelMonitorSmartScheduleRouteRuntimePriority(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.effective_priority ?? route.priority
}

export function channelMonitorSmartScheduleRouteRuntimeWeight(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.effective_weight ?? route.weight
}

export function channelMonitorSmartScheduleRouteCandidateChannelId(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.routing_candidate_channel_id || route.channel_id
}

export function channelMonitorSmartScheduleRouteCandidateMemberWeight(
  route: ChannelMonitorSmartScheduleRoute
) {
  const memberIds = route.logical_member_ids
  const memberWeights = route.logical_member_weights
  if (memberIds && memberWeights) {
    const memberIndex = memberIds.indexOf(route.channel_id)
    if (memberIndex >= 0) return memberWeights[memberIndex] ?? 0
  }
  return channelMonitorSmartScheduleRouteRuntimeWeight(route)
}

function channelMonitorSmartScheduleRecordedCandidateId(
  routes: readonly ChannelMonitorSmartScheduleRoute[],
  channelId: number
) {
  if (channelId <= 0) return channelId
  const route = routes.find((item) => item.channel_id === channelId)
  return route
    ? channelMonitorSmartScheduleRouteCandidateChannelId(route)
    : channelId
}

export function channelMonitorSmartScheduleRouteParticipates(
  route: ChannelMonitorSmartScheduleRoute
) {
  return route.state.participation_set && !route.state.excluded
}

export function channelMonitorSmartScheduleRouteRuntimeParticipates(
  route: ChannelMonitorSmartScheduleRoute
) {
  const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
  return (
    channelMonitorSmartScheduleRouteParticipates(route) &&
    runtimeState.participation_set &&
    !runtimeState.excluded
  )
}

export function channelMonitorSmartScheduleRouteIsAvailable(
  route: ChannelMonitorSmartScheduleRoute
) {
  return (
    route.enabled &&
    route.channel_status === CHANNEL_STATUS.ENABLED &&
    !channelMonitorSmartScheduleRouteIsTrafficPaused(route)
  )
}

export function channelMonitorSmartScheduleRouteIsTrafficPaused(
  route: ChannelMonitorSmartScheduleRoute,
  nowSeconds = Date.now() / 1000
) {
  return (route.traffic_paused_until ?? 0) > nowSeconds
}

export function channelMonitorSmartScheduleRouteIsRateLimitCoolingDown(
  route: ChannelMonitorSmartScheduleRoute,
  nowSeconds = Date.now() / 1000
) {
  return (
    (route.rate_limit_bypass_until ?? 0) <= nowSeconds &&
    (route.rate_limit_cooldown_until ?? 0) > nowSeconds
  )
}

export function channelMonitorSmartScheduleRouteIsActive(
  route: ChannelMonitorSmartScheduleRoute
) {
  return (
    channelMonitorSmartScheduleRouteRuntimeParticipates(route) &&
    channelMonitorSmartScheduleRouteIsAvailable(route) &&
    channelMonitorSmartScheduleRouteRuntimeState(route).stability_state !==
      'degraded'
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
  let pausedCount = 0
  let degradedCount = 0
  let probingCount = 0
  let insufficientSampleCount = 0
  let failedCount = 0
  let lastScheduleTime = 0

  for (const route of routes) {
    const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
    const runtimePriority =
      channelMonitorSmartScheduleRouteRuntimePriority(route)
    const runtimeWeight = channelMonitorSmartScheduleRouteRuntimeWeight(route)
    const participates = channelMonitorSmartScheduleRouteRuntimeParticipates(route)
    const paused =
      participates &&
      route.enabled &&
      route.channel_status === CHANNEL_STATUS.ENABLED &&
      channelMonitorSmartScheduleRouteIsTrafficPaused(route)
    const available =
      participates && channelMonitorSmartScheduleRouteIsAvailable(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    if (participates) participatingCount += 1
    if (active) activeCount += 1
    if (paused) pausedCount += 1
    if (available && runtimeState.stability_state === 'degraded') {
      degradedCount += 1
    }
    if (active && runtimeState.stability_state === 'probing') {
      probingCount += 1
    }
    if (
      active &&
      (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
        runtimeState.temporary_traffic_kind === 'adaptive_sampling')
    ) {
      insufficientSampleCount += 1
    }
    if (active && runtimeState.last_schedule_status === 'failed') {
      failedCount += 1
    }
    lastScheduleTime = Math.max(
      lastScheduleTime,
      runtimeState.last_schedule_time
    )

    const existing = groupMap.get(route.group)
    if (!existing) {
      groupMap.set(route.group, {
        group: route.group,
        routeCount: 1,
        participatingCount: participates ? 1 : 0,
        activeCount: active ? 1 : 0,
        pausedCount: paused ? 1 : 0,
        priorityMin: runtimePriority,
        priorityMax: runtimePriority,
        weightMin: runtimeWeight,
        weightMax: runtimeWeight,
        degradedCount:
          available && runtimeState.stability_state === 'degraded' ? 1 : 0,
        probingCount:
          active && runtimeState.stability_state === 'probing' ? 1 : 0,
        insufficientSampleCount:
          active &&
          (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
            runtimeState.temporary_traffic_kind === 'adaptive_sampling')
            ? 1
            : 0,
      })
      continue
    }
    existing.routeCount += 1
    if (participates) existing.participatingCount += 1
    if (active) existing.activeCount += 1
    if (paused) existing.pausedCount += 1
    existing.priorityMin = Math.min(existing.priorityMin, runtimePriority)
    existing.priorityMax = Math.max(existing.priorityMax, runtimePriority)
    existing.weightMin = Math.min(existing.weightMin, runtimeWeight)
    existing.weightMax = Math.max(existing.weightMax, runtimeWeight)
    if (available && runtimeState.stability_state === 'degraded') {
      existing.degradedCount += 1
    }
    if (active && runtimeState.stability_state === 'probing') {
      existing.probingCount += 1
    }
    if (
      active &&
      (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
        runtimeState.temporary_traffic_kind === 'adaptive_sampling')
    ) {
      existing.insufficientSampleCount += 1
    }
  }

  return {
    channelId: firstRoute.channel_id,
    channelName: firstRoute.channel_name,
    routeCount: routes.length,
    participatingCount,
    activeCount,
    pausedCount,
    degradedCount,
    probingCount,
    insufficientSampleCount,
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
  historicalDecision:
    | ChannelMonitorSmartScheduleScoreDetails['decision']
    | undefined
  decisionSource: 'current_window' | 'last_schedule' | 'none'
  actualPrimaryChannelId: number
  actualHighestPriority: number | null
  actualTopLayerChannelIds: number[]
}

function getChannelMonitorSmartSchedulePoolRoutingSnapshot(
  routes: readonly ChannelMonitorSmartScheduleRoute[]
): ChannelMonitorSmartSchedulePoolRoutingSnapshot {
  const candidatesById = new Map<
    number,
    {
      routes: ChannelMonitorSmartScheduleRoute[]
      activeRoutes: ChannelMonitorSmartScheduleRoute[]
      priority: number
      weight: number
    }
  >()
  for (const route of routes) {
    const candidateId =
      channelMonitorSmartScheduleRouteCandidateChannelId(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    const candidate = candidatesById.get(candidateId)
    if (candidate) {
      candidate.routes.push(route)
      if (active) {
        candidate.activeRoutes.push(route)
        if (candidate.activeRoutes.length === 1) {
          candidate.priority = channelMonitorSmartScheduleRouteRuntimePriority(
            route
          )
          candidate.weight = channelMonitorSmartScheduleRouteRuntimeWeight(route)
        } else {
          candidate.priority = Math.max(
            candidate.priority,
            channelMonitorSmartScheduleRouteRuntimePriority(route)
          )
          candidate.weight = Math.max(
            candidate.weight,
            channelMonitorSmartScheduleRouteRuntimeWeight(route)
          )
        }
      }
    } else {
      candidatesById.set(candidateId, {
        routes: [route],
        activeRoutes: active ? [route] : [],
        priority: active
          ? channelMonitorSmartScheduleRouteRuntimePriority(route)
          : 0,
        weight: active
          ? channelMonitorSmartScheduleRouteRuntimeWeight(route)
          : 0,
      })
    }
  }
  let historicalDecision:
    | ChannelMonitorSmartScheduleScoreDetails['decision']
    | undefined
  let decisionTime = -1
  for (const route of routes) {
    const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
    const candidate = runtimeState.last_schedule_score_details?.decision
    if (candidate && runtimeState.last_schedule_time >= decisionTime) {
      historicalDecision = candidate
      decisionTime = runtimeState.last_schedule_time
    }
  }
  const currentDecision = routes
    .map((route) => route.current_window_score_details?.decision)
    .find((candidate) => candidate != null)
  const decision = currentDecision ?? historicalDecision
  let decisionSource: ChannelMonitorSmartSchedulePoolRoutingSnapshot['decisionSource'] =
    'none'
  if (currentDecision) decisionSource = 'current_window'
  else if (historicalDecision) decisionSource = 'last_schedule'

  const activeCandidates = [...candidatesById.values()].filter(
    (candidate) => candidate.activeRoutes.length > 0
  )
  const candidatesOutsideRateLimitCooldown = activeCandidates.filter(
    (candidate) =>
      candidate.activeRoutes.some(
        (route) => !channelMonitorSmartScheduleRouteIsRateLimitCoolingDown(route)
      )
  )
  const routableCandidates =
    candidatesOutsideRateLimitCooldown.length > 0
      ? candidatesOutsideRateLimitCooldown
      : activeCandidates
  const actualHighestPriority = routableCandidates.reduce<number | null>(
    (current, candidate) =>
      current == null ? candidate.priority : Math.max(current, candidate.priority),
    null
  )
  const actualTopLayerCandidates =
    actualHighestPriority == null
      ? []
      : routableCandidates.filter(
          (candidate) => candidate.priority === actualHighestPriority
        )
  const actualTopLayerChannelIds = actualTopLayerCandidates
    .map((candidate) =>
      candidate.routes[0]
        ? channelMonitorSmartScheduleRouteCandidateChannelId(candidate.routes[0])
        : 0
    )
    .filter((channelId) => channelId > 0)
    .sort((first, second) => first - second)
  const recordedTopLayer = [
    ...new Set(
      (decision?.actual_top_layer_channel_ids ?? []).filter(
        (channelId) => channelId > 0
      ).map((channelId) =>
        channelMonitorSmartScheduleRecordedCandidateId(routes, channelId)
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
    ...actualTopLayerCandidates.map((candidate) => Math.max(0, candidate.weight))
  )
  const largestWeightCount = actualTopLayerCandidates.filter(
    (candidate) => Math.max(0, candidate.weight) === largestWeight
  ).length
  const weightedPrimary = actualTopLayerCandidates.find(
    (candidate) =>
      actualTopLayerCandidates.length === 1 ||
      (Math.max(0, candidate.weight) === largestWeight && largestWeightCount === 1)
  )
  let actualPrimaryChannelId = weightedPrimary?.routes[0]
    ? channelMonitorSmartScheduleRouteCandidateChannelId(weightedPrimary.routes[0])
    : 0
  if (actualPrimaryChannelId === 0 && decisionMatchesCurrentRouting) {
    const recordedPrimaryChannelId =
      channelMonitorSmartScheduleRecordedCandidateId(
        routes,
        decision?.actual_primary_channel_id ?? 0
      )
    if (actualTopLayerCandidates.some((candidate) =>
      candidate.routes.some(
        (route) =>
          channelMonitorSmartScheduleRouteCandidateChannelId(route) ===
          recordedPrimaryChannelId
      )
    )) {
      actualPrimaryChannelId = recordedPrimaryChannelId
    }
  }

  return {
    decision,
    historicalDecision,
    decisionSource,
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
    const candidatesById = new Map<
      number,
      ChannelMonitorSmartScheduleRoute[]
    >()
    for (const route of poolRoutes) {
      const candidateId =
        channelMonitorSmartScheduleRouteCandidateChannelId(route)
      const candidateRoutes = candidatesById.get(candidateId)
      if (candidateRoutes) candidateRoutes.push(route)
      else candidatesById.set(candidateId, [route])
    }
    const candidates = [...candidatesById.entries()]
      .map(([candidateId, candidateRoutes]) => {
        const activeRoutes = candidateRoutes.filter(
          channelMonitorSmartScheduleRouteIsActive
        )
        const firstActiveRoute = activeRoutes[0]
        return {
          candidateId,
          routes: candidateRoutes,
          activeRoutes,
          priority: firstActiveRoute
            ? channelMonitorSmartScheduleRouteRuntimePriority(firstActiveRoute)
            : 0,
          weight: firstActiveRoute
            ? channelMonitorSmartScheduleRouteRuntimeWeight(firstActiveRoute)
            : 0,
        }
      })
      .filter(
        (candidate) =>
          actualTopLayerChannelIdSet.has(candidate.candidateId) &&
          candidate.activeRoutes.length > 0
      )
    const recordedScoringWinnerChannelId = channelMonitorSmartScheduleRecordedCandidateId(
      poolRoutes,
      snapshot.decision?.raw_winner_channel_id ?? 0
    )
    const scoringWinnerChannelId = poolRoutes.some(
      (route) =>
        channelMonitorSmartScheduleRouteCandidateChannelId(route) ===
          recordedScoringWinnerChannelId &&
        channelMonitorSmartScheduleRouteIsActive(route)
    )
      ? recordedScoringWinnerChannelId
      : 0
    const totalWeight = candidates.reduce(
      (total, candidate) =>
        total + Math.max(0, candidate.weight),
      0
    )
    const candidateShares = new Map<number, number>()
    for (const candidate of candidates) {
      const share =
        totalWeight > 0
          ? Math.max(0, candidate.weight) / totalWeight
          : 1 / candidates.length
      candidateShares.set(candidate.candidateId, share)
    }
    const shares = new Map<string, number>()
    for (const candidate of candidates) {
      const candidateShare = candidateShares.get(candidate.candidateId) ?? 0
      const memberRoutes = candidate.activeRoutes
      const totalMemberWeight = memberRoutes.reduce(
        (total, route) =>
          total +
          Math.max(
            0,
            channelMonitorSmartScheduleRouteCandidateMemberWeight(route)
          ),
        0
      )
      for (const route of candidate.routes) {
        if (!memberRoutes.includes(route)) {
          shares.set(channelMonitorSmartScheduleRouteKey(route), 0)
          continue
        }
        const memberWeight = Math.max(
          0,
          channelMonitorSmartScheduleRouteCandidateMemberWeight(route)
        )
        const memberShare =
          totalMemberWeight > 0
            ? memberWeight / totalMemberWeight
            : 1 / memberRoutes.length
        shares.set(
          channelMonitorSmartScheduleRouteKey(route),
          candidateShare * memberShare
        )
      }
    }
    const actualPrimaryChannelId = snapshot.actualPrimaryChannelId

    for (const route of poolRoutes) {
      let role: ChannelMonitorSmartScheduleRouteRole = 'backup'
      let estimatedShare: number | null = null
      const configured =
        route.enabled && route.channel_status === CHANNEL_STATUS.ENABLED
      const paused =
        configured && channelMonitorSmartScheduleRouteIsTrafficPaused(route)
      const rateLimited =
        configured &&
        !paused &&
        channelMonitorSmartScheduleRouteIsRateLimitCoolingDown(route)
      const candidateChannelId =
        channelMonitorSmartScheduleRouteCandidateChannelId(route)
      const isActualTopLayer = actualTopLayerChannelIdSet.has(candidateChannelId)
      const routeShare = shares.get(channelMonitorSmartScheduleRouteKey(route))
      if (!channelMonitorSmartScheduleRouteRuntimeParticipates(route)) {
        role = 'excluded'
        estimatedShare = 0
      } else if (!configured) {
        role = 'unavailable'
      } else if (paused) {
        role = 'paused'
        estimatedShare = 0
      } else if (rateLimited) {
        role = 'rate_limited'
        estimatedShare = isActualTopLayer ? (routeShare ?? 0) : 0
      } else if (isActualTopLayer) {
        estimatedShare = routeShare ?? 0
        role =
          candidateChannelId === actualPrimaryChannelId
            ? 'primary'
            : 'candidate'
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
        isActualPrimary: candidateChannelId === actualPrimaryChannelId,
        isScoringWinner: candidateChannelId === scoringWinnerChannelId,
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
  if (!channelMonitorSmartScheduleRouteRuntimeParticipates(route)) return 'excluded'
  if (!route.enabled || route.channel_status !== CHANNEL_STATUS.ENABLED) {
    return 'unavailable'
  }
  if (channelMonitorSmartScheduleRouteIsTrafficPaused(route)) return 'paused'
  if (channelMonitorSmartScheduleRouteIsRateLimitCoolingDown(route)) {
    return 'rate_limited'
  }
  const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
  if (runtimeState.stability_state === 'degraded') return 'degraded'
  if (runtimeState.stability_state === 'probing') return 'probing'
  if (runtimeState.temporary_traffic_kind === 'insufficient_samples') {
    return 'insufficient_samples'
  }
  if (runtimeState.temporary_traffic_kind === 'adaptive_sampling') {
    return 'adaptive_sampling'
  }
  if (runtimeState.last_schedule_status === 'failed') return 'failed'
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
  const priorityOrder =
    channelMonitorSmartScheduleRouteRuntimePriority(second) -
    channelMonitorSmartScheduleRouteRuntimePriority(first)
  if (priorityOrder !== 0) return priorityOrder
  const weightOrder =
    channelMonitorSmartScheduleRouteRuntimeWeight(second) -
    channelMonitorSmartScheduleRouteRuntimeWeight(first)
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
    const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
    const runtimePriority =
      channelMonitorSmartScheduleRouteRuntimePriority(route)
    const runtimeWeight = channelMonitorSmartScheduleRouteRuntimeWeight(route)
    const key = `${route.group}\u0000${route.model}`
    const poolRoutes = routesByPool.get(key)
    if (poolRoutes) poolRoutes.push(route)
    else routesByPool.set(key, [route])
    const participates = channelMonitorSmartScheduleRouteRuntimeParticipates(route)
    const paused =
      participates &&
      route.enabled &&
      route.channel_status === CHANNEL_STATUS.ENABLED &&
      channelMonitorSmartScheduleRouteIsTrafficPaused(route)
    const available =
      participates && channelMonitorSmartScheduleRouteIsAvailable(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    const existing = poolMap.get(key)
    if (!existing) {
      poolMap.set(key, {
        group: route.group,
        model: route.model,
        routeCount: 1,
        participatingCount: participates ? 1 : 0,
        activeCount: active ? 1 : 0,
        pausedCount: paused ? 1 : 0,
        priorityMin: runtimePriority,
        priorityMax: runtimePriority,
        weightMin: runtimeWeight,
        weightMax: runtimeWeight,
        degradedCount:
          available && runtimeState.stability_state === 'degraded' ? 1 : 0,
        probingCount:
          active && runtimeState.stability_state === 'probing' ? 1 : 0,
        insufficientSampleCount:
          active &&
          (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
            runtimeState.temporary_traffic_kind === 'adaptive_sampling')
            ? 1
            : 0,
        failedCount:
          active && runtimeState.last_schedule_status === 'failed' ? 1 : 0,
        breakEvenFallbackCount:
          route.economic_role === 'break_even_fallback' ? 1 : 0,
        breakEvenFallbackFixedCount:
          route.economic_role === 'break_even_fallback' &&
          runtimeState.manual_primary_until > 0
            ? 1
            : 0,
        breakEvenFallbackTakingOver: false,
        topPriority: active ? runtimePriority : null,
        candidateCount: active ? 1 : 0,
        scoringWinnerChannelId: 0,
        historicalScoringWinnerChannelId: 0,
        scoringWinnerSource: 'none',
        actualPrimaryChannelId: 0,
        actualHighestPriority: null,
        actualTopLayerChannelIds: [],
      })
      continue
    }
    existing.routeCount += 1
    if (participates) existing.participatingCount += 1
    if (active) existing.activeCount += 1
    if (paused) existing.pausedCount += 1
    existing.priorityMin = Math.min(existing.priorityMin, runtimePriority)
    existing.priorityMax = Math.max(existing.priorityMax, runtimePriority)
    existing.weightMin = Math.min(existing.weightMin, runtimeWeight)
    existing.weightMax = Math.max(existing.weightMax, runtimeWeight)
    if (available && runtimeState.stability_state === 'degraded') {
      existing.degradedCount += 1
    }
    if (active && runtimeState.stability_state === 'probing') {
      existing.probingCount += 1
    }
    if (
      active &&
      (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
        runtimeState.temporary_traffic_kind === 'adaptive_sampling')
    ) {
      existing.insufficientSampleCount += 1
    }
    if (active && runtimeState.last_schedule_status === 'failed') {
      existing.failedCount += 1
    }
    if (route.economic_role === 'break_even_fallback') {
      existing.breakEvenFallbackCount += 1
      if (runtimeState.manual_primary_until > 0) {
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
      topLayerChannelIdSet.has(
        channelMonitorSmartScheduleRouteCandidateChannelId(route)
      ) && channelMonitorSmartScheduleRouteIsActive(route)
    )
    const scoringWinnerChannelId = channelMonitorSmartScheduleRecordedCandidateId(
      poolRoutes,
      snapshot.decision?.raw_winner_channel_id ?? 0
    )
    const historicalScoringWinnerChannelId =
      channelMonitorSmartScheduleRecordedCandidateId(
        poolRoutes,
        snapshot.historicalDecision?.raw_winner_channel_id ?? 0
      )
    return {
      ...summary,
      topPriority: snapshot.actualHighestPriority,
      candidateCount: snapshot.actualTopLayerChannelIds.length,
      scoringWinnerChannelId:
        poolRoutes.some(
          (route) =>
            channelMonitorSmartScheduleRouteCandidateChannelId(route) ===
              scoringWinnerChannelId &&
            channelMonitorSmartScheduleRouteIsActive(route)
        )
          ? scoringWinnerChannelId
          : 0,
      historicalScoringWinnerChannelId:
        poolRoutes.some(
          (route) =>
            channelMonitorSmartScheduleRouteCandidateChannelId(route) ===
              historicalScoringWinnerChannelId &&
            channelMonitorSmartScheduleRouteIsActive(route)
        )
          ? historicalScoringWinnerChannelId
          : 0,
      scoringWinnerSource: snapshot.decisionSource,
      actualPrimaryChannelId: snapshot.actualPrimaryChannelId,
      actualHighestPriority: snapshot.actualHighestPriority,
      actualTopLayerChannelIds: snapshot.actualTopLayerChannelIds,
      breakEvenFallbackTakingOver:
        topLayerRoutes.length > 0 &&
        topLayerRoutes.every(
          (route) =>
            channelMonitorSmartScheduleRouteIsBreakEvenFallback(route) &&
            channelMonitorSmartScheduleRouteRuntimeState(route)
              .manual_primary_until <= 0
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
  pausedCount?: number
  degradedCount: number
  probingCount: number
  insufficientSampleCount: number
  failedCount?: number
  breakEvenFallbackTakingOver?: boolean
}): ChannelMonitorSmartSchedulePoolStatus {
  if (pool.participatingCount === 0) return '未参与调度'
  if ((pool.pausedCount ?? 0) > 0) {
    if (
      pool.activeCount === 0 &&
      (pool.pausedCount ?? 0) >= pool.participatingCount
    ) {
      return '流量已暂停'
    }
    return '部分流量暂停'
  }
  if (pool.degradedCount > 0) return '稳定性降级'
  if (pool.probingCount > 0) return '稳定性试放'
  if (pool.insufficientSampleCount > 0) return '统一采样'
  if ((pool.failedCount ?? 0) > 0) return '最近失败'
  if (pool.breakEvenFallbackTakingOver) return '保本兜底接管'
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
  let pausedCount = 0
  let degradedCount = 0
  let probingCount = 0
  let insufficientSampleCount = 0
  let failedCount = 0
  for (const route of routes) {
    channels.add(route.channel_id)
    groups.add(route.group)
    const runtimeState = channelMonitorSmartScheduleRouteRuntimeState(route)
    const participates = channelMonitorSmartScheduleRouteRuntimeParticipates(route)
    if (participates) {
      participatingCount += 1
    }
    const available =
      participates && channelMonitorSmartScheduleRouteIsAvailable(route)
    const active = channelMonitorSmartScheduleRouteIsActive(route)
    if (
      participates &&
      route.enabled &&
      route.channel_status === CHANNEL_STATUS.ENABLED &&
      channelMonitorSmartScheduleRouteIsTrafficPaused(route)
    ) {
      pausedCount += 1
    }
    if (active) activeCount += 1
    if (available && runtimeState.stability_state === 'degraded') {
      degradedCount += 1
    }
    if (active && runtimeState.stability_state === 'probing') {
      probingCount += 1
    }
    if (
      active &&
      (runtimeState.temporary_traffic_kind === 'insufficient_samples' ||
        runtimeState.temporary_traffic_kind === 'adaptive_sampling')
    ) {
      insufficientSampleCount += 1
    }
    if (active && runtimeState.last_schedule_status === 'failed') {
      failedCount += 1
    }
  }
  const pools = summarizeChannelMonitorSmartSchedulePools(routes)
  return {
    routeCount: routes.length,
    participatingCount,
    activeCount,
    pausedCount,
    channelCount: channels.size,
    groupCount: groups.size,
    poolCount: pools.length,
    healthyPoolCount: pools.filter(
      (pool) =>
        pool.activeCount > 0 &&
        pool.pausedCount === 0 &&
        pool.degradedCount === 0 &&
        pool.probingCount === 0 &&
        pool.insufficientSampleCount === 0 &&
        pool.failedCount === 0 &&
        !pool.breakEvenFallbackTakingOver
    ).length,
    degradedCount,
    probingCount,
    insufficientSampleCount,
    failedCount,
  }
}

export function isChannelMonitorSmartScheduleResultStale(
  generatedAt: number,
  nowSeconds = Date.now() / 1000
) {
  if (generatedAt <= 0) return false
  return nowSeconds - generatedAt > 10 * 60
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
