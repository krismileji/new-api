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
    curve_exponent: 1,
    relative_weight_enabled: true,
    relative_weight_start_percent: 3,
    relative_weight_full_percent: 10,
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
