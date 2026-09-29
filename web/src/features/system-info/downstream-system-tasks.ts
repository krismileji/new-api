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
import type { TFunction } from 'i18next'

import { SYSTEM_TASK_TYPE_LABEL } from './constants'

const DOWNSTREAM_TASK_LABELS: Record<string, string> = {
  channel_ratio_monitor: '渠道监控',
  channel_model_detection: '渠道模型检测',
  channel_smart_schedule: '智能调度',
  channel_smart_schedule_probe: '智能调度探测',
  channel_monitor_cost_retention: '监控历史清理',
  upstream_account_balance: '上游账户余额',
  upstream_automation: '上游自动任务',
  channel_local_response_refund: '本地响应退款',
}

export const SYSTEM_TASK_TYPES = [
  ...Object.keys(SYSTEM_TASK_TYPE_LABEL),
  ...Object.keys(DOWNSTREAM_TASK_LABELS),
]

export function getSystemTaskTypeLabel(type: string, t: TFunction): string {
  return DOWNSTREAM_TASK_LABELS[type] ?? t(SYSTEM_TASK_TYPE_LABEL[type] ?? type)
}
