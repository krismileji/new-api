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
import { formatTimestampToDate } from '@/lib/format'

type ChannelMonitorStatusWindowUnit = 'minute' | 'hour' | 'day'

const BUCKET_SECONDS: Record<ChannelMonitorStatusWindowUnit, number> = {
  minute: 60,
  hour: 3_600,
  day: 86_400,
}

export function formatChannelMonitorStatusWindowRange(
  startedAt: number,
  unit: ChannelMonitorStatusWindowUnit
) {
  const start = formatTimestampToDate(startedAt)
  const end = formatTimestampToDate(startedAt + BUCKET_SECONDS[unit])
  if (start.slice(0, 10) === end.slice(0, 10)) {
    return `${start} - ${end.slice(11)}`
  }
  return `${start} - ${end}`
}
