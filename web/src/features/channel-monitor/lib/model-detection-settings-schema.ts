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

import type {
  ChannelModelDetectionSettings,
  ChannelModelDetectionSettingsUpdateRequest,
} from '../types-model-detection'

export const CHANNEL_MODEL_DETECTION_INTERVAL_OPTIONS = [
  6, 12, 24, 48, 72, 168,
] as const

export const CHANNEL_MODEL_DETECTION_TIMEZONE_OPTIONS = [
  'Asia/Shanghai',
  'Asia/Hong_Kong',
  'Asia/Tokyo',
  'Asia/Singapore',
  'UTC',
  'Europe/London',
  'America/Los_Angeles',
  'America/New_York',
] as const

const detectorURLSchema = z
  .string()
  .trim()
  .superRefine((value, context) => {
    if (!value) return
    if (value.length > 1024) {
      context.addIssue({
        code: 'custom',
        message: '检测器地址不能超过 1024 个字符',
      })
      return
    }
    let parsed: URL
    try {
      parsed = new URL(value)
    } catch {
      context.addIssue({ code: 'custom', message: '检测器地址格式无效' })
      return
    }
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      context.addIssue({
        code: 'custom',
        message: '检测器地址必须使用 http 或 https',
      })
    }
    if (parsed.username || parsed.password || parsed.search || parsed.hash) {
      context.addIssue({
        code: 'custom',
        message: '检测器地址不得包含用户信息、查询参数或片段',
      })
    }
  })

export const channelModelDetectionSettingsSchema = z
  .object({
    detectorURL: detectorURLSchema,
    clearDetectorURL: z.boolean(),
    scheduledPreset: z.enum(['low', 'medium', 'high']),
    scheduleEnabled: z.boolean(),
    intervalHours: z
      .number()
      .refine(
        (value) =>
          CHANNEL_MODEL_DETECTION_INTERVAL_OPTIONS.includes(
            value as (typeof CHANNEL_MODEL_DETECTION_INTERVAL_OPTIONS)[number]
          ),
        '请选择允许的检测周期'
      ),
    scheduleTime: z
      .string()
      .trim()
      .regex(/^([01]\d|2[0-3]):[0-5]\d$/, '执行时间必须使用 HH:mm 格式'),
    timezone: z
      .string()
      .trim()
      .min(1, '时区不能为空')
      .max(64, '时区不能超过 64 个字符')
      .refine((value) => {
        try {
          new Intl.DateTimeFormat('en-US', { timeZone: value }).format()
          return true
        } catch {
          return false
        }
      }, '请输入有效的 IANA 时区'),
    confirmHighCost: z.boolean(),
    revision: z.number().int().positive(),
  })
  .superRefine((value, context) => {
    if (value.clearDetectorURL && value.detectorURL) {
      context.addIssue({
        code: 'custom',
        path: ['detectorURL'],
        message: '新地址与清除地址不能同时提交',
      })
    }
    if (
      value.scheduleEnabled &&
      value.scheduledPreset === 'high' &&
      !value.confirmHighCost
    ) {
      context.addIssue({
        code: 'custom',
        path: ['confirmHighCost'],
        message: '启用高档定时检测前必须确认成本风险',
      })
    }
  })

export type ChannelModelDetectionSettingsFormValues = z.infer<
  typeof channelModelDetectionSettingsSchema
>

export const CHANNEL_MODEL_DETECTION_SETTINGS_QUERY_KEY = [
  'channel-monitor',
  'model-detection',
  'settings',
] as const

export const CHANNEL_MODEL_DETECTION_SETTINGS_EMPTY_VALUES: ChannelModelDetectionSettingsFormValues =
  {
    detectorURL: '',
    clearDetectorURL: false,
    scheduledPreset: 'medium',
    scheduleEnabled: false,
    intervalHours: 24,
    scheduleTime: '02:30',
    timezone: 'Asia/Shanghai',
    confirmHighCost: false,
    revision: 1,
  }

export function channelModelDetectionSettingsToFormValues(
  settings: ChannelModelDetectionSettings
): ChannelModelDetectionSettingsFormValues {
  return {
    detectorURL: '',
    clearDetectorURL: false,
    scheduledPreset: settings.scheduled_preset,
    scheduleEnabled: settings.schedule_enabled,
    intervalHours: settings.interval_hours,
    scheduleTime: settings.schedule_time,
    timezone: settings.timezone,
    confirmHighCost: false,
    revision: settings.revision,
  }
}

export function createChannelModelDetectionSettingsUpdateRequest(
  values: ChannelModelDetectionSettingsFormValues
): ChannelModelDetectionSettingsUpdateRequest {
  const detectorURL = values.detectorURL.trim()
  return {
    ...(detectorURL ? { detector_url: detectorURL } : {}),
    clear_detector_url: values.clearDetectorURL,
    scheduled_preset: values.scheduledPreset,
    confirm_high_cost:
      values.scheduleEnabled && values.scheduledPreset === 'high'
        ? values.confirmHighCost
        : false,
    schedule_enabled: values.scheduleEnabled,
    interval_hours: values.intervalHours,
    schedule_time: values.scheduleTime.trim(),
    timezone: values.timezone.trim(),
    revision: values.revision,
  }
}
