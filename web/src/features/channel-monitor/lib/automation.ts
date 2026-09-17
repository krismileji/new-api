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
import { z } from 'zod'

import type { UpstreamAutomation } from '../api-automations'
import {
  createChannelMonitorCustomFormConfig,
  createChannelMonitorCustomRequestConfig,
} from './custom-upstream'
import type { UpstreamConfigFormValues } from './schema'
import { emptyVariableGroup, variableGroupFormValues } from './variable-group'

export function emptyUpstreamAutomation(): UpstreamAutomation {
  return {
    id: '',
    revision: 0,
    name: '',
    enabled: false,
    base_url: '',
    proxy: '',
    interval_minutes: 5,
    request_timeout: 30,
    channel_ids: [],
    custom_config: createChannelMonitorCustomRequestConfig(
      createChannelMonitorCustomFormConfig(undefined)
    ),
    state: {
      revision: 0,
      last_check: 0,
      next_check: 0,
      status: 'waiting',
      message: '',
      failures: 0,
      actions: {},
      history: [],
    },
  }
}

export function upstreamAutomationFormValues(
  task: UpstreamAutomation
): UpstreamConfigFormValues {
  return {
    ...variableGroupFormValues(emptyVariableGroup()),
    baseUrl: task.base_url,
    customConfig: createChannelMonitorCustomFormConfig(task.custom_config),
  }
}

export function upstreamAutomationPayload(
  task: UpstreamAutomation,
  values: UpstreamConfigFormValues
): UpstreamAutomation {
  return {
    ...task,
    // Form validation does not replace the raw strings returned by getValues().
    ...automationMetadataSchema.parse(task),
    base_url: values.baseUrl,
    custom_config: createChannelMonitorCustomRequestConfig(values.customConfig),
  }
}

export const automationMetadataSchema = z.object({
  account_id: z.coerce.number().int().min(0).optional(),
  ratio_channel_id: z.coerce.number().int().min(0).optional(),
  name: z
    .string()
    .trim()
    .min(1, '请输入任务名称')
    .max(80, '名称最多 80 个字符'),
  enabled: z.boolean(),
  proxy: z.string().trim().max(2048, '代理地址过长'),
  interval_minutes: z.coerce
    .number()
    .int()
    .min(1, '至少间隔 1 分钟')
    .max(10080, '最多间隔 10080 分钟'),
  request_timeout: z.coerce
    .number()
    .int()
    .min(1, '超时至少 1 秒')
    .max(120, '超时最多 120 秒'),
  channel_ids: z
    .array(z.number().int().positive())
    .max(100, '最多关联 100 个渠道'),
})
export type AutomationMetadata = z.infer<typeof automationMetadataSchema>
