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
import { MoneyBag02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CHANNEL_STATUS } from '@/features/channels/constants'

import { formatChannelMonitorCost, formatMonitorRatio } from '../lib/format'
import type { ChannelMonitorCostChannel } from '../types'
import { ChannelMonitorStatusBadge } from './channel-monitor-status-badge'

type ChannelMonitorChannelCostTableProps = {
  items: readonly ChannelMonitorCostChannel[]
  detailDate: string
}

export function ChannelMonitorChannelCostTable(
  props: ChannelMonitorChannelCostTableProps
) {
  const orderedItems = useMemo(
    () =>
      [...props.items].sort((first, second) => {
        const firstEnabled = first.status === CHANNEL_STATUS.ENABLED
        const secondEnabled = second.status === CHANNEL_STATUS.ENABLED
        if (firstEnabled !== secondEnabled) return firstEnabled ? -1 : 1

        const firstRatio =
          first.cost_ratio != null && Number.isFinite(first.cost_ratio)
            ? first.cost_ratio
            : null
        const secondRatio =
          second.cost_ratio != null && Number.isFinite(second.cost_ratio)
            ? second.cost_ratio
            : null
        if (firstRatio == null && secondRatio != null) return 1
        if (firstRatio != null && secondRatio == null) return -1
        if (
          firstRatio != null &&
          secondRatio != null &&
          firstRatio !== secondRatio
        ) {
          return firstRatio - secondRatio
        }

        const nameOrder = first.channel_name.localeCompare(second.channel_name)
        return nameOrder !== 0
          ? nameOrder
          : first.channel_id - second.channel_id
      }),
    [props.items]
  )

  if (props.items.length === 0) {
    return (
      <Empty className='min-h-56 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={MoneyBag02Icon} />
          </EmptyMedia>
          <EmptyTitle>所选日期暂无渠道成本</EmptyTitle>
          <EmptyDescription>{props.detailDate}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <section className='flex min-w-0 flex-col gap-2'>
      <div className='flex items-center justify-between gap-3'>
        <h3 className='text-sm font-medium'>渠道成本明细</h3>
        <span className='text-muted-foreground font-mono text-xs'>
          {props.detailDate}
        </span>
      </div>
      <div className='overflow-hidden rounded-md border'>
        <Table className='w-full table-fixed'>
          <TableHeader>
            <TableRow>
              <TableHead className='w-[28%] whitespace-normal'>渠道</TableHead>
              <TableHead className='w-[32%] whitespace-normal'>备注</TableHead>
              <TableHead className='w-[18%] text-right whitespace-normal'>
                成本倍率
              </TableHead>
              <TableHead className='w-[22%] text-right whitespace-normal'>
                成本
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {orderedItems.map((channel) => (
              <TableRow key={channel.channel_id}>
                <TableCell className='min-w-0 whitespace-normal'>
                  <div className='flex min-w-0 flex-col gap-1'>
                    <div className='flex min-w-0 flex-wrap items-center gap-2'>
                      <span className='min-w-0 font-medium break-words'>
                        {channel.channel_name}
                      </span>
                      <ChannelMonitorStatusBadge
                        status={channel.status}
                        className='shrink-0'
                      />
                    </div>
                    <span className='text-muted-foreground text-xs'>
                      ID {channel.channel_id}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='whitespace-normal'>
                  {channel.channel_remark ? (
                    <span
                      className='text-muted-foreground break-words'
                      title={channel.channel_remark}
                    >
                      {channel.channel_remark}
                    </span>
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </TableCell>
                <TableCell className='text-right font-mono font-medium tabular-nums'>
                  {formatMonitorRatio(channel.cost_ratio)}
                </TableCell>
                <TableCell className='text-right'>
                  <div className='flex flex-col items-end gap-0.5 font-mono tabular-nums'>
                    <span>{formatChannelMonitorCost(channel.cost_cny)}</span>
                    {channel.unresolved_count > 0 ? (
                      <span className='text-warning font-sans text-xs'>
                        不完整
                      </span>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}
