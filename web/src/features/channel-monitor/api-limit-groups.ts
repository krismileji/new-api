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
import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import type { ChannelMonitorApiResponse } from './types'

export type ChannelLimitTier = {
  priority: number
  reserved_concurrency: number
  reserved_rpm: number
}
export type ChannelLimitMember = { channel_id: number; priority: number }
export type ChannelLimitGroup = {
  id: number
  name: string
  revision: number
  enabled: boolean
  concurrency_limit: number
  rpm_limit: number
  updated_at: number
  tiers: ChannelLimitTier[]
  members: ChannelLimitMember[]
  runtime?: {
    active: number
    rpm: number
    waiting: number
    reason?: string
    tiers?: { priority: number; active: number; rpm: number }[]
  }
  channel_usage?: Record<number, { active: number; current_rpm: number }>
}
export const channelLimitGroupsKey = [
  'channel-monitor',
  'limit-groups',
] as const
const endpoint = '/api/channel_monitor/limit-groups'
const requestConfig = { skipBusinessError: true, skipErrorHandler: true }

function result<T>(response: ChannelMonitorApiResponse<T>) {
  if (!response.success) throw new Error(response.message || '共享限流请求失败')
  return response.data
}
export function useChannelLimitGroups(enabled = true) {
  return useQuery({
    queryKey: channelLimitGroupsKey,
    enabled,
    queryFn: async () =>
      result(
        (
          await api.get<ChannelMonitorApiResponse<ChannelLimitGroup[]>>(
            endpoint,
            requestConfig
          )
        ).data
      ),
  })
}
export async function saveChannelLimitGroup(group: ChannelLimitGroup) {
  const body = {
    name: group.name,
    revision: group.revision,
    enabled: group.enabled,
    concurrency_limit: group.concurrency_limit,
    rpm_limit: group.rpm_limit,
    tiers: group.tiers,
    members: group.members,
  }
  const response = group.id
    ? await api.put<ChannelMonitorApiResponse<ChannelLimitGroup>>(
        `${endpoint}/${group.id}`,
        body,
        requestConfig
      )
    : await api.post<ChannelMonitorApiResponse<ChannelLimitGroup>>(
        endpoint,
        body,
        requestConfig
      )
  return result(response.data)
}
export async function deleteChannelLimitGroup(group: ChannelLimitGroup) {
  return result(
    (
      await api.delete<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${group.id}`,
        {
          ...requestConfig,
          params: { revision: group.revision },
        }
      )
    ).data
  )
}
export function channelLimitRuntimeLabel(reason?: string) {
  if (!reason) return '运行正常'
  if (reason === 'paused') return '已暂停新请求'
  if (reason === 'publishing') return '配置尚未生效，请重新保存'
  return '运行状态不可用'
}

export function channelLimitErrorMessage(error: unknown): string {
  if (isAxiosError<ChannelMonitorApiResponse<unknown>>(error)) {
    return error.response?.data?.message || '共享限流请求失败，请检查网络后重试'
  }
  return error instanceof Error ? error.message : '共享限流请求失败'
}
