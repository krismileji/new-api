import { z } from 'zod'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ChannelMonitorApiResponse } from '../types'

export type RequestStatsSettings = {
  revision: number
  low_input_threshold_k: number
  enabled: boolean
  disable_rate_percent: number
  minimum_known_input_requests: number
  started_at: number
}
export type RequestStatsRow = {
  id: number
  name: string
  user_id: number
  api_key_id: number
  request_count: number
  stream_request_count: number
  non_stream_request_count: number
  unclassified_stream_request_count: number
  known_input_request_count: number
  unknown_input_request_count: number
  low_input_count: number
  low_input_rate: number | null
  low_input_auto_disabled_count: number
}
export type RequestStatsQuery = {
  date: string
  level: 'user' | 'api_key' | 'channel'
  user_id?: number
  api_key_id?: number
  search: string
  sort: 'low_input_rate' | 'request_count' | 'low_input_auto_disabled_count'
  direction: 'asc' | 'desc'
  page: number
  page_size: number
}
export type RequestStatsResult = {
  low_input_threshold_k: number
  revision: number
  coverage_partial: boolean
  data_cutoff_at: number
  processed_at: number
  source: string
  summary: RequestStatsRow
  items: RequestStatsRow[]
  total: number
}
export type RequestStatsDisable = {
  id: number
  day_start: number
  api_key_id: number
  user_id: number
  api_key_name: string
  config_revision: number
  low_input_threshold_k: number
  disable_rate_percent: number
  minimum_known_input_requests: number
  known_input_request_count: number
  low_input_count: number
  created_at: number
  restored_at: number
  restored_by: string
  mail_status: string
  mail_attempts: number
  mail_last_error: string
}

const endpoint = '/api/channel_monitor/request-stats'
const config = { skipBusinessError: true, skipErrorHandler: true }
export const requestStatsQueryKey = ['channel-monitor-request-stats']

export async function getRequestStats(
  query: RequestStatsQuery,
  signal?: AbortSignal
) {
  return requireServerSuccess(
    (
      await api.get<ChannelMonitorApiResponse<RequestStatsResult>>(endpoint, {
        ...config,
        params: query,
        signal,
      })
    ).data
  ).data
}
export async function getRequestStatsSettings() {
  return requireServerSuccess(
    (
      await api.get<ChannelMonitorApiResponse<RequestStatsSettings>>(
        `${endpoint}/settings`,
        config
      )
    ).data
  ).data
}
export async function saveRequestStatsSettings(values: {
  revision: number
  low_input_threshold_k: number
}) {
  return requireServerSuccess(
    (
      await api.put<ChannelMonitorApiResponse<RequestStatsSettings>>(
        `${endpoint}/settings`,
        values,
        config
      )
    ).data
  ).data
}
export async function saveRequestStatsAutoDisable(values: {
  revision: number
  enabled: boolean
  disable_rate_percent: number
  minimum_known_input_requests: number
}) {
  return requireServerSuccess(
    (
      await api.put<ChannelMonitorApiResponse<RequestStatsSettings>>(
        `${endpoint}/auto-disable/settings`,
        values,
        config
      )
    ).data
  ).data
}
export async function getRequestStatsHistory(
  query: {
    user_id?: number
    api_key_id?: number
    page: number
    page_size: number
  },
  signal?: AbortSignal
) {
  return requireServerSuccess(
    (
      await api.get<
        ChannelMonitorApiResponse<{
          items: RequestStatsDisable[]
          total: number
        }>
      >(`${endpoint}/disable-history`, { ...config, params: query, signal })
    ).data
  ).data
}

export const requestStatsThresholdSchema = z.object({
  low_input_threshold_k: z
    .number()
    .int('请输入整数')
    .min(1, '阈值至少为 1K')
    .max(1000, '阈值不得超过 1000K'),
})
export const requestStatsAutoDisableSchema = z.object({
  enabled: z.boolean(),
  disable_rate_percent: z
    .number()
    .int('请输入整数')
    .min(0, '占比至少为 0%')
    .max(100, '占比不得超过 100%'),
  minimum_known_input_requests: z
    .number()
    .int('请输入整数')
    .min(1, '至少需要 1 个有效样本')
    .max(1000000000, '样本数过大'),
})
