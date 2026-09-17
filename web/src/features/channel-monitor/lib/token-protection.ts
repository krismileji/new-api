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
import { z } from 'zod'

import { api } from '@/lib/api'

import type { ChannelMonitorApiResponse } from '../types'

export type TokenProtectionRule = {
  id: string
  name: string
  enabled: boolean
  channel_ids: number[]
  status_codes: number[]
  keywords: string[]
  match_all: boolean
  case_sensitive: boolean
  response_status: number
  response_message: string
}

export type TokenProtectionSettings = {
  enabled: boolean
  revision: number
  rules: TokenProtectionRule[]
}

export type TokenProtectionRecord = {
  id: string
  token_id: number
  user_id: number
  token_name: string
  rule_name: string
  channel_id: number
  request_id: string
  upstream_status: number
  error_summary: string
  response_status: number
  response_message: string
  canceled_requests: number
  created_at: number
  released_at: number
  released_by: number
  persistence_error?: string
}

export type TokenProtectionRecords = {
  records: TokenProtectionRecord[]
  total: number
  pending: TokenProtectionRecord[]
}

const endpoint = '/api/channel_monitor/token_protection'
const requestConfig = { skipBusinessError: true, skipErrorHandler: true }
export const tokenProtectionQueryKey = ['channel-monitor-token-protection']

function result<T>(response: ChannelMonitorApiResponse<T>): T {
  if (!response.success) {
    throw new Error(response.message || 'API Key 自动禁用操作失败')
  }
  return response.data
}

export async function getTokenProtectionSettings() {
  return result(
    (
      await api.get<ChannelMonitorApiResponse<TokenProtectionSettings>>(
        `${endpoint}/settings`,
        requestConfig
      )
    ).data
  )
}

export async function saveTokenProtectionSettings(
  settings: TokenProtectionSettings
) {
  return result(
    (
      await api.put<ChannelMonitorApiResponse<TokenProtectionSettings>>(
        `${endpoint}/settings`,
        settings,
        requestConfig
      )
    ).data
  )
}

export async function getTokenProtectionRecords(page: number) {
  return result(
    (
      await api.get<ChannelMonitorApiResponse<TokenProtectionRecords>>(
        `${endpoint}/records`,
        { ...requestConfig, params: { page } }
      )
    ).data
  )
}

export async function releaseTokenProtection(id: string) {
  return result(
    (
      await api.post<ChannelMonitorApiResponse<null>>(
        `${endpoint}/records/${encodeURIComponent(id)}/release`,
        {},
        requestConfig
      )
    ).data
  )
}

const byteLength = (value: string) => new TextEncoder().encode(value).length
const numbers = (value: string) =>
  value
    .trim()
    .split(/[\s,，]+/)
    .filter(Boolean)
    .map(Number)
const keywords = (value: string) =>
  value
    .split('\n')
    .map((word) => word.trim())
    .filter(Boolean)

export const tokenProtectionFormSchema = z.object({
  enabled: z.boolean(),
  rules: z
    .array(
      z.object({
        ruleId: z.string(),
        name: z
          .string()
          .trim()
          .min(1, '请输入规则名称')
          .max(128, '名称最多 128 个字符'),
        enabled: z.boolean(),
        channels: z
          .string()
          .refine(
            (value) =>
              numbers(value).length <= 200 &&
              numbers(value).every((id) => Number.isSafeInteger(id) && id > 0),
            '渠道编号必须为正整数，最多 200 个'
          ),
        statuses: z
          .string()
          .refine(
            (value) =>
              numbers(value).length > 0 &&
              numbers(value).length <= 100 &&
              numbers(value).every(
                (code) => Number.isInteger(code) && code >= 100 && code <= 599
              ),
            '请输入 100 到 599 之间的状态码'
          ),
        keywords: z
          .string()
          .refine(
            (value) =>
              keywords(value).length > 0 &&
              keywords(value).length <= 32 &&
              keywords(value).every((word) => byteLength(word) <= 512),
            '请输入 1 到 32 个关键词，每个最多 512 字节'
          ),
        match_all: z.boolean(),
        case_sensitive: z.boolean(),
        response_status: z
          .number()
          .int()
          .min(400, '返回状态码不得小于 400')
          .max(599, '返回状态码不得大于 599'),
        response_message: z
          .string()
          .trim()
          .min(1, '请输入返回错误信息')
          .refine(
            (value) => byteLength(value) <= 4096,
            '返回信息最多 4096 字节'
          ),
      })
    )
    .max(64, '最多支持 64 条规则'),
})

export type TokenProtectionForm = z.infer<typeof tokenProtectionFormSchema>

export function tokenProtectionFormValues(
  settings: TokenProtectionSettings
): TokenProtectionForm {
  return {
    enabled: settings.enabled,
    rules: settings.rules.map((rule) => ({
      ...rule,
      ruleId: rule.id,
      channels: (rule.channel_ids ?? []).join(', '),
      statuses: rule.status_codes.join(', '),
      keywords: rule.keywords.join('\n'),
    })),
  }
}

export function tokenProtectionPayload(
  values: TokenProtectionForm,
  revision: number
): TokenProtectionSettings {
  return {
    enabled: values.enabled,
    revision,
    rules: values.rules.map((rule) => ({
      id: rule.ruleId,
      name: rule.name,
      enabled: rule.enabled,
      channel_ids: numbers(rule.channels),
      status_codes: numbers(rule.statuses),
      keywords: keywords(rule.keywords),
      match_all: rule.match_all,
      case_sensitive: rule.case_sensitive,
      response_status: rule.response_status,
      response_message: rule.response_message,
    })),
  }
}

export function newTokenProtectionRule(): TokenProtectionForm['rules'][number] {
  return {
    ruleId: crypto.randomUUID(),
    name: '',
    enabled: true,
    channels: '',
    statuses: '403',
    keywords: '',
    match_all: false,
    case_sensitive: false,
    response_status: 403,
    response_message: '此 API Key 已被自动禁用，请联系管理员',
  }
}
