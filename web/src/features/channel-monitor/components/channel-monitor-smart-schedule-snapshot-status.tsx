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
import { Badge } from '@/components/ui/badge'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelMonitorSmartScheduleRouteSnapshotStatus } from '../types'

export function ChannelMonitorSmartScheduleSnapshotStatus(props: {
  snapshot?: ChannelMonitorSmartScheduleRouteSnapshotStatus
}) {
  const snapshot = props.snapshot
  if (!snapshot?.available) {
    return <Badge variant='destructive'>路由快照不可用</Badge>
  }
  if (snapshot.stale || snapshot.protection_mode) {
    return <Badge variant='destructive'>路由快照已过期</Badge>
  }
  return (
    <span
      className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-2 text-xs'
      role='status'
    >
      <Badge variant='outline'>
        {snapshot.dirty ? '路由刷新中' : '路由已生效'}
      </Badge>
      <span>{formatTimestampToDate(snapshot.generated_at)}</span>
      {snapshot.revision > 0 ? <span>版本 {snapshot.revision}</span> : null}
    </span>
  )
}
