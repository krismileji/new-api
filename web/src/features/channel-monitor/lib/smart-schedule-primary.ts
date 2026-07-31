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
import type { ChannelMonitorSmartScheduleRoute } from '../types'

export type ChannelMonitorSmartSchedulePrimaryFormState = {
  durationMinutes: string
  allowStabilityDegrade: boolean
}

export function createChannelMonitorSmartSchedulePrimaryFormState(
  route: ChannelMonitorSmartScheduleRoute,
  nowSeconds: number
): ChannelMonitorSmartSchedulePrimaryFormState {
  if (route.state.manual_primary_until <= 0) {
    return {
      durationMinutes: '60',
      allowStabilityDegrade: true,
    }
  }

  return {
    durationMinutes: String(
      Math.max(
        1,
        Math.ceil((route.state.manual_primary_until - nowSeconds) / 60)
      )
    ),
    allowStabilityDegrade: route.state.manual_primary_allow_stability_degrade,
  }
}
