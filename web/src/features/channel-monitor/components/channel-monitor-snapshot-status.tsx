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

type ChannelMonitorSnapshotStatusProps = {
  generatedAt?: number
  dataCutoffAt?: number
  eventWatermark?: number
  snapshotAgeSeconds?: number
  stale?: boolean
  className?: string
}

export function ChannelMonitorSnapshotStatus(
  props: ChannelMonitorSnapshotStatusProps
) {
  const generatedLabel = props.generatedAt
    ? formatTimestampToDate(props.generatedAt)
    : '暂无'
  const cutoffLabel = props.dataCutoffAt
    ? formatTimestampToDate(props.dataCutoffAt)
    : '暂无'

  return (
    <div
      className={cn(
        'text-muted-foreground flex min-w-0 flex-wrap items-center gap-2 text-xs',
        props.className
      )}
      data-slot='channel-monitor-snapshot-status'
    >
      {props.stale ? <Badge variant='warning'>任务快照已过期</Badge> : null}
      <span className='min-w-0 text-wrap'>
        快照生成 {generatedLabel}
        {props.dataCutoffAt !== undefined ? ` · 数据截至 ${cutoffLabel}` : ''}
        {' · '}已处理事件序号 {props.eventWatermark ?? 0} · 快照年龄{' '}
        {props.snapshotAgeSeconds ?? 0} 秒
      </span>
    </div>
  )
}
