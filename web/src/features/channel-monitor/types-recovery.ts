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
export type ChannelMonitorRecovery = {
  status: 'healthy' | 'degraded' | 'unavailable'
  recovery_status: string
  node_id: string
  checked_at: number
  recovered_at: number
  pending_count: number
  message: string
  action: string
  data_gap_reasons: string[]
  notification_error?: string
  quarantine_count?: number
  cost_dead_letter_count?: number
  cost_publish_failed_count?: number
  dropped_sample_count?: number
}
