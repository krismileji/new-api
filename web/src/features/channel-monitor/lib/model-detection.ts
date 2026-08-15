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
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionCost,
  ChannelModelDetectionFilters,
  ChannelModelDetectionHealth,
  ChannelModelDetectionKnownOutcomeCode,
  ChannelModelDetectionOutcomeCode,
  ChannelModelDetectionPreset,
  ChannelModelDetectionPresetSource,
  ChannelModelDetectionRunStatus,
  ChannelModelDetectionStatusFilter,
} from '../types-model-detection'

export const CHANNEL_MODEL_DETECTION_ENDPOINTS = {
  overview: '/api/channel_monitor/model_detection',
  settings: '/api/channel_monitor/model_detection/settings',
  service: '/api/channel_monitor/model_detection/service',
  serviceTest: '/api/channel_monitor/model_detection/service/test',
  channelConfig: (channelId: number) =>
    `/api/channel_monitor/model_detection/channel/${channelId}/config`,
  channelRun: (channelId: number) =>
    `/api/channel_monitor/model_detection/channel/${channelId}/run`,
  channelEstimate: (channelId: number) =>
    `/api/channel_monitor/model_detection/channel/${channelId}/estimate`,
  channelRuns: (channelId: number) =>
    `/api/channel_monitor/model_detection/channel/${channelId}/runs`,
  run: (runId: string) => `/api/channel_monitor/model_detection/runs/${runId}`,
  cancelRun: (runId: string) =>
    `/api/channel_monitor/model_detection/runs/${runId}/cancel`,
} as const

export const CHANNEL_MODEL_DETECTION_STATUS_FILTERS: ReadonlyArray<
  readonly [ChannelModelDetectionStatusFilter, string]
> = [
  ['all', '全部'],
  ['issue', '异常'],
  ['attention', '需关注'],
  ['running', '检测中'],
  ['healthy', '正常'],
  ['paused', '已暂停'],
  ['unconfigured', '未配置'],
]

export const CHANNEL_MODEL_DETECTION_SORT_OPTIONS = [
  { value: 'ratio_asc', label: '成本倍率：从低到高' },
  { value: 'ratio_desc', label: '成本倍率：从高到低' },
  { value: 'latest_desc', label: '最近检测：从新到旧' },
  { value: 'latest_asc', label: '最近检测：从旧到新' },
  { value: 'issue_first', label: '异常优先' },
  { value: 'schedule_first', label: '已参加定时优先' },
  { value: 'channel_id_asc', label: '渠道 ID：从小到大' },
] as const

const KNOWN_OUTCOME_CODES = new Set<ChannelModelDetectionKnownOutcomeCode>([
  'juice_pass_fingerprint_strong',
  'juice_pass_fingerprint_unclear',
  'juice_mismatch_fingerprint_strong',
  'juice_mismatch_fingerprint_unclear',
  'juice_insufficient_fingerprint_strong',
  'juice_insufficient_fingerprint_unclear',
  'possible_non_gpt',
])

const HEALTH_PRIORITY: Record<ChannelModelDetectionHealth, number> = {
  running: 9,
  unhealthy: 8,
  attention: 7,
  detector_unavailable: 6,
  stale: 5,
  healthy: 4,
  pending: 3,
  paused: 2,
  unconfigured: 1,
}

export function isKnownChannelModelDetectionOutcome(
  outcome: ChannelModelDetectionOutcomeCode
): outcome is ChannelModelDetectionKnownOutcomeCode {
  return KNOWN_OUTCOME_CODES.has(
    outcome as ChannelModelDetectionKnownOutcomeCode
  )
}

export function channelModelDetectionPresetLabel(
  preset: ChannelModelDetectionPreset
) {
  if (preset === 'low') return '低档'
  if (preset === 'high') return '高档'
  return '中档'
}

export function channelModelDetectionPresetSourceLabel(
  source: ChannelModelDetectionPresetSource
) {
  return source === 'manual_selected' ? '手动' : '定时'
}

export function channelModelDetectionRunStatusLabel(
  status: ChannelModelDetectionRunStatus
) {
  if (status === 'queued') return '排队中'
  if (status === 'waiting_detector') return '等待检测器'
  if (status === 'submitting') return '提交中'
  if (status === 'submission_unknown') return '启动待确认'
  if (status === 'running') return '检测中'
  if (status === 'canceling') return '取消中'
  if (status === 'completed') return '已完成'
  if (status === 'partial') return '部分完成'
  if (status === 'failed') return '失败'
  if (status === 'external_session_conflict') return '外部会话冲突'
  return '已取消'
}

export function channelModelDetectionClaimedModelLabel(model: string) {
  if (model === 'gpt-5.6-sol') return 'Sol'
  if (model === 'gpt-5.6-terra') return 'Terra'
  if (model === 'gpt-5.6-luna') return 'Luna'
  return model
}

export function channelModelDetectionLatestAt(
  channel: ChannelModelDetectionChannel
) {
  let latestAt = channel.active_run?.updated_at ?? 0
  for (const target of channel.targets) {
    const targetLatestAt =
      target.latest?.finished_at || target.latest?.updated_at || 0
    latestAt = Math.max(latestAt, targetLatestAt)
  }
  return latestAt
}

export function formatChannelModelDetectionRelativeTime(
  timestamp: number,
  serverNow: number
) {
  if (timestamp <= 0) return '尚未检测'
  const difference = timestamp - serverNow
  const seconds = Math.abs(difference)
  if (seconds < 60) return difference > 0 ? '不到 1 分钟后' : '刚刚'
  const suffix = difference > 0 ? '后' : '前'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟${suffix}`
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)} 小时${suffix}`
  return `${Math.floor(seconds / 86_400)} 天${suffix}`
}

export function channelModelDetectionCostLines(
  cost: ChannelModelDetectionCost | null
) {
  if (!cost) return ['尚无成本记录']

  const lines: string[] = []
  if (cost.settled_request_count > 0) {
    if (cost.settled_cost_cny == null) {
      lines.push(`已结算请求 ${cost.settled_request_count} 次 · 金额暂无法估算`)
    } else {
      const quota = cost.settled_quota.toLocaleString('zh-CN')
      lines.push(`已结算成本 ¥${cost.settled_cost_cny} · 额度 ${quota}`)
    }
  }

  if (cost.unresolved_request_count > 0) {
    if (cost.unresolved_cost_cny == null) {
      lines.push(`暂无法估算 · ${cost.unresolved_request_count} 次请求`)
    } else {
      const unknown = cost.unresolved_cost_unknown_count
        ? ` · ${cost.unresolved_cost_unknown_count} 次暂无法估算`
        : ''
      lines.push(`待核实预计成本 ¥${cost.unresolved_cost_cny}${unknown}`)
    }
  }

  if (
    cost.unresolved_cost_unknown_count > 0 &&
    cost.unresolved_request_count === 0
  ) {
    lines.push(`暂无法估算 · ${cost.unresolved_cost_unknown_count} 次请求`)
  }

  if (lines.length > 0) return lines
  if (cost.status === 'pending') return ['成本结算中']
  if (cost.status === 'not_started') return ['尚未发出上游请求']
  return ['已结算成本 ¥0.000000000']
}

export function isChannelModelDetectionRunActive(
  status: ChannelModelDetectionRunStatus
) {
  return (
    status === 'queued' ||
    status === 'waiting_detector' ||
    status === 'submitting' ||
    status === 'submission_unknown' ||
    status === 'running' ||
    status === 'canceling'
  )
}

export function isChannelModelDetectionIssue(
  health: ChannelModelDetectionHealth
) {
  return health === 'unhealthy' || health === 'detector_unavailable'
}

function matchesStatus(
  health: ChannelModelDetectionHealth,
  filter: ChannelModelDetectionStatusFilter
) {
  if (filter === 'all') return true
  if (filter === 'issue') return isChannelModelDetectionIssue(health)
  if (filter === 'attention') {
    return health === 'attention' || health === 'stale'
  }
  return health === filter
}

export function filterChannelModelDetectionChannels(
  channels: ChannelModelDetectionChannel[],
  filters: ChannelModelDetectionFilters
) {
  const search = filters.search.trim().toLocaleLowerCase()
  return channels.filter((channel) => {
    if (
      filters.onlyConfigured &&
      (!channel.config || !channel.targets.some((target) => target.enabled))
    ) {
      return false
    }
    if (!matchesStatus(channel.health_status, filters.status)) return false
    if (filters.group && !channel.groups.includes(filters.group)) return false
    if (
      filters.model &&
      !channel.supported_models.includes(filters.model) &&
      !channel.targets.some((target) => target.request_model === filters.model)
    ) {
      return false
    }
    if (!search) return true
    return (
      channel.name.toLocaleLowerCase().includes(search) ||
      channel.remark.toLocaleLowerCase().includes(search) ||
      String(channel.id).includes(search)
    )
  })
}

export function sortChannelModelDetectionChannels(
  channels: ChannelModelDetectionChannel[],
  sort: ChannelModelDetectionFilters['sort']
) {
  return [...channels].sort((left, right) => {
    if (sort === 'ratio_asc' || sort === 'ratio_desc') {
      const leftRatio = Number.isFinite(left.cost_ratio)
        ? left.cost_ratio
        : null
      const rightRatio = Number.isFinite(right.cost_ratio)
        ? right.cost_ratio
        : null
      let ratioDifference = 0
      if (leftRatio == null && rightRatio != null) ratioDifference = 1
      if (leftRatio != null && rightRatio == null) ratioDifference = -1
      if (leftRatio != null && rightRatio != null) {
        ratioDifference =
          sort === 'ratio_asc' ? leftRatio - rightRatio : rightRatio - leftRatio
      }
      if (ratioDifference) return ratioDifference
      const nameDifference = left.name.localeCompare(right.name, 'zh-CN', {
        numeric: true,
        sensitivity: 'base',
      })
      return nameDifference || left.id - right.id
    }
    if (sort === 'channel_id_asc') return left.id - right.id
    if (sort === 'schedule_first') {
      const scheduleDifference =
        Number(Boolean(right.config?.schedule_enabled)) -
        Number(Boolean(left.config?.schedule_enabled))
      return scheduleDifference || left.id - right.id
    }
    if (sort === 'issue_first') {
      const statusDifference =
        HEALTH_PRIORITY[right.health_status] -
        HEALTH_PRIORITY[left.health_status]
      return statusDifference || left.id - right.id
    }
    const latestDifference =
      channelModelDetectionLatestAt(left) - channelModelDetectionLatestAt(right)
    if (sort === 'latest_asc') return latestDifference || left.id - right.id
    return -latestDifference || left.id - right.id
  })
}
