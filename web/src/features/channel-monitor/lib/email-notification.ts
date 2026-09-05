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
import type { ChannelMonitorEmailNotificationType } from '../types'

export const CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES = [
  'ratio_change',
  'balance_warning',
  'channel_disabled',
  'group_membership_removed',
  'upstream_sync_failed',
  'task_failed',
  'monitoring_health',
] as const satisfies readonly ChannelMonitorEmailNotificationType[]

export const DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES: ChannelMonitorEmailNotificationType[] =
  [...CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES]

export const CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPE_OPTIONS = [
  {
    value: 'ratio_change',
    label: '渠道倍率变更',
    description: '上游倍率或换算后的成本倍率发生变化',
  },
  {
    value: 'balance_warning',
    label: '上游余额预警',
    description: '上游余额达到配置的预警阈值',
  },
  {
    value: 'channel_disabled',
    label: '渠道自动禁用',
    description: '倍率、余额或连续更新失败触发自动禁用',
  },
  {
    value: 'group_membership_removed',
    label: '渠道移出分组',
    description: '倍率策略自动解除渠道与分组的关联',
  },
  {
    value: 'upstream_sync_failed',
    label: '上游同步失败',
    description: '渠道在完成重试后仍未同步成功',
  },
  {
    value: 'task_failed',
    label: '定时任务失败',
    description: '任务执行或分组倍率写入失败',
  },
  {
    value: 'monitoring_health',
    label: '监控链路异常',
    description: 'Redis 不可用、队列满、Stream 积压或监控消费者异常',
  },
] as const satisfies ReadonlyArray<{
  value: ChannelMonitorEmailNotificationType
  label: string
  description: string
}>
