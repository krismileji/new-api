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
  ChannelMonitorApplyGroupResult,
  ChannelMonitorApiResponse,
  ChannelMonitorCostOverview,
  ChannelMonitorEmailNotificationType,
  ChannelMonitorEmailPreview,
  ChannelMonitorFetchResult,
  ChannelMonitorGroupChannelsUpdateResult,
  ChannelMonitorGroupRatioSyncResult,
  ChannelMonitorOverview,
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorPerformanceResult,
  ChannelMonitorSettings,
  ChannelMonitorSmartScheduleStabilityClearResult,
  ChannelMonitorSmartSchedulePrimaryUpdateResult,
  ChannelMonitorSmartScheduleRouteResult,
  ChannelMonitorSuccessDetailResult,
  ChannelMonitorTodaySuccessResult,
  ChannelMonitorTaskRunResult,
  ChannelMonitorTaskPage,
  ChannelMonitorTaskKind,
  ChannelMonitorUpstreamBalanceResult,
  ChannelMonitorUpstreamConfig,
  ChannelMonitorUpstreamGroupsResult,
  ChannelMonitorUpstreamRequest,
  ChannelMonitorUpstreamVersionResult,
  ChannelRatioHistoryPage,
  NewAPIGroupRatioResult,
} from './types'

const channelMonitorRequestConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

function ensureChannelMonitorSuccess<T>(
  response: ChannelMonitorApiResponse<T>
) {
  if (!response.success) {
    throw new Error(response.message || '渠道监控请求失败')
  }
  return response
}

export async function getChannelMonitorOverview() {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorOverview>
  >('/api/channel_monitor/', channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorCostOverview(
  days: number,
  channelId?: number,
  page = 1,
  summaryOnly = false,
  date?: string
) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorCostOverview>
  >(
    '/api/channel_monitor/cost',
    channelMonitorRequestConfig({
      params: {
        days,
        channel_id: channelId,
        page,
        summary_only: summaryOnly || undefined,
        date,
      },
    })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorPerformance(
  minutes: ChannelMonitorPerformanceRangeMinutes
) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorPerformanceResult>
  >(
    '/api/channel_monitor/performance',
    channelMonitorRequestConfig({ params: { minutes } })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorSuccessDetail(request: {
  minutes: ChannelMonitorPerformanceRangeMinutes
  channelId?: number
  modelName?: string
  groupName?: string
}) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorSuccessDetailResult>
  >(
    '/api/channel_monitor/success/detail',
    channelMonitorRequestConfig({
      params: {
        minutes: request.minutes,
        channel_id: request.channelId,
        model_name: request.modelName,
        group: request.groupName,
      },
    })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorTodaySuccess(request?: {
  days: number
  date: string
}) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorTodaySuccessResult>
  >(
    '/api/channel_monitor/success/today',
    channelMonitorRequestConfig(
      request
        ? {
            params: {
              days: request.days,
              date: request.date,
            },
          }
        : undefined
    )
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorChannelOrder(channelIds: number[]) {
  const response = await api.put<
    ChannelMonitorApiResponse<{ channel_order: number[] }>
  >(
    '/api/channel_monitor/order',
    {
      channel_ids: channelIds,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorTasks(
  page: number,
  pageSize: number,
  kind: ChannelMonitorTaskKind
) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorTaskPage>
  >(
    '/api/channel_monitor/tasks',
    channelMonitorRequestConfig({
      params: { p: page, page_size: pageSize, kind },
    })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function runChannelMonitorSmartSchedule() {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorTaskRunResult>
  >(
    '/api/channel_monitor/schedule/run',
    undefined,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorSmartScheduleRoutes() {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleRouteResult>
  >('/api/channel_monitor/schedule', channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function runChannelMonitorRatioUpdate() {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorTaskRunResult>
  >('/api/channel_monitor/ratio/run', undefined, channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSmartScheduleChannelConfig(request: {
  channelId: number
  excluded: boolean
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<{ total: number; updated: number }>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/routes`,
    {
      excluded: request.excluded,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSmartScheduleRouteConfig(request: {
  channelId: number
  group: string
  model: string
  excluded: boolean
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<{
      channel_id: number
      group: string
      model: string
      excluded: boolean
    }>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route`,
    {
      group: request.group,
      model: request.model,
      excluded: request.excluded,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSmartScheduleManualRouting(request: {
  channelId: number
  group: string
  model: string
  priority: number
  weight: number
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<{
      channel_id: number
      group: string
      model: string
      priority: number
      weight: number
      routing_changed: boolean
    }>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/routing`,
    {
      group: request.group,
      model: request.model,
      priority: request.priority,
      weight: request.weight,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSmartScheduleRoutePrimary(request: {
  channelId: number
  group: string
  model: string
  durationMinutes: number
  allowStabilityDegrade: boolean
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorSmartSchedulePrimaryUpdateResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/primary`,
    {
      group: request.group,
      model: request.model,
      duration_minutes: request.durationMinutes,
      allow_stability_degrade: request.allowStabilityDegrade,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function clearChannelMonitorSmartScheduleRouteStability(request: {
  channelId: number
  group: string
  model: string
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleStabilityClearResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/stability/clear`,
    { group: request.group, model: request.model },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorRatio(request: {
  channelId: number
  ratio: number
  remark: string
}) {
  const response = await api.put(
    `/api/channel_monitor/channel/${request.channelId}`,
    {
      ratio: request.ratio,
      remark: request.remark,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorConcurrencyLimit(request: {
  channelId: number
  concurrencyLimit: number
}) {
  const response = await api.put(
    `/api/channel_monitor/channel/${request.channelId}/concurrency`,
    {
      concurrency_limit: request.concurrencyLimit,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorHistory(channelId: number) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelRatioHistoryPage>
  >(
    `/api/channel_monitor/channel/${channelId}/history`,
    channelMonitorRequestConfig({ params: { p: 1, page_size: 100 } })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorGroupRatio(request: {
  group: string
  ratio: number
}) {
  const response = await api.put(
    '/api/channel_monitor/group',
    request,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorGroupChannels(request: {
  group: string
  channelIds: number[]
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorGroupChannelsUpdateResult>
  >(
    '/api/channel_monitor/group/channels',
    {
      group: request.group,
      channel_ids: request.channelIds,
    },
    channelMonitorRequestConfig()
  )
  const result = ensureChannelMonitorSuccess(response.data)
  return {
    ...result,
    data: {
      ...result.data,
      channel_ids: result.data.channel_ids ?? [],
      added_channel_ids: result.data.added_channel_ids ?? [],
      removed_channel_ids: result.data.removed_channel_ids ?? [],
    },
  }
}

export async function syncChannelMonitorGroupRatio(request: {
  group: string
  coefficient: number
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorGroupRatioSyncResult>
  >('/api/channel_monitor/group/sync', request, channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSettings(
  settings: Partial<ChannelMonitorSettings> & {
    smart_schedule_force_reset?: boolean
  }
) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorSettings>
  >('/api/channel_monitor/settings', settings, channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function previewChannelMonitorNotificationEmail(request: {
  notificationTypes: ChannelMonitorEmailNotificationType[]
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorEmailPreview>
  >(
    '/api/channel_monitor/settings/email-preview',
    { notification_types: request.notificationTypes },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelMonitorAvailableGroups() {
  const response = await api.get<ChannelMonitorApiResponse<string[]>>(
    '/api/group/',
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateMonitoredChannelStatus(request: {
  channelId: number
  status: number
}) {
  const response = await api.post<ChannelMonitorApiResponse<boolean>>(
    `/api/channel/${request.channelId}/status`,
    { status: request.status },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateMonitoredChannelGroups(request: {
  channelId: number
  groups: string[]
}) {
  const response = await api.put<ChannelMonitorApiResponse<unknown>>(
    '/api/channel/',
    { id: request.channelId, group: request.groups.join(',') },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function saveChannelMonitorUpstreamConfig(request: {
  channelId: number
  config: ChannelMonitorUpstreamRequest
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorUpstreamConfig>
  >(
    `/api/channel_monitor/channel/${request.channelId}/upstream`,
    request.config,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function fetchChannelMonitorSub2APIUpstreamVersion(request: {
  channelId: number
  baseUrl: string
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorUpstreamVersionResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/upstream/version`,
    {
      base_url: request.baseUrl,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function testChannelMonitorUpstreamConfig(request: {
  channelId: number
  config: ChannelMonitorUpstreamRequest
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<NewAPIGroupRatioResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/upstream/test`,
    request.config,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function listChannelMonitorUpstreamGroups(request: {
  channelId: number
  config: ChannelMonitorUpstreamRequest
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorUpstreamGroupsResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/upstream/groups`,
    request.config,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function fetchChannelMonitorUpstreamRatio(channelId: number) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorFetchResult>
  >(
    `/api/channel_monitor/channel/${channelId}/upstream/fetch`,
    undefined,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function fetchChannelMonitorUpstreamBalance(channelId: number) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorUpstreamBalanceResult>
  >(
    `/api/channel_monitor/channel/${channelId}/upstream/balance/fetch`,
    undefined,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function applyChannelMonitorUpstreamGroup(channelId: number) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorApplyGroupResult>
  >(
    `/api/channel_monitor/channel/${channelId}/upstream/group/apply`,
    undefined,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}
