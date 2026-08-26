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
import type {
  ChannelStatusProbeChannel,
  ChannelStatusProbeHealth,
} from '../types'

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

export function isChannelStatusProbeIssue(health: ChannelStatusProbeHealth) {
  return (
    health === 'unhealthy' || health === 'rate_limited' || health === 'partial'
  )
}

export function isChannelStatusProbeActive(channel: ChannelStatusProbeChannel) {
  return channel.running || Boolean(channel.config?.manual_request_id)
}
