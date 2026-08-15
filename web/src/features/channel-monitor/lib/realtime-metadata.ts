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
import type { ChannelMonitorRealtimeMetadata } from '../types'

export function mergeChannelMonitorRealtimeMetadata(
  snapshots: readonly (ChannelMonitorRealtimeMetadata | undefined)[]
): ChannelMonitorRealtimeMetadata | undefined {
  let merged: ChannelMonitorRealtimeMetadata | undefined
  for (const snapshot of snapshots) {
    if (!snapshot) continue
    if (!merged) {
      merged = { ...snapshot }
      continue
    }
    merged.data_cutoff_at = Math.min(
      merged.data_cutoff_at,
      snapshot.data_cutoff_at
    )
    merged.processed_at = Math.min(merged.processed_at, snapshot.processed_at)
    merged.event_watermark = Math.min(
      merged.event_watermark,
      snapshot.event_watermark
    )
    merged.queue_depth = Math.max(merged.queue_depth, snapshot.queue_depth)
    merged.realtime_degraded =
      merged.realtime_degraded || snapshot.realtime_degraded
  }
  return merged
}
