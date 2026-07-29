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
import { CHANNEL_STATUS } from '../constants'

export function compareChannelStatusesEnabledFirst(
  firstStatus: number | null | undefined,
  secondStatus: number | null | undefined
) {
  const firstEnabled = firstStatus === CHANNEL_STATUS.ENABLED
  const secondEnabled = secondStatus === CHANNEL_STATUS.ENABLED
  if (firstEnabled === secondEnabled) return 0
  return firstEnabled ? -1 : 1
}

export function orderChannelsEnabledFirst<T extends { status: number }>(
  channels: readonly T[]
): T[] {
  return [...channels].sort((first, second) =>
    compareChannelStatusesEnabledFirst(first.status, second.status)
  )
}
