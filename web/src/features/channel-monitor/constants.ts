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
  ChannelMonitorPolicyAction,
  ChannelMonitorSmartScheduleScoring,
  ChannelMonitorUpstreamAuthType,
  ChannelMonitorUpstreamType,
} from './types'

export const DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_SCORING: ChannelMonitorSmartScheduleScoring =
  {
    stability_percent: 50,
    primary_traffic_percent: 90,
    primary_switch_threshold_percent: 3,
    smart: {
      cost_ratio_percent: 40,
      first_token_percent: 40,
      tps_percent: 20,
    },
    ratio: {
      cost_ratio_percent: 70,
      first_token_percent: 20,
      tps_percent: 10,
    },
  }

export const DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_POLICY_CONTROLS = {
  jitterTolerancePercent: 5,
  jitterSlowThresholdSeconds: 10,
  minSamples: 5,
  recoveryStabilityScore: 95,
  fastFailurePenaltyPercent: 40,
  fastFailureSeconds: 1,
  fastFailureSameChannelRetryCount: 0,
  fastFailureSameChannelRetryDelayMs: 1_000,
  slowFailureSeconds: 10,
  burstFailureWindowSeconds: 30,
  consecutiveFailureThreshold: 2,
  burstFailureThreshold: 3,
  recoverySuccessThreshold: 2,
  cooldownMinutes: 30,
  explorationTrafficPercent: 3,
  explorationMaxPromptTokens: 16_384,
  stabilityReleaseMaxPromptTokens: 0,
  probeIntervalMinutes: 10,
  degradedProbeEnabled: false,
  prioritySamplingIntervalMinutes: 10,
  prioritySamplingBasePercent: 3,
  prioritySamplingDecayPercent: 70,
  prioritySamplingMinPercent: 0.5,
  adaptiveSamplingEnabled: true,
  adaptiveSamplingBasePercent: 3,
  adaptiveSamplingMaxPercent: 30,
  adaptiveSamplingPrimaryMinPercent: 70,
  adaptiveSamplingErrorWarningPercent: 5,
  adaptiveSamplingErrorCriticalPercent: 15,
  adaptiveSamplingFirstTokenWarningSeconds: 5,
  adaptiveSamplingFirstTokenCriticalSeconds: 10,
  adaptiveSamplingWindowSeconds: 600,
  adaptiveSamplingEnterRequestPercent: 10,
  adaptiveSamplingRecoverRequestPercent: 95,
  adaptiveSamplingSwitchConfirmRequestPercent: 95,
  adaptiveSamplingMinComparableChannels: 2,
} as const

export const CHANNEL_MONITOR_STATUS_LABELS: Partial<Record<number, string>> = {
  0: '未知',
  1: '已启用',
  2: '手动禁用',
  3: '系统禁用',
}

export function getChannelMonitorStatusLabel(status: number): string {
  return CHANNEL_MONITOR_STATUS_LABELS[status] ?? '未知状态'
}

export const CHANNEL_MONITOR_POLICY_ACTION_LABELS: Record<
  ChannelMonitorPolicyAction,
  string
> = {
  none: '仅记录',
  update_group_ratio: '更新分组倍率',
  disable_channel: '禁用渠道',
  remove_from_group: '移除当前渠道',
}

export const CHANNEL_MONITOR_UPSTREAM_TYPE_LABELS: Record<
  ChannelMonitorUpstreamType,
  string
> = {
  new_api: 'New API',
  sub2api: 'Sub2API',
  custom: '自定义上游',
}

export const CHANNEL_MONITOR_UPSTREAM_AUTH_LABELS: Record<
  ChannelMonitorUpstreamAuthType,
  string
> = {
  public: '公开接口',
  user: '账号登录',
  api_key: 'API Key（新版）',
  account: '账号密码登录',
  token: '手动 Token',
  custom: '自定义请求',
}
