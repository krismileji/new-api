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
import { useMemo, useState } from 'react'

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

import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
  formatMonitorRatio,
} from '../lib/format'
import type { ChannelMonitorCostChannel } from '../types'
import {
  ChannelMonitorSortableTableHead,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'
import { ChannelMonitorStatusBadge } from './channel-monitor-status-badge'

type ChannelMonitorChannelCostTableProps = {
  items: readonly ChannelMonitorCostChannel[]
  detailDate: string
}

type ChannelCostSortKey =
  | 'channel_name'
  | 'cost_ratio'
  | 'cost_cny'
  | 'probe_cost_cny'
  | 'model_detection_cost_cny'
  | 'settled_count'
  | 'unresolved_count'
  | 'resolution_rate'

type ChannelCostSort = {
  key: ChannelCostSortKey
  direction: ChannelMonitorSortDirection
} | null

function getResolutionRate(item: ChannelMonitorCostChannel) {
  const total = item.settled_count + item.unresolved_count
  return total > 0 ? item.settled_count / total : null
}

function compareChannelCostItems(
  first: ChannelMonitorCostChannel,
  second: ChannelMonitorCostChannel,
  sort: Exclude<ChannelCostSort, null>
) {
  if (sort.key === 'channel_name') {
    const nameOrder = first.channel_name.localeCompare(second.channel_name)
    if (nameOrder !== 0) {
      return sort.direction === 'asc' ? nameOrder : -nameOrder
    }
    return first.channel_id - second.channel_id
  }

  const firstValue =
    sort.key === 'resolution_rate' ? getResolutionRate(first) : first[sort.key]
  const secondValue =
    sort.key === 'resolution_rate'
      ? getResolutionRate(second)
      : second[sort.key]
  const firstNumber = Number.isFinite(firstValue) ? Number(firstValue) : null
  const secondNumber = Number.isFinite(secondValue) ? Number(secondValue) : null
  if (firstNumber == null && secondNumber != null) return 1
  if (firstNumber != null && secondNumber == null) return -1
  if (firstNumber != null && secondNumber != null) {
    const result = firstNumber - secondNumber
    if (result !== 0) {
      return sort.direction === 'asc' ? result : -result
    }
  }
  return first.channel_id - second.channel_id
}

function toggleChannelCostSort(
  current: ChannelCostSort,
  key: ChannelCostSortKey
): Exclude<ChannelCostSort, null> {
  return {
    key,
    direction:
      current?.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }
}

export function ChannelMonitorChannelCostTable(
  props: ChannelMonitorChannelCostTableProps
) {
  const [sort, setSort] = useState<ChannelCostSort>(null)
  const orderedItems = useMemo(() => {
    const items = props.items.filter(
      (item) => item.settled_count > 0 || item.unresolved_count > 0
    )
    return items.sort((first, second) => {
      if (sort) return compareChannelCostItems(first, second, sort)
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
      return nameOrder !== 0 ? nameOrder : first.channel_id - second.channel_id
    })
  }, [props.items, sort])
  const sortDirection = (key: ChannelCostSortKey) =>
    sort?.key === key ? sort.direction : undefined

  if (orderedItems.length === 0) {
    return (
      <Empty className='min-h-56 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={MoneyBag02Icon} />
          </EmptyMedia>
          <EmptyTitle>所选日期暂无渠道成本尝试</EmptyTitle>
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
        <Table className='min-w-[980px] table-fixed'>
          <TableHeader>
            <TableRow>
              <ChannelMonitorSortableTableHead
                label='渠道'
                className='w-[18%]'
                direction={sortDirection('channel_name')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'channel_name')
                  )
                }
              />
              <TableHead className='w-[18%] whitespace-normal'>备注</TableHead>
              <ChannelMonitorSortableTableHead
                label='成本倍率'
                align='right'
                className='w-[10%]'
                direction={sortDirection('cost_ratio')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'cost_ratio')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='已结算成本'
                align='right'
                className='w-[14%]'
                direction={sortDirection('cost_cny')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='探测成本'
                align='right'
                className='w-[12%]'
                direction={sortDirection('probe_cost_cny')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'probe_cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='模型检测成本'
                align='right'
                className='w-[14%]'
                direction={sortDirection('model_detection_cost_cny')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'model_detection_cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='成本覆盖'
                align='right'
                className='w-[14%]'
                direction={sortDirection('resolution_rate')}
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'resolution_rate')
                  )
                }
              />
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
                  {channel.cost_ratio == null ? (
                    <span className='text-warning'>配置缺失</span>
                  ) : (
                    formatMonitorRatio(channel.cost_ratio)
                  )}
                </TableCell>
                <TableCell className='text-right font-mono tabular-nums'>
                  {formatChannelMonitorCost(channel.cost_cny)}
                </TableCell>
                <TableCell className='text-right font-mono tabular-nums'>
                  {formatChannelMonitorCost(channel.probe_cost_cny)}
                </TableCell>
                <TableCell className='text-right font-mono tabular-nums'>
                  {formatChannelMonitorCost(channel.model_detection_cost_cny)}
                </TableCell>
                <TableCell className='text-right'>
                  <div className='flex flex-col items-end gap-0.5 text-xs'>
                    <span className='font-mono tabular-nums'>
                      已结算 {channel.settled_count} · 未解析{' '}
                      {channel.unresolved_count}
                    </span>
                    <span className='text-muted-foreground'>
                      解析率{' '}
                      {formatChannelMonitorResolutionRate(
                        channel.settled_count,
                        channel.unresolved_count
                      )}
                    </span>
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
