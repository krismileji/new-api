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
import { api, type ApiRequestConfig } from '@/lib/api'

import type {
  ChannelGroupMonitorApiResponse,
  ChannelGroupMonitorExecutionPage,
  ChannelGroupMonitorGroup,
  ChannelGroupMonitorOverview,
  ChannelGroupMonitorSettings,
  ChannelGroupMonitorSettingsResponse,
  PricingGroupMonitor,
} from './types'

const groupMonitorRequestConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

function ensureGroupMonitorSuccess<T>(
  response: ChannelGroupMonitorApiResponse<T>
) {
  if (!response.success) {
    throw new Error(response.message || '分组监控请求失败')
  }
  return response
}

export async function getPricingGroupMonitor(): Promise<
  ChannelGroupMonitorApiResponse<PricingGroupMonitor>
> {
  const response = await api.get<
    ChannelGroupMonitorApiResponse<PricingGroupMonitor>
  >('/api/pricing/group-monitor', groupMonitorRequestConfig())
  return ensureGroupMonitorSuccess(response.data)
}

export async function getChannelGroupMonitorSettings(): Promise<
  ChannelGroupMonitorApiResponse<ChannelGroupMonitorSettingsResponse>
> {
  const response = await api.get<
    ChannelGroupMonitorApiResponse<ChannelGroupMonitorSettingsResponse>
  >('/api/channel_monitor/group_monitor/settings', groupMonitorRequestConfig())
  return ensureGroupMonitorSuccess(response.data)
}

export async function getChannelGroupMonitorOverview(): Promise<
  ChannelGroupMonitorApiResponse<ChannelGroupMonitorOverview>
> {
  const response = await api.get<
    ChannelGroupMonitorApiResponse<ChannelGroupMonitorOverview>
  >('/api/channel_monitor/group_monitor/overview', groupMonitorRequestConfig())
  return ensureGroupMonitorSuccess(response.data)
}

export async function updateChannelGroupMonitorSettings(request: {
  enabled: boolean
  groups: ChannelGroupMonitorGroup[]
  intervalSeconds: number
  displayValue: number
  displayUnit: ChannelGroupMonitorSettings['display_unit']
  revision: number
}): Promise<ChannelGroupMonitorApiResponse<ChannelGroupMonitorSettings>> {
  const response = await api.put<
    ChannelGroupMonitorApiResponse<ChannelGroupMonitorSettings>
  >(
    '/api/channel_monitor/group_monitor/settings',
    {
      enabled: request.enabled,
      groups: request.groups,
      interval_seconds: request.intervalSeconds,
      display_value: request.displayValue,
      display_unit: request.displayUnit,
      revision: request.revision,
    },
    groupMonitorRequestConfig()
  )
  return ensureGroupMonitorSuccess(response.data)
}

export async function runChannelGroupMonitorNow(): Promise<
  ChannelGroupMonitorApiResponse<{ manual_request_id: string }>
> {
  const response = await api.post<
    ChannelGroupMonitorApiResponse<{ manual_request_id: string }>
  >(
    '/api/channel_monitor/group_monitor/run',
    undefined,
    groupMonitorRequestConfig()
  )
  return ensureGroupMonitorSuccess(response.data)
}

export async function getChannelGroupMonitorExecutions(request: {
  page?: number
  pageSize?: number
  group?: string
  result?: string
}): Promise<ChannelGroupMonitorApiResponse<ChannelGroupMonitorExecutionPage>> {
  const response = await api.get<
    ChannelGroupMonitorApiResponse<ChannelGroupMonitorExecutionPage>
  >('/api/channel_monitor/group_monitor/executions', {
    ...groupMonitorRequestConfig(),
    params: {
      page: request.page,
      page_size: request.pageSize,
      group: request.group || undefined,
      result: request.result || undefined,
    },
  })
  return ensureGroupMonitorSuccess(response.data)
}
