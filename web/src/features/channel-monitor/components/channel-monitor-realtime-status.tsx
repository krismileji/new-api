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
import { cn } from '@/lib/utils'

import type { ChannelMonitorRealtimeMetadata } from '../types'

type ChannelMonitorRealtimeStatusProps = {
  metadata: ChannelMonitorRealtimeMetadata | undefined
  className?: string
}

export function ChannelMonitorRealtimeStatus(
  props: ChannelMonitorRealtimeStatusProps
) {
  if (!props.metadata) return null

  const cutoffLabel = props.metadata.data_cutoff_at
    ? formatTimestampToDate(props.metadata.data_cutoff_at)
    : '暂无已处理事件'
  const processedLabel = props.metadata.processed_at
    ? formatTimestampToDate(props.metadata.processed_at)
    : '暂无'

  return (
    <span
      className={cn(
        'inline-flex min-w-0 flex-wrap items-center gap-2',
        props.className
      )}
      data-channel-monitor-realtime-status
    >
      {props.metadata.realtime_degraded ? (
        <Badge variant='destructive'>实时数据已降级</Badge>
      ) : null}
      {props.metadata.queue_depth > 0 ? (
        <Badge variant='warning'>队列待处理 {props.metadata.queue_depth}</Badge>
      ) : null}
      <span
        className='text-muted-foreground truncate text-xs font-normal'
        title={`处理于 ${processedLabel} · 事件水位 ${props.metadata.event_watermark}`}
      >
        数据截至 {cutoffLabel}
      </span>
    </span>
  )
}
