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
export const CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS = [
  {
    value: 'smart',
    label: '智能调度',
    description: '综合成本倍率、首字和 TPS',
  },
  {
    value: 'ratio',
    label: '按成本倍率',
    description: '倍率越低，调度得分越高',
  },
  {
    value: 'first_token',
    label: '按首字',
    description: '平均首字时间越低，调度得分越高',
  },
  {
    value: 'tps',
    label: '按 TPS',
    description: '平均 TPS 越高，调度得分越高',
  },
] as const

export const CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS = [
  {
    value: 'weight',
    label: '只调整权重',
    description: '保留现有优先级，让同层最高分渠道承接目标流量',
  },
  {
    value: 'priority_weight',
    label: '优先级分层 + 权重',
    description: '正常参与渠道按评分形成独立优先级，低优先级渠道可轮转采样',
  },
] as const

export function getChannelMonitorSmartScheduleStrategyLabel(
  value: string
): string {
  return (
    CHANNEL_MONITOR_SMART_SCHEDULE_STRATEGY_OPTIONS.find(
      (option) => option.value === value
    )?.label ?? value
  )
}

export function getChannelMonitorSmartScheduleApplyModeLabel(
  value: string
): string {
  return (
    CHANNEL_MONITOR_SMART_SCHEDULE_APPLY_MODE_OPTIONS.find(
      (option) => option.value === value
    )?.label ?? value
  )
}
