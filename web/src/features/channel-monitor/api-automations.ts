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
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

import type {
  ChannelMonitorApiResponse,
  ChannelMonitorCustomActionState,
  ChannelMonitorCustomUpstreamConfig,
  ChannelMonitorCustomVariable,
  NewAPIGroupRatioResult,
} from './types'

export type UpstreamAutomation = {
  id: string
  revision: number
  name: string
  enabled: boolean
  base_url: string
  proxy: string
  interval_minutes: number
  request_timeout: number
  channel_ids: number[]
  custom_config: ChannelMonitorCustomUpstreamConfig
  state: {
    revision: number
    last_check: number
    next_check: number
    lease_until?: number
    status: string
    message: string
    balance?: number
    ratio?: number
    failures: number
    actions: Record<string, ChannelMonitorCustomActionState>
    history:
      | { id: string; time: number; status: string; message: string }[]
      | null
  }
}

export const automationsQueryKey = ['upstream-automations']
const endpoint = '/api/channel_monitor/automations'
const requestOptions = { skipBusinessError: true, skipErrorHandler: true }

function result<T>(response: ChannelMonitorApiResponse<T>): T {
  if (!response.success) {
    throw new Error(response.message || '上游自动任务请求失败')
  }
  return response.data
}

export function useUpstreamAutomations() {
  return useQuery({
    queryKey: automationsQueryKey,
    queryFn: listUpstreamAutomations,
    refetchInterval: 5000,
  })
}

export async function listUpstreamAutomations() {
  const response = await api.get<
    ChannelMonitorApiResponse<UpstreamAutomation[]> & {
      migration_warning?: string
    }
  >(endpoint, requestOptions)
  return {
    tasks: result(response.data),
    migrationWarning: response.data.migration_warning || '',
  }
}

export async function saveUpstreamAutomation(task: UpstreamAutomation) {
  return result(
    (
      await api.put<ChannelMonitorApiResponse<UpstreamAutomation>>(
        endpoint,
        task,
        requestOptions
      )
    ).data
  )
}

export async function deleteUpstreamAutomation(task: UpstreamAutomation) {
  return result(
    (
      await api.delete<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${task.id}`,
        { ...requestOptions, params: { revision: task.revision } }
      )
    ).data
  )
}

export async function runUpstreamAutomation(task: UpstreamAutomation) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<{ task_id: string }>>(
        `${endpoint}/${task.id}/run`,
        {},
        requestOptions
      )
    ).data
  )
}

export async function acknowledgeUpstreamAutomation(request: {
  task: UpstreamAutomation
  actionId: string
  state: ChannelMonitorCustomActionState
}) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${request.task.id}/actions/${request.actionId}/acknowledge`,
        {
          revision: request.task.revision,
          attempt_id: request.state.attempt_id,
        },
        requestOptions
      )
    ).data
  )
}

export async function resetUpstreamAutomationAttempts(request: {
  task: UpstreamAutomation
  actionId: string
  state: ChannelMonitorCustomActionState
}) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${request.task.id}/actions/${request.actionId}/reset-count`,
        {
          revision: request.task.revision,
          day: request.state.day,
          attempts: request.state.attempts,
          last_attempt: request.state.last_attempt,
        },
        requestOptions
      )
    ).data
  )
}

export async function testUpstreamAutomation(task: UpstreamAutomation) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<NewAPIGroupRatioResult>>(
        `${endpoint}/test`,
        task,
        { ...requestOptions, timeout: 150000 }
      )
    ).data
  )
}

export async function fetchUpstreamAutomationVariables(
  task: UpstreamAutomation,
  requestId: string
) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<ChannelMonitorCustomVariable[]>>(
        `${endpoint}/variable/fetch`,
        { ...task, request_id: requestId },
        { ...requestOptions, timeout: 150000 }
      )
    ).data
  )
}
