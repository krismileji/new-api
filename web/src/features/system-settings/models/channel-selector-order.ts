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

import type { UpstreamChannel } from '../types'
import { MODELS_DEV_PRESET_ID, OFFICIAL_CHANNEL_ID } from './constants'

// Synthesized presets from `controller/ratio_sync.go` always use stable IDs.
export function isOfficialUpstreamChannel(channel: UpstreamChannel): boolean {
  return (
    channel.id === OFFICIAL_CHANNEL_ID || channel.id === MODELS_DEV_PRESET_ID
  )
}

export function orderUpstreamChannelsForSelection(
  channels: readonly UpstreamChannel[]
): UpstreamChannel[] {
  return [...channels].sort((first, second) => {
    const statusOrder = compareChannelStatusesEnabledFirst(
      first.status,
      second.status
    )
    if (statusOrder !== 0) return statusOrder

    const firstOfficial = isOfficialUpstreamChannel(first)
    const secondOfficial = isOfficialUpstreamChannel(second)
    if (firstOfficial === secondOfficial) return 0
    return firstOfficial ? -1 : 1
  })
}
