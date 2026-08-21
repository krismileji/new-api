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
import { compareChannelStatusesEnabledFirst } from '@/features/channels/lib/channel-status-order'

import type {
  ChannelStatusProbeChannel,
  ChannelStatusProbeHealth,
  ChannelStatusProbeSortMode,
} from '../types'

export const CHANNEL_STATUS_PROBE_SORT_STORAGE_KEY =
  'channel-monitor:status-probe-sort:v1'
export const DEFAULT_CHANNEL_STATUS_PROBE_SORT: ChannelStatusProbeSortMode =
  'ratio_asc'

const VALID_SORT_MODES = new Set<ChannelStatusProbeSortMode>([
  'ratio_asc',
  'ratio_desc',
  'first_token_asc',
  'first_token_desc',
  'tps_desc',
  'tps_asc',
])

function compareNames(
  first: ChannelStatusProbeChannel,
  second: ChannelStatusProbeChannel
) {
  const compared = first.name.localeCompare(second.name, 'zh-CN', {
    numeric: true,
    sensitivity: 'base',
  })
  return compared || first.id - second.id
}

function compareNullableNumber(
  first: number | null | undefined,
  second: number | null | undefined,
  descending: boolean
) {
  if (first == null && second == null) return 0
  if (first == null) return 1
  if (second == null) return -1
  return descending ? second - first : first - second
}

export function sortChannelStatusProbeChannels(
  channels: ChannelStatusProbeChannel[],
  mode: ChannelStatusProbeSortMode
) {
  return [...channels].sort((first, second) => {
    const statusComparison = compareChannelStatusesEnabledFirst(
      first.channel_status,
      second.channel_status
    )
    if (statusComparison !== 0) return statusComparison

    let compared = 0
    switch (mode) {
      case 'ratio_asc':
      case 'ratio_desc':
        compared = compareNullableNumber(
          first.cost_ratio,
          second.cost_ratio,
          mode === 'ratio_desc'
        )
        break
      case 'first_token_asc':
      case 'first_token_desc':
        compared = compareNullableNumber(
          first.avg_first_token_ms,
          second.avg_first_token_ms,
          mode === 'first_token_desc'
        )
        break
      case 'tps_asc':
      case 'tps_desc':
        compared = compareNullableNumber(
          first.avg_tps,
          second.avg_tps,
          mode === 'tps_desc'
        )
        break
    }
    return compared || compareNames(first, second)
  })
}

export function matchesChannelStatusProbeSearch(
  channel: ChannelStatusProbeChannel,
  search: string
) {
  const normalizedSearch = search.trim().toLocaleLowerCase('zh-CN')
  if (!normalizedSearch) return true

  return [channel.name, channel.remark, String(channel.id)]
    .join(' ')
    .toLocaleLowerCase('zh-CN')
    .includes(normalizedSearch)
}

export function matchesChannelStatusProbeGroup(
  channel: ChannelStatusProbeChannel,
  group: string
) {
  return !group || channel.groups.includes(group)
}

export function loadChannelStatusProbeSort(): ChannelStatusProbeSortMode {
  try {
    const value = localStorage.getItem(CHANNEL_STATUS_PROBE_SORT_STORAGE_KEY)
    if (value && VALID_SORT_MODES.has(value as ChannelStatusProbeSortMode)) {
      return value as ChannelStatusProbeSortMode
    }
  } catch {}
  return DEFAULT_CHANNEL_STATUS_PROBE_SORT
}

export function saveChannelStatusProbeSort(mode: ChannelStatusProbeSortMode) {
  try {
    localStorage.setItem(CHANNEL_STATUS_PROBE_SORT_STORAGE_KEY, mode)
  } catch {}
}

export function isChannelStatusProbeIssue(health: ChannelStatusProbeHealth) {
  return (
    health === 'unhealthy' || health === 'rate_limited' || health === 'partial'
  )
}

export function isChannelStatusProbeActive(channel: ChannelStatusProbeChannel) {
  return channel.running || Boolean(channel.config?.manual_request_id)
}
