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
import type { ChannelTestResponse } from '../../types'

export type ChannelTestMetricValues = {
  responseTime?: number
  firstTokenMs?: number
  tokensPerSecond?: number
  usageAvailable?: boolean
  inputTokens?: number
  outputTokens?: number
  totalTokens?: number
  cachedTokens?: number
  cacheWriteTokens?: number
  reasoningTokens?: number
  smartScheduleSampleRecorded?: boolean
  smartScheduleSampleMessage?: string
}

function readFiniteNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

export function parseChannelTestMetrics(
  response?: ChannelTestResponse,
  fallbackResponseTime?: number
): ChannelTestMetricValues {
  const data = response?.data
  const usageAvailable = data?.usage_available === true

  return {
    responseTime:
      readFiniteNumber(data?.response_time) ??
      readFiniteNumber(fallbackResponseTime),
    firstTokenMs: readFiniteNumber(data?.first_token_ms),
    tokensPerSecond: readFiniteNumber(data?.tokens_per_second),
    usageAvailable,
    inputTokens: usageAvailable
      ? readFiniteNumber(data?.input_tokens)
      : undefined,
    outputTokens: usageAvailable
      ? readFiniteNumber(data?.output_tokens)
      : undefined,
    totalTokens: usageAvailable
      ? readFiniteNumber(data?.total_tokens)
      : undefined,
    cachedTokens: usageAvailable
      ? readFiniteNumber(data?.cached_tokens)
      : undefined,
    cacheWriteTokens: usageAvailable
      ? readFiniteNumber(data?.cache_write_tokens)
      : undefined,
    reasoningTokens: usageAvailable
      ? readFiniteNumber(data?.reasoning_tokens)
      : undefined,
    smartScheduleSampleRecorded: data?.smart_schedule_sample_recorded,
    smartScheduleSampleMessage: data?.smart_schedule_sample_message,
  }
}

export function formatChannelTestDuration(value?: number): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'

  const milliseconds = Math.max(0, value)
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(2)} s`
}
