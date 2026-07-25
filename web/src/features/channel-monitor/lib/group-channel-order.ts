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
import { CHANNEL_STATUS } from '@/features/channels/constants'

type GroupChannelOrderItem = {
  id: number
  name: string
  status: number
  cost_ratio: number | null
}

export function orderGroupChannelOptions<T extends GroupChannelOrderItem>(
  channels: readonly T[]
): T[] {
  return [...channels].sort((leftChannel, rightChannel) => {
    const leftEnabled = leftChannel.status === CHANNEL_STATUS.ENABLED
    const rightEnabled = rightChannel.status === CHANNEL_STATUS.ENABLED
    if (leftEnabled !== rightEnabled) return leftEnabled ? -1 : 1

    const leftRatio =
      leftChannel.cost_ratio != null && Number.isFinite(leftChannel.cost_ratio)
        ? leftChannel.cost_ratio
        : null
    const rightRatio =
      rightChannel.cost_ratio != null &&
      Number.isFinite(rightChannel.cost_ratio)
        ? rightChannel.cost_ratio
        : null
    if (leftRatio == null && rightRatio != null) return 1
    if (leftRatio != null && rightRatio == null) return -1
    if (leftRatio != null && rightRatio != null && leftRatio !== rightRatio) {
      return leftRatio - rightRatio
    }

    const nameOrder = leftChannel.name.localeCompare(rightChannel.name)
    return nameOrder !== 0 ? nameOrder : leftChannel.id - rightChannel.id
  })
}
