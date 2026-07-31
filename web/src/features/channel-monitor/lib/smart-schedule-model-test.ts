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
import type { ChannelMonitorSmartScheduleModelTestResult } from '../types'

export const CHANNEL_MONITOR_MODEL_TEST_ENDPOINT_OPTIONS = [
  { value: 'auto', label: '自动检测' },
  { value: 'openai', label: 'OpenAI Chat (/v1/chat/completions)' },
  { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
  {
    value: 'openai-response-compact',
    label: 'OpenAI 响应压缩 (/v1/responses/compact)',
  },
  { value: 'anthropic', label: 'Anthropic Messages (/v1/messages)' },
  {
    value: 'gemini',
    label: 'Gemini Generate Content (/v1beta/models/{model}:generateContent)',
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
  { value: 'image-generation', label: '图像生成 (/v1/images/generations)' },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
]

export const CHANNEL_MONITOR_MODEL_TEST_STREAM_INCOMPATIBLE_ENDPOINTS = new Set(
  ['embeddings', 'image-generation', 'jina-rerank', 'openai-response-compact']
)

export function mergeChannelMonitorModelTestRetry(
  current: ChannelMonitorSmartScheduleModelTestResult | null,
  retried: ChannelMonitorSmartScheduleModelTestResult
) {
  const retriedItem = retried.results[0]
  if (!current || !retriedItem) return retried

  let replaced = false
  const results = current.results.map((item) => {
    if (item.channel_id !== retriedItem.channel_id) return item
    replaced = true
    return retriedItem
  })
  if (!replaced) results.push(retriedItem)

  return {
    ...current,
    stream: retried.stream,
    endpoint_type: retried.endpoint_type,
    total: results.length,
    succeeded: results.filter((item) => item.status === 'success').length,
    failed: results.filter((item) => item.status === 'failure').length,
    skipped: results.filter((item) => item.status === 'skipped').length,
    results,
  }
}
