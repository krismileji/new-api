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

import type {
  ChannelMonitorSmartScheduleRouteSnapshotStatus,
  ChannelMonitorSmartScheduleTraffic,
} from '../types'

const SOURCE_LABELS: Record<string, string> = {
  smart_schedule: '智能调度',
  weighted: '权重选路',
  affinity: '亲和',
  retry: '重试',
  concurrency_fallback: '并发切换',
  specific_channel: '指定渠道',
  other: '其他',
  unknown: '未记录',
}

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

export function ChannelMonitorSmartScheduleTrafficTable(props: {
  items?: readonly ChannelMonitorSmartScheduleTraffic[]
  routes: readonly { channel_id: number; channel_name: string }[]
  group: string
  model: string
  degraded?: boolean
}) {
  const items =
    props.items?.filter(
      (item) => item.group === props.group && item.model === props.model
    ) ?? []
  const available = items.some((item) => item.available)
  const complete =
    items.length > 0 &&
    !props.degraded &&
    items.every((item) => item.available && item.complete) &&
    props.routes.every((route) =>
      items.some((item) => item.channel_id === route.channel_id)
    )
  const attempts = items.reduce((total, item) => total + item.attempt_count, 0)
  const successes = items.reduce(
    (total, item) => total + item.final_success_count,
    0
  )
  const names = new Map(
    props.routes.map((route) => [route.channel_id, route.channel_name])
  )
  const first = items[0]
  return (
    <section className='min-w-0 border-b px-4 py-3' aria-label='实际请求分布'>
      <div className='mb-2 flex flex-wrap items-center gap-x-3 gap-y-1'>
        <h3 className='text-sm font-medium'>实际请求分布</h3>
        <span className='text-muted-foreground text-xs'>
          当前渠道 · 业务请求
        </span>
        {first ? (
          <span className='text-muted-foreground text-xs'>
            {formatTimestampToDate(first.window_start)} 至{' '}
            {formatTimestampToDate(first.window_end)}
          </span>
        ) : null}
        {available && !complete ? (
          <Badge variant='outline'>统计窗口不完整</Badge>
        ) : null}
      </div>
      {!available ? (
        <p className='text-muted-foreground text-xs' role='status'>
          实际请求统计暂不可用
        </p>
      ) : (
        <div className='max-w-full overflow-x-auto'>
          <table className='w-full min-w-[620px] text-left text-xs'>
            <thead className='text-muted-foreground border-b'>
              <tr>
                <th className='px-2 py-2 font-medium'>渠道</th>
                <th className='px-2 py-2 text-right font-medium'>最终成功</th>
                <th className='px-2 py-2 text-right font-medium'>上游尝试</th>
                <th className='px-2 py-2 text-right font-medium'>重试请求</th>
                <th className='px-2 py-2 font-medium'>尝试来源</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.channel_id} className='border-b last:border-0'>
                  <th className='max-w-56 px-2 py-2 font-medium break-all'>
                    {names.get(item.channel_id) || `渠道 #${item.channel_id}`}
                  </th>
                  <td className='px-2 py-2 text-right tabular-nums'>
                    {item.available ? item.final_success_count : '未知'}
                    <span className='text-muted-foreground ml-2'>
                      {complete && successes > 0
                        ? `${((item.final_success_count / successes) * 100).toFixed(1)}%`
                        : '-'}
                    </span>
                  </td>
                  <td className='px-2 py-2 text-right tabular-nums'>
                    {item.available ? item.attempt_count : '未知'}
                    <span className='text-muted-foreground ml-2'>
                      {complete && attempts > 0
                        ? `${((item.attempt_count / attempts) * 100).toFixed(1)}%`
                        : '-'}
                    </span>
                  </td>
                  <td className='px-2 py-2 text-right tabular-nums'>
                    {item.available && item.retry_count_known
                      ? item.retry_request_count
                      : '未知'}
                  </td>
                  <td className='max-w-64 px-2 py-2 break-words'>
                    {Object.entries(item.source_counts)
                      .map(
                        ([source, count]) =>
                          `${SOURCE_LABELS[source] ?? '其他'} ${count}`
                      )
                      .join(' · ') || '-'}
                    {item.logical_member_count > 0 ? (
                      <div className='text-muted-foreground'>
                        逻辑成员 {item.logical_member_count}
                      </div>
                    ) : null}
                    {item.rate_limit_fallback_count > 0 ? (
                      <div className='text-muted-foreground'>
                        429 冷却选路 {item.rate_limit_fallback_count}
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {attempts === 0 && complete ? (
            <p className='text-muted-foreground py-2 text-xs'>
              窗口内无业务请求
            </p>
          ) : null}
        </div>
      )}
    </section>
  )
}
