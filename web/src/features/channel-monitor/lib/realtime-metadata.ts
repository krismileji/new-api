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
      merged.realtime_degraded =
        snapshot.realtime_degraded ||
        (snapshot.marker_release_failure_active ?? false) ||
        (snapshot.stream_trim_failure_active ?? false)
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
    if (
      merged.pending_count !== undefined ||
      snapshot.pending_count !== undefined
    ) {
      merged.pending_count = Math.max(
        merged.pending_count ?? merged.queue_depth,
        snapshot.pending_count ?? snapshot.queue_depth
      )
    }
    if (
      merged.oldest_pending_at !== undefined ||
      snapshot.oldest_pending_at !== undefined
    ) {
      merged.oldest_pending_at = mergeOldestTimestamp(
        merged.oldest_pending_at,
        snapshot.oldest_pending_at
      )
    }
    if (
      merged.consumer_lag_seconds !== undefined ||
      snapshot.consumer_lag_seconds !== undefined
    ) {
      merged.consumer_lag_seconds = Math.max(
        merged.consumer_lag_seconds ?? 0,
        snapshot.consumer_lag_seconds ?? 0
      )
    }
    if (
      merged.last_published_at !== undefined ||
      snapshot.last_published_at !== undefined
    ) {
      merged.last_published_at = Math.max(
        merged.last_published_at ?? 0,
        snapshot.last_published_at ?? 0
      )
    }
    if (
      merged.last_processed_at !== undefined ||
      snapshot.last_processed_at !== undefined
    ) {
      merged.last_processed_at = Math.max(
        merged.last_processed_at ?? 0,
        snapshot.last_processed_at ?? 0
      )
    }
    if (
      merged.retry_count !== undefined ||
      snapshot.retry_count !== undefined
    ) {
      merged.retry_count = Math.max(
        merged.retry_count ?? 0,
        snapshot.retry_count ?? 0
      )
    }
    if (
      merged.takeover_count !== undefined ||
      snapshot.takeover_count !== undefined
    ) {
      merged.takeover_count = Math.max(
        merged.takeover_count ?? 0,
        snapshot.takeover_count ?? 0
      )
    }
    if (
      merged.marker_release_failure_count !== undefined ||
      snapshot.marker_release_failure_count !== undefined
    ) {
      merged.marker_release_failure_count = Math.max(
        merged.marker_release_failure_count ?? 0,
        snapshot.marker_release_failure_count ?? 0
      )
    }
    if (
      merged.stream_trim_failure_count !== undefined ||
      snapshot.stream_trim_failure_count !== undefined
    ) {
      merged.stream_trim_failure_count = Math.max(
        merged.stream_trim_failure_count ?? 0,
        snapshot.stream_trim_failure_count ?? 0
      )
    }
    if (
      merged.marker_release_failure_active !== undefined ||
      snapshot.marker_release_failure_active !== undefined
    ) {
      merged.marker_release_failure_active =
        (merged.marker_release_failure_active ?? false) ||
        (snapshot.marker_release_failure_active ?? false)
    }
    if (
      merged.stream_trim_failure_active !== undefined ||
      snapshot.stream_trim_failure_active !== undefined
    ) {
      merged.stream_trim_failure_active =
        (merged.stream_trim_failure_active ?? false) ||
        (snapshot.stream_trim_failure_active ?? false)
    }
    if (
      merged.redis_status !== undefined ||
      snapshot.redis_status !== undefined
    ) {
      merged.redis_status =
        merged.redis_status === 'unavailable' ||
        snapshot.redis_status === 'unavailable'
          ? 'unavailable'
          : (snapshot.redis_status ?? merged.redis_status)
    }
    if (
      merged.redis_available !== undefined ||
      snapshot.redis_available !== undefined
    ) {
      merged.redis_available = mergeAvailability(
        merged.redis_available,
        snapshot.redis_available
      )
    }
    if (
      merged.redis_consumer_running !== undefined ||
      snapshot.redis_consumer_running !== undefined
    ) {
      merged.redis_consumer_running = mergeAvailability(
        merged.redis_consumer_running,
        snapshot.redis_consumer_running
      )
    }
    merged.realtime_degraded =
      merged.realtime_degraded ||
      snapshot.realtime_degraded ||
      (merged.marker_release_failure_active ?? false) ||
      (merged.stream_trim_failure_active ?? false)
  }
  return merged
}

function mergeAvailability(
  first: boolean | undefined,
  second: boolean | undefined
): boolean | undefined {
  if (first === false || second === false) return false
  return second ?? first
}

function mergeOldestTimestamp(
  first: number | undefined,
  second: number | undefined
): number {
  if (!first) return second ?? 0
  if (!second) return first
  return Math.min(first, second)
}
