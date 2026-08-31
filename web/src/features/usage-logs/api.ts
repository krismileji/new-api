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

import { buildQueryParams } from './lib/query-params'
import { parseTaskArtifactsResponse } from './lib/task-artifacts'
import type {
  GetLogsParams,
  GetLogsResponse,
  GetLogStatsParams,
  GetLogStatsResponse,
  GetMidjourneyLogsParams,
  GetTaskLogsParams,
  LogsViewScope,
  TaskArtifactsResponse,
  UserInfo,
} from './types'

// ============================================================================
// Generic API Helpers
// ============================================================================

function buildApiPath(endpoint: string, scope: LogsViewScope): string {
  if (scope === 'all') return endpoint
  if (scope === 'user-visible') return `${endpoint}/user-visible`
  return `${endpoint}/self`
}

async function fetchLogs<T>(
  endpoint: string,
  params: T,
  scope: LogsViewScope
): Promise<GetLogsResponse> {
  const paramRecord = params as unknown as Record<string, unknown>
  const queryParams = buildQueryParams({
    p: paramRecord.p || 1,
    page_size: paramRecord.page_size || 20,
    ...params,
  })
  const path = buildApiPath(endpoint, scope)
  const res = await api.get(`${path}?${queryParams}`)
  return res.data
}

async function fetchLogStats<T>(
  endpoint: string,
  params: T,
  scope: LogsViewScope
): Promise<GetLogStatsResponse> {
  const queryParams = buildQueryParams(
    params as unknown as Record<string, unknown>
  )
  const path = buildApiPath(endpoint, scope)
  const res = await api.get(`${path}/stat?${queryParams}`)
  return res.data
}

// ============================================================================
// Common Log APIs
// ============================================================================

export const getAllLogs = (params: GetLogsParams = {}) =>
  fetchLogs('/api/log', params, 'all')

export const getAllUserVisibleLogs = (params: GetLogsParams = {}) =>
  fetchLogs('/api/log', params, 'user-visible')

export const getUserLogs = (
  params: Omit<GetLogsParams, 'username' | 'channel'> = {}
) => fetchLogs('/api/log', params, 'self')

export const getLogStats = (params: GetLogStatsParams = {}) =>
  fetchLogStats('/api/log', params, 'all')

export const getAllUserVisibleLogStats = (params: GetLogStatsParams = {}) =>
  fetchLogStats('/api/log', params, 'user-visible')

export const getUserLogStats = (
  params: Omit<GetLogStatsParams, 'username' | 'channel'> = {}
) => fetchLogStats('/api/log', params, 'self')

export async function getUserInfo(
  userId: number
): Promise<{ success: boolean; message?: string; data?: UserInfo }> {
  const res = await api.get(`/api/user/${userId}`)
  return res.data
}

// ============================================================================
// MjProxy (Drawing) Logs API
// ============================================================================

export const getAllMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, 'all')

export const getAllUserVisibleMidjourneyLogs = (
  params: GetMidjourneyLogsParams
) => fetchLogs('/api/mj', params, 'user-visible')

export const getUserMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, 'self')

// ============================================================================
// Task Logs API
// ============================================================================

export const getAllTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, 'all')

export const getAllUserVisibleTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, 'user-visible')

export const getUserTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, 'self')

const taskArtifactRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
} satisfies ApiRequestConfig

export async function getTaskArtifacts(taskId: string) {
  const response = await api.get<TaskArtifactsResponse>(
    `/api/task/${encodeURIComponent(taskId)}/artifacts`,
    taskArtifactRequestConfig
  )
  return parseTaskArtifactsResponse(response.data)
}
