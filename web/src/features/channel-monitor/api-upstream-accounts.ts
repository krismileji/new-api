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
  ChannelMonitorBalanceEstimate,
  ChannelMonitorUpstreamConfig,
} from './types'

export type UpstreamAccount = {
  has_balance_key: boolean
  refresh_interval_minutes: number
  id: number
  name: string
  revision: number
  channel_ids: number[]
  channel_revisions: Record<number, number>
  upstream: ChannelMonitorUpstreamConfig
  balance: number | null
  balance_estimate?: ChannelMonitorBalanceEstimate
  last_balance_time: number
  last_balance_error: string
  proxy: string
}

export type UpstreamAccountInput = {
  proxy?: string
  balance_key?: string
  refresh_interval_minutes?: number
  id: number
  revision: number
  name: string
  source_channel_id: number
  channel_ids: number[]
  channel_revisions: Record<number, number>
}

export type UpstreamAccountDifference = {
  channel_id: number
  revision: number
  fields: string[]
}

export const upstreamAccountsQueryKey = ['channel-monitor', 'upstream-accounts']
const endpoint = '/api/channel_monitor/upstream_accounts'
const options = { skipBusinessError: true, skipErrorHandler: true }

function result<T>(response: ChannelMonitorApiResponse<T>): T {
  if (!response.success) throw new Error(response.message || '上游账户请求失败')
  return response.data
}

export function useUpstreamAccounts() {
  return useQuery({
    queryKey: upstreamAccountsQueryKey,
    queryFn: async () =>
      result(
        (
          await api.get<ChannelMonitorApiResponse<UpstreamAccount[]>>(
            endpoint,
            options
          )
        ).data
      ),
  })
}

export async function previewUpstreamAccount(input: UpstreamAccountInput) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<UpstreamAccountDifference[]>>(
        `${endpoint}/preview`,
        input,
        options
      )
    ).data
  )
}

export async function saveUpstreamAccount(input: UpstreamAccountInput) {
  return result(
    (
      await api.put<ChannelMonitorApiResponse<UpstreamAccount>>(
        endpoint,
        input,
        options
      )
    ).data
  )
}

export async function refreshUpstreamAccount(id: number) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${id}/refresh`,
        {},
        options
      )
    ).data
  )
}

export async function deleteUpstreamAccount(account: UpstreamAccount) {
  return result(
    (
      await api.delete<ChannelMonitorApiResponse<null>>(
        `${endpoint}/${account.id}`,
        { ...options, params: { revision: account.revision } }
      )
    ).data
  )
}
