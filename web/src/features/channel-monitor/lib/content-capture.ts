import { z } from 'zod'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ChannelMonitorApiResponse } from '../types'

export type ContentCaptureConfig = {
  enabled: boolean
  limit: number
  max_body_kb: number
  started_at: number
  expires_at: number
}
export type ContentCaptureSummary = {
  id: string
  created_at: number
  user_id: number
  username: string
  token_id: number
  token_name: string
  group: string
  model: string
  channel_id: number
  path: string
  ip: string
  status_code: number
  is_stream: boolean
  duration_ms: number
  request_size: number
  response_size: number
  request_truncated: boolean
  response_truncated: boolean
  request_preview: string
}
export type ContentCaptureRecord = ContentCaptureSummary & {
  request_body: string
  response_body: string
}
export type ContentCaptureState = {
  config: ContentCaptureConfig
  items: ContentCaptureSummary[]
}

const endpoint = '/api/channel_monitor/content-capture'
const config = { skipBusinessError: true, skipErrorHandler: true }
export const contentCaptureQueryKey = ['channel-monitor-content-capture']

export async function getContentCapture(signal?: AbortSignal) {
  return requireServerSuccess(
    (
      await api.get<ChannelMonitorApiResponse<ContentCaptureState>>(endpoint, {
        ...config,
        signal,
      })
    ).data
  ).data
}
export async function saveContentCapture(
  values:
    | { enabled: false }
    | { enabled: true; limit: number; max_body_kb: number; hours: number }
) {
  return requireServerSuccess(
    (
      await api.put<ChannelMonitorApiResponse<ContentCaptureState>>(
        endpoint,
        values,
        config
      )
    ).data
  ).data
}
export async function getContentCaptureRecord(
  id: string,
  signal?: AbortSignal
) {
  return requireServerSuccess(
    (
      await api.get<ChannelMonitorApiResponse<ContentCaptureRecord>>(
        `${endpoint}/records/${encodeURIComponent(id)}`,
        { ...config, signal }
      )
    ).data
  ).data
}

export const contentCaptureSchema = z.object({
  limit: z
    .number()
    .int('请输入整数')
    .min(1, '至少记录 1 条')
    .max(1000, '最多记录 1000 条'),
  max_body_kb: z
    .number()
    .int('请输入整数')
    .min(1, '至少 1KB')
    .max(256, '不得超过 256KB'),
  hours: z
    .number()
    .int('请输入整数')
    .min(1, '至少 1 小时')
    .max(72, '不得超过 72 小时'),
})
