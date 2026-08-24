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

import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
  formatMonitorRatio,
} from '../lib/format'
import type { ChannelMonitorCostChannel } from '../types'
import {
  ChannelMonitorSortButton,
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
  | 'group_probe_cost_cny'
  | 'model_detection_cost_cny'
  | 'settled_count'
  | 'unresolved_count'
  | 'resolution_rate'

type ChannelCostSort = {
  key: ChannelCostSortKey
  direction: ChannelMonitorSortDirection
}

function getResolutionRate(item: ChannelMonitorCostChannel) {
  const total = item.settled_count + item.unresolved_count
  return total > 0 ? item.settled_count / total : null
}

function compareChannelCostItems(
  first: ChannelMonitorCostChannel,
  second: ChannelMonitorCostChannel,
  sort: ChannelCostSort
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
): ChannelCostSort {
  return {
    key,
    direction:
      current?.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }
}

export function ChannelMonitorChannelCostTable(
  props: ChannelMonitorChannelCostTableProps
) {
  const [sort, setSort] = useState<ChannelCostSort>({
    key: 'cost_cny',
    direction: 'desc',
  })
  const orderedItems = useMemo(() => {
    const items = props.items.filter(
      (item) => item.settled_count > 0 || item.unresolved_count > 0
    )
    return items.sort((first, second) => {
      return compareChannelCostItems(first, second, sort)
    })
  }, [props.items, sort])
  const sortDirection = (key: ChannelCostSortKey) =>
    sort.key === key ? sort.direction : undefined

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
        <Table className='w-full table-fixed [&_td]:overflow-hidden [&_td]:text-ellipsis'>
          <TableHeader className='bg-muted/30'>
            <TableRow>
              <ChannelMonitorSortableTableHead
                label='渠道'
                className='w-[25%] px-1 text-xs'
                direction={sortDirection('channel_name')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'channel_name')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='成本倍率'
                align='right'
                className='w-[9%] px-1 text-xs'
                direction={sortDirection('cost_ratio')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'cost_ratio')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='已结算成本'
                align='right'
                className='w-[12%] px-1 text-xs'
                direction={sortDirection('cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='探测成本'
                align='right'
                className='w-[11%] px-1 text-xs'
                direction={sortDirection('probe_cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'probe_cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='分组探测成本'
                align='right'
                className='w-[12%] px-1 text-xs'
                direction={sortDirection('group_probe_cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'group_probe_cost_cny')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='模型检测成本'
                align='right'
                className='w-[13%] px-1 text-xs'
                direction={sortDirection('model_detection_cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleChannelCostSort(current, 'model_detection_cost_cny')
                  )
                }
              />
              <TableHead className='w-[18%] p-0'>
                <div className='grid grid-cols-3'>
                  <ChannelMonitorSortButton
                    label='结算'
                    align='right'
                    className='rounded-none px-1 text-xs'
                    direction={sortDirection('settled_count')}
                    subtleUnsortedIcon
                    onSort={() =>
                      setSort((current) =>
                        toggleChannelCostSort(current, 'settled_count')
                      )
                    }
                  />
                  <ChannelMonitorSortButton
                    label='未解析'
                    align='right'
                    className='rounded-none px-1 text-xs'
                    direction={sortDirection('unresolved_count')}
                    subtleUnsortedIcon
                    onSort={() =>
                      setSort((current) =>
                        toggleChannelCostSort(current, 'unresolved_count')
                      )
                    }
                  />
                  <ChannelMonitorSortButton
                    label='解析率'
                    align='right'
                    className='rounded-none px-1 text-xs'
                    direction={sortDirection('resolution_rate')}
                    subtleUnsortedIcon
                    onSort={() =>
                      setSort((current) =>
                        toggleChannelCostSort(current, 'resolution_rate')
                      )
                    }
                  />
                </div>
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
                    <div className='text-muted-foreground flex min-w-0 items-center gap-2 text-xs'>
                      <span className='shrink-0'>ID {channel.channel_id}</span>
                      {channel.channel_remark ? (
                        <span
                          className='truncate border-l pl-2'
                          title={channel.channel_remark}
                        >
                          {channel.channel_remark}
                        </span>
                      ) : null}
                    </div>
                  </div>
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
                  {formatChannelMonitorCost(channel.group_probe_cost_cny)}
                </TableCell>
                <TableCell className='text-right font-mono tabular-nums'>
                  {formatChannelMonitorCost(channel.model_detection_cost_cny)}
                </TableCell>
                <TableCell>
                  <div className='grid grid-cols-3 gap-2 text-right font-mono text-xs tabular-nums'>
                    <span aria-label={`已结算 ${channel.settled_count}`}>
                      {channel.settled_count}
                    </span>
                    <span aria-label={`未解析 ${channel.unresolved_count}`}>
                      {channel.unresolved_count}
                    </span>
                    <span
                      className='text-muted-foreground'
                      aria-label={`解析率 ${formatChannelMonitorResolutionRate(channel.settled_count, channel.unresolved_count)}`}
                    >
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
