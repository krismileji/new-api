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
export type ChannelMonitorDiagnostics = {
  day_start: number
  counted_since: number
  last_reset_at: number
  observed_at: number
  retry_count: number
  takeover_count: number
  quarantine_count: number
  last_quarantined_at: number
  marker_release_failure_count: number
  stream_trim_failure_count: number
}

export type ChannelMonitorDiagnosticsInput = {
  data?: ChannelMonitorDiagnostics
  loading?: boolean
  failed?: boolean
  onReset?: (dayStart: number) => Promise<unknown>
}
