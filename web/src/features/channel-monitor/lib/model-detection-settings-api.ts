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
  ChannelModelDetectionDetectorService,
  ChannelModelDetectionSettings,
  ChannelModelDetectionSettingsUpdateRequest,
} from '../types-model-detection'
import { CHANNEL_MODEL_DETECTION_ENDPOINTS } from './model-detection'

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

export class ChannelModelDetectionSettingsConflictError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ChannelModelDetectionSettingsConflictError'
  }
}

export class ChannelModelDetectionServiceTestError extends Error {
  constructor(
    message: string,
    readonly service: ChannelModelDetectionDetectorService | null
  ) {
    super(message)
    this.name = 'ChannelModelDetectionServiceTestError'
  }
}

export function isChannelModelDetectionSettingsConflict(error: unknown) {
  if (error instanceof ChannelModelDetectionSettingsConflictError) return true
  if (!axios.isAxiosError(error)) return false
  const response = error.response?.data as
    | ChannelModelDetectionApiResponse<unknown>
    | undefined
  return (
    error.response?.status === 409 || response?.code === 'revision_conflict'
  )
}

export function channelModelDetectionRequestErrorMessage(error: unknown) {
  if (axios.isAxiosError(error)) {
    const response = error.response?.data as
      | Partial<ChannelModelDetectionApiResponse<unknown>>
      | undefined
    return response?.message || error.message || '模型检测请求失败'
  }
  return error instanceof Error && error.message
    ? error.message
    : '模型检测请求失败'
}

export function channelModelDetectionServiceFromError(error: unknown) {
  return error instanceof ChannelModelDetectionServiceTestError
    ? error.service
    : null
}

export async function getChannelModelDetectionSettings() {
  const response = await api.get<
    ChannelModelDetectionApiResponse<ChannelModelDetectionSettings>
  >(CHANNEL_MODEL_DETECTION_ENDPOINTS.settings, requestConfig())
  return ensureSuccess(response.data)
}

export async function updateChannelModelDetectionSettings(
  request: ChannelModelDetectionSettingsUpdateRequest
) {
  try {
    const response = await api.put<
      ChannelModelDetectionApiResponse<ChannelModelDetectionSettings>
    >(CHANNEL_MODEL_DETECTION_ENDPOINTS.settings, request, requestConfig())
    if (!response.data.success && response.data.code === 'revision_conflict') {
      throw new ChannelModelDetectionSettingsConflictError(
        response.data.message || '设置已被其他管理员更新'
      )
    }
    return ensureSuccess(response.data)
  } catch (error) {
    if (isChannelModelDetectionSettingsConflict(error)) {
      throw new ChannelModelDetectionSettingsConflictError(
        channelModelDetectionRequestErrorMessage(error)
      )
    }
    throw error
  }
}

export async function testChannelModelDetectionService() {
  try {
    const response = await api.post<
      ChannelModelDetectionApiResponse<ChannelModelDetectionDetectorService>
    >(CHANNEL_MODEL_DETECTION_ENDPOINTS.serviceTest, undefined, requestConfig())
    return ensureSuccess(response.data)
  } catch (error) {
    if (!axios.isAxiosError(error)) throw error
    const response = error.response?.data as
      | Partial<
          ChannelModelDetectionApiResponse<ChannelModelDetectionDetectorService>
        >
      | undefined
    throw new ChannelModelDetectionServiceTestError(
      response?.message || error.message || '检测器连接测试失败',
      response?.data ?? null
    )
  }
}
