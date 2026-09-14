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
  ChannelMonitorCustomVariableRequest,
} from './types'

export type ChannelMonitorVariableGroup = {
  id: number
  name: string
  base_url: string
  proxy: string
  request_timeout: number
  revision: number
  variable_requests: ChannelMonitorCustomVariableRequest[]
  source_channel_id?: number
}

export const variableGroupsQueryKey = ['channel-monitor-variable-groups']
const requestConfig = { skipBusinessError: true, skipErrorHandler: true }
const endpoint = '/api/channel_monitor/variable_groups'

function result<T>(response: ChannelMonitorApiResponse<T>): T {
  if (!response.success) throw new Error(response.message || '共享配置请求失败')
  return response.data
}

export function useChannelMonitorVariableGroups(enabled = true) {
  return useQuery({
    queryKey: variableGroupsQueryKey,
    queryFn: async () => {
      const response = await api.get<
        ChannelMonitorApiResponse<ChannelMonitorVariableGroup[]>
      >(endpoint, requestConfig)
      return result(response.data)
    },
    enabled,
  })
}

export async function saveChannelMonitorVariableGroup(
  group: ChannelMonitorVariableGroup
) {
  const response = await api.put<
    ChannelMonitorApiResponse<ChannelMonitorVariableGroup>
  >(endpoint, group, requestConfig)
  return result(response.data)
}

export async function deleteChannelMonitorVariableGroup(
  group: ChannelMonitorVariableGroup
) {
  const response = await api.delete<ChannelMonitorApiResponse<null>>(
    `${endpoint}/${group.id}`,
    { ...requestConfig, params: { revision: group.revision } }
  )
  return result(response.data)
}

export async function fetchChannelMonitorVariableGroupDraft(request: {
  group: ChannelMonitorVariableGroup
  requestId: string
}) {
  const response = await api.post<
    ChannelMonitorApiResponse<{
      request_id: string
      variables: ChannelMonitorCustomVariableRequest['variables']
    }>
  >(
    `${endpoint}/fetch`,
    { ...request.group, request_id: request.requestId },
    requestConfig
  )
  return result(response.data)
}
