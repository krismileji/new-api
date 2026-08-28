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
  ChannelMonitorConcurrencyOverview,
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
  ChannelMonitorSmartScheduleExplorationClearResult,
  ChannelMonitorSmartScheduleStabilityClearResult,
  ChannelMonitorSmartSchedulePrimaryUpdateResult,
  ChannelMonitorSmartScheduleRouteResult,
  ChannelMonitorSmartScheduleGroupPauseResult,
  ChannelMonitorSmartScheduleRateLimitCooldownResult,
  ChannelMonitorSmartScheduleExecutionDetailPage,
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
  ChannelStatusProbeConfig,
  ChannelStatusProbeDisplayUnit,
  ChannelStatusProbeExecutionPage,
  ChannelStatusProbeOverview,
  ChannelStatusProbeResult,
  ChannelStatusProbeTrigger,
  NewAPIGroupRatioResult,
} from './types'
import type {
  ChannelModelDetectionApiResponse,
  ChannelModelDetectionOverview,
} from './types-model-detection'

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

export async function getChannelMonitorConcurrency() {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorConcurrencyOverview>
  >('/api/channel_monitor/concurrency', channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelModelDetectionOverview() {
  const response = await api.get<
    ChannelModelDetectionApiResponse<ChannelModelDetectionOverview>
  >('/api/channel_monitor/model_detection', channelMonitorRequestConfig())
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelStatusProbeOverview(model?: string) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelStatusProbeOverview>
  >(
    '/api/channel_monitor/status',
    channelMonitorRequestConfig({ params: { model: model || undefined } })
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelStatusProbeConfig(request: {
  channelId: number
  enabled: boolean
  models: string[]
  intervalSeconds: number
  displayValue: number
  displayUnit: ChannelStatusProbeDisplayUnit
  recordSample: boolean
  revision: number
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelStatusProbeConfig>
  >(
    `/api/channel_monitor/status/channel/${request.channelId}/config`,
    {
      enabled: request.enabled,
      models: request.models,
      interval_seconds: request.intervalSeconds,
      display_value: request.displayValue,
      display_unit: request.displayUnit,
      record_sample: request.recordSample,
      revision: request.revision,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function runChannelStatusProbe(channelId: number) {
  const response = await api.post<
    ChannelMonitorApiResponse<{ manual_request_id: string }>
  >(
    `/api/channel_monitor/status/channel/${channelId}/run`,
    undefined,
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function getChannelStatusProbeExecutions(request: {
  channelId: number
  page: number
  pageSize: number
  model?: string
  result?: '' | ChannelStatusProbeResult
  trigger?: '' | ChannelStatusProbeTrigger
}) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelStatusProbeExecutionPage>
  >(
    `/api/channel_monitor/status/channel/${request.channelId}/executions`,
    channelMonitorRequestConfig({
      params: {
        page: request.page,
        page_size: request.pageSize,
        model: request.model || undefined,
        result: request.result || undefined,
        trigger: request.trigger || undefined,
      },
    })
  )
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

export async function getChannelMonitorSmartScheduleExecutionDetails(
  taskId: string,
  request: {
    page: number
    pageSize: number
    search?: string
    group?: string
    model?: string
    action?: string
  }
) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleExecutionDetailPage>
  >(
    `/api/channel_monitor/tasks/${encodeURIComponent(taskId)}/details`,
    channelMonitorRequestConfig({
      params: {
        p: request.page,
        page_size: request.pageSize,
        q: request.search || undefined,
        group: request.group || undefined,
        model: request.model || undefined,
        action: request.action || undefined,
      },
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

export async function getChannelMonitorSmartScheduleRoutes(
  metrics: boolean = true
) {
  const response = await api.get<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleRouteResult>
  >(
    '/api/channel_monitor/schedule',
    channelMonitorRequestConfig({ params: { metrics } })
  )
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

export async function updateChannelMonitorSmartScheduleGroupPause(request: {
  channelId: number
  group: string
  model: string
  durationMinutes: number
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleGroupPauseResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/pause`,
    {
      group: request.group,
      model: request.model,
      duration_minutes: request.durationMinutes,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export async function updateChannelMonitorSmartScheduleRateLimitCooldown(request: {
  channelId: number
  group: string
  model: string
  durationSeconds: number
}) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleRateLimitCooldownResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/rate-limit-cooldown`,
    {
      group: request.group,
      model: request.model,
      duration_seconds: request.durationSeconds,
    },
    channelMonitorRequestConfig()
  )
  return ensureChannelMonitorSuccess(response.data)
}

export type ChannelMonitorSmartSchedulePrimaryUpdateRequest = {
  channelId: number
  group: string
  model: string
  durationMinutes: number
  allowStabilityDegrade: boolean
  confirmStabilityOverride?: boolean
}

export class ChannelMonitorSmartScheduleStabilityConfirmationRequiredError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ChannelMonitorSmartScheduleStabilityConfirmationRequiredError'
  }
}

export async function updateChannelMonitorSmartScheduleRoutePrimary(
  request: ChannelMonitorSmartSchedulePrimaryUpdateRequest
) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorSmartSchedulePrimaryUpdateResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/primary`,
    {
      group: request.group,
      model: request.model,
      duration_minutes: request.durationMinutes,
      allow_stability_degrade: request.allowStabilityDegrade,
      confirm_stability_override:
        request.confirmStabilityOverride === true ? true : undefined,
    },
    channelMonitorRequestConfig()
  )
  if (
    !response.data.success &&
    response.data.code ===
      'smart_schedule_route_stability_confirmation_required'
  ) {
    throw new ChannelMonitorSmartScheduleStabilityConfirmationRequiredError(
      response.data.message
    )
  }
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

export async function clearChannelMonitorSmartScheduleRouteExploration(request: {
  channelId: number
  group: string
  model: string
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<ChannelMonitorSmartScheduleExplorationClearResult>
  >(
    `/api/channel_monitor/channel/${request.channelId}/schedule/route/exploration/clear`,
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
  return ensureChannelMonitorSuccess(response.data)
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
