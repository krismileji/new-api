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
import type { ChannelMonitorSmartScheduleRoutePlacement } from './smart-schedule-summary'

export type ChannelMonitorSmartScheduleTemporaryTrafficKind =
  | ''
  | 'insufficient_samples'
  | 'priority_sampling'

export function getChannelMonitorSmartScheduleTemporaryTrafficLabel(
  kind: ChannelMonitorSmartScheduleTemporaryTrafficKind
) {
  if (kind === 'insufficient_samples') return '样本不足补量'
  if (kind === 'priority_sampling') return '低优先级轮转'
  return '无临时流量'
}

export function formatChannelMonitorSmartScheduleTemporaryTraffic(
  kind: ChannelMonitorSmartScheduleTemporaryTrafficKind,
  targetPercent: number
) {
  if (!kind) return '-'
  return `${getChannelMonitorSmartScheduleTemporaryTrafficLabel(kind)} · 目标 ${targetPercent.toFixed(1)}%`
}

export function formatChannelMonitorSmartScheduleEstimatedShare(
  placement: ChannelMonitorSmartScheduleRoutePlacement | undefined
) {
  if (placement?.estimatedShare != null) {
    return `${(placement.estimatedShare * 100).toFixed(1)}%`
  }
  if (placement?.role === 'backup') {
    return '0%'
  }
  return '-'
}
