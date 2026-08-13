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
import axios from 'axios'

import { api, type ApiRequestConfig } from '@/lib/api'

import type {
  ChannelModelDetectionApiResponse,
  ChannelModelDetectionChannelConfigResult,
  ChannelModelDetectionConfigUpdateRequest,
  ChannelModelDetectionCancelResponse,
  ChannelModelDetectionEstimateRequest,
  ChannelModelDetectionEstimateResult,
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionRunAccepted,
  ChannelModelDetectionRunDetail,
  ChannelModelDetectionRunHistoryPage,
  ChannelModelDetectionRunRequest,
} from '../types-model-detection'
import { CHANNEL_MODEL_DETECTION_ENDPOINTS } from './model-detection'
import { channelModelDetectionRequestErrorMessage } from './model-detection-settings-api'

const requestConfig = (config: ApiRequestConfig = {}): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

function ensureSuccess<T>(response: ChannelModelDetectionApiResponse<T>) {
  if (!response.success) {
    throw new Error(response.message || '模型检测请求失败')
  }
  return response.data
}

export class ChannelModelDetectionConfigConflictError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ChannelModelDetectionConfigConflictError'
  }
}

export class ChannelModelDetectionInfrastructureConflictError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ChannelModelDetectionInfrastructureConflictError'
  }
}

export function isChannelModelDetectionConfigConflict(error: unknown) {
  if (error instanceof ChannelModelDetectionConfigConflictError) return true
  if (!axios.isAxiosError(error)) return false
  const response = error.response?.data as
    | Partial<ChannelModelDetectionApiResponse<unknown>>
    | undefined
  return (
    error.response?.status === 409 || response?.code === 'revision_conflict'
  )
}

export function isChannelModelDetectionInfrastructureConflict(error: unknown) {
  if (error instanceof ChannelModelDetectionInfrastructureConflictError) {
    return true
  }
  if (!axios.isAxiosError(error)) return false
  return error.response?.status === 409
}

export async function updateChannelModelDetectionConfig(
  channelId: number,
  request: ChannelModelDetectionConfigUpdateRequest
) {
  try {
    const response = await api.put<
      ChannelModelDetectionApiResponse<ChannelModelDetectionChannelConfigResult>
    >(
      CHANNEL_MODEL_DETECTION_ENDPOINTS.channelConfig(channelId),
      request,
      requestConfig()
    )
    if (!response.data.success && response.data.code === 'revision_conflict') {
      throw new ChannelModelDetectionConfigConflictError(
        response.data.message || '渠道配置已被其他管理员更新'
      )
    }
    return ensureSuccess(response.data)
  } catch (error) {
    if (isChannelModelDetectionConfigConflict(error)) {
      throw new ChannelModelDetectionConfigConflictError(
        channelModelDetectionRequestErrorMessage(error)
      )
    }
    throw error
  }
}

export async function estimateChannelModelDetectionCost(
  channelId: number,
  request: ChannelModelDetectionEstimateRequest
) {
  const response = await api.post<
    ChannelModelDetectionApiResponse<ChannelModelDetectionEstimateResult>
  >(
    CHANNEL_MODEL_DETECTION_ENDPOINTS.channelEstimate(channelId),
    request,
    requestConfig()
  )
  return ensureSuccess(response.data)
}

export async function startChannelModelDetectionRun(
  channelId: number,
  request: ChannelModelDetectionRunRequest
) {
  try {
    const response = await api.post<
      ChannelModelDetectionApiResponse<ChannelModelDetectionRunAccepted>
    >(
      CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRun(channelId),
      request,
      requestConfig()
    )
    return ensureSuccess(response.data)
  } catch (error) {
    if (isChannelModelDetectionInfrastructureConflict(error)) {
      throw new ChannelModelDetectionInfrastructureConflictError(
        channelModelDetectionRequestErrorMessage(error)
      )
    }
    throw error
  }
}

export async function getChannelModelDetectionRun(runId: string) {
  const response = await api.get<
    ChannelModelDetectionApiResponse<ChannelModelDetectionRunDetail>
  >(CHANNEL_MODEL_DETECTION_ENDPOINTS.run(runId), requestConfig())
  return ensureSuccess(response.data)
}

export async function getChannelModelDetectionRuns(
  channelId: number,
  query: ChannelModelDetectionHistoryQuery
) {
  const response = await api.get<
    ChannelModelDetectionApiResponse<ChannelModelDetectionRunHistoryPage>
  >(
    CHANNEL_MODEL_DETECTION_ENDPOINTS.channelRuns(channelId),
    requestConfig({
      params: {
        page: query.page,
        page_size: query.page_size,
        trigger: query.trigger || undefined,
        status: query.status || undefined,
        model: query.model || undefined,
        outcome: query.outcome || undefined,
      },
    })
  )
  return ensureSuccess(response.data)
}

export async function cancelChannelModelDetectionRun(runId: string) {
  try {
    const response = await api.post<
      ChannelModelDetectionApiResponse<ChannelModelDetectionCancelResponse>
    >(
      CHANNEL_MODEL_DETECTION_ENDPOINTS.cancelRun(runId),
      undefined,
      requestConfig()
    )
    return ensureSuccess(response.data)
  } catch (error) {
    if (isChannelModelDetectionInfrastructureConflict(error)) {
      throw new ChannelModelDetectionInfrastructureConflictError(
        channelModelDetectionRequestErrorMessage(error)
      )
    }
    throw error
  }
}
