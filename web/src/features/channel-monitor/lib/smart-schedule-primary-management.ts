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
import {
  updateChannelMonitorSmartScheduleChannelPrimaryRule,
  updateChannelMonitorSmartScheduleRoutePrimary,
} from '../api'
import type {
  ChannelMonitorSmartScheduleChannelPrimaryRule,
  ChannelMonitorSmartScheduleManualPrimary,
  ChannelMonitorSmartSchedulePrimaryRuleRoute,
} from '../types'

export type PrimaryEntry =
  | (ChannelMonitorSmartScheduleChannelPrimaryRule & {
      kind: 'channel'
      key: string
    })
  | (ChannelMonitorSmartScheduleManualPrimary & { kind: 'route'; key: string })

export type PrimaryEdit = {
  entry: PrimaryEntry
  durationMinutes: number | null
  preserveUntil?: boolean
  allowStabilityDegrade: boolean
  confirmStabilityOverride?: boolean
}

export async function updatePrimaryEntry(edit: PrimaryEdit): Promise<void> {
  const request = {
    channelId: edit.entry.channel_id,
    group: edit.entry.group,
    durationMinutes: edit.durationMinutes,
    preserveUntil: edit.preserveUntil,
    allowStabilityDegrade: edit.allowStabilityDegrade,
    confirmStabilityOverride: edit.confirmStabilityOverride,
  }
  if (edit.entry.kind === 'channel') {
    await updateChannelMonitorSmartScheduleChannelPrimaryRule(request)
    return
  }
  await updateChannelMonitorSmartScheduleRoutePrimary({
    ...request,
    model: edit.entry.model,
  })
}

export function primaryRouteStatus(
  route: ChannelMonitorSmartSchedulePrimaryRuleRoute
) {
  switch (route.status) {
    case 'active':
      return '已生效'
    case 'not_eligible':
      return '未参与调度或已禁用'
    case 'single_model_override':
      return `被单模型固定覆盖：${route.current_primary_channel_name}`
    case 'channel_rule_conflict':
      return `被渠道固定覆盖：${route.current_primary_channel_name}`
    case 'stability_protected':
      return '稳定性保护中，暂未固定'
    default:
      return '等待路由参与调度'
  }
}

export function primaryEntryStatus(entry: PrimaryEntry) {
  if (entry.kind === 'route') return 'route'
  if (entry.routes.length === 0) return 'pending'
  const active = entry.routes.filter(
    (route) => route.status === 'active'
  ).length
  if (active === entry.routes.length) return 'active'
  return active > 0 ? 'partial' : 'inactive'
}
