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
import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
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
import { cn } from '@/lib/utils'

import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
} from '../lib/format'
import type { ChannelMonitorCostAPIKey } from '../types'
import {
  ChannelMonitorSortButton,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'

type ChannelMonitorAPIKeyCostTableProps = {
  items: readonly ChannelMonitorCostAPIKey[]
}

type APIKeyCostSortKey =
  | 'api_key_name'
  | 'channel_count'
  | 'settled_count'
  | 'unresolved_count'
  | 'resolution_rate'
  | 'cost_cny'

type APIKeyCostSort = {
  key: APIKeyCostSortKey
  direction: ChannelMonitorSortDirection
}

const apiKeyCostGridClassName =
  'grid grid-cols-[minmax(14rem,2.2fr)_minmax(6rem,0.8fr)_minmax(6rem,0.9fr)_minmax(6rem,0.9fr)_minmax(6rem,0.8fr)_minmax(8rem,1.2fr)]'

function getAPIKeyName(item: ChannelMonitorCostAPIKey) {
  if (item.api_key_name) return item.api_key_name
  if (item.api_key_id > 0) return `未命名 API Key #${item.api_key_id}`
  if (item.api_key) return `上游 Key ${item.api_key}`
  return '未识别 API Key'
}

function getAPIKeyCostValue(
  item: ChannelMonitorCostAPIKey & { display_name: string },
  key: APIKeyCostSortKey
) {
  if (key === 'api_key_name') return item.display_name
  if (key === 'channel_count') return item.channels.length
  if (key === 'resolution_rate') {
    const total = item.settled_count + item.unresolved_count
    return total > 0 ? item.settled_count / total : null
  }
  return item[key]
}

function compareAPIKeyCosts(
  first: ChannelMonitorCostAPIKey & { display_name: string },
  second: ChannelMonitorCostAPIKey & { display_name: string },
  sort: APIKeyCostSort
) {
  const firstValue = getAPIKeyCostValue(first, sort.key)
  const secondValue = getAPIKeyCostValue(second, sort.key)
  if (typeof firstValue === 'string' && typeof secondValue === 'string') {
    const result = firstValue.localeCompare(secondValue)
    if (result !== 0) return sort.direction === 'asc' ? result : -result
  } else {
    const firstNumber = Number.isFinite(firstValue) ? Number(firstValue) : null
    const secondNumber = Number.isFinite(secondValue)
      ? Number(secondValue)
      : null
    if (firstNumber == null && secondNumber != null) return 1
    if (firstNumber != null && secondNumber == null) return -1
    if (firstNumber != null && secondNumber != null) {
      const result = firstNumber - secondNumber
      if (result !== 0) return sort.direction === 'asc' ? result : -result
    }
  }
  return first.api_key_id - second.api_key_id || first.id - second.id
}

function toggleAPIKeyCostSort(
  current: APIKeyCostSort,
  key: APIKeyCostSortKey
): APIKeyCostSort {
  return {
    key,
    direction:
      current.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }
}

export function ChannelMonitorAPIKeyCostTable(
  props: ChannelMonitorAPIKeyCostTableProps
) {
  const [sort, setSort] = useState<APIKeyCostSort>({
    key: 'cost_cny',
    direction: 'desc',
  })
  const costItems = useMemo(
    () =>
      props.items
        .filter((item) => item.settled_count > 0 || item.unresolved_count > 0)
        .map((item) => ({
          ...item,
          display_name: getAPIKeyName(item),
          channels: (item.channels ?? []).filter(
            (channel) =>
              channel.settled_count > 0 || channel.unresolved_count > 0
          ),
        }))
        .sort((first, second) => compareAPIKeyCosts(first, second, sort)),
    [props.items, sort]
  )
  const sortDirection = (key: APIKeyCostSortKey) =>
    sort.key === key ? sort.direction : undefined

  return (
    <section
      className='flex min-w-0 flex-col gap-2'
      aria-labelledby='api-key-cost-title'
    >
      <h3 id='api-key-cost-title' className='text-sm font-medium'>
        API Key 成本明细
      </h3>
      {costItems.length === 0 ? (
        <Empty className='min-h-32 border'>
          <EmptyHeader>
            <EmptyTitle>暂无 API Key 成本尝试</EmptyTitle>
            <EmptyDescription>
              上游请求结算或进入未解析后开始记录
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='max-h-[min(30rem,50dvh)] overflow-auto rounded-md border'>
          <div className='min-w-[840px]'>
            <div
              className={cn(
                apiKeyCostGridClassName,
                'bg-muted/30 min-h-10 items-center border-b px-3'
              )}
            >
              <ChannelMonitorSortButton
                label='API Key'
                direction={sortDirection('api_key_name')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'api_key_name')
                  )
                }
              />
              <ChannelMonitorSortButton
                label='关联渠道'
                align='right'
                direction={sortDirection('channel_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'channel_count')
                  )
                }
              />
              <ChannelMonitorSortButton
                label='结算请求'
                align='right'
                direction={sortDirection('settled_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'settled_count')
                  )
                }
              />
              <ChannelMonitorSortButton
                label='未解析'
                align='right'
                direction={sortDirection('unresolved_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'unresolved_count')
                  )
                }
              />
              <ChannelMonitorSortButton
                label='解析率'
                align='right'
                direction={sortDirection('resolution_rate')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'resolution_rate')
                  )
                }
              />
              <ChannelMonitorSortButton
                label='结算成本'
                align='right'
                direction={sortDirection('cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) =>
                    toggleAPIKeyCostSort(current, 'cost_cny')
                  )
                }
              />
            </div>
            <div className='divide-border divide-y'>
              {costItems.map((item) => {
                return (
                  <details
                    key={`${item.api_key_id}:${item.id}:${item.display_name}`}
                    className='group'
                  >
                    <summary
                      className={cn(
                        apiKeyCostGridClassName,
                        'hover:bg-muted/40 focus-visible:ring-ring/50 cursor-pointer list-none items-center px-3 py-3 outline-none focus-visible:ring-3 [&::-webkit-details-marker]:hidden'
                      )}
                    >
                      <span className='flex min-w-0 items-center pl-1'>
                        <HugeiconsIcon
                          icon={ArrowRight01Icon}
                          aria-hidden='true'
                          className='text-muted-foreground mr-1 size-4 shrink-0 transition-transform group-open:rotate-90'
                        />
                        <span className='min-w-0 flex-1'>
                          <span
                            className='block truncate font-medium'
                            title={item.display_name}
                          >
                            {item.display_name}
                          </span>
                          {item.api_key_id > 0 ? (
                            <span className='text-muted-foreground block truncate text-xs'>
                              ID {item.api_key_id}
                            </span>
                          ) : null}
                          {item.api_key ? (
                            <span
                              className='text-muted-foreground block truncate font-mono text-xs'
                              title={item.api_key}
                            >
                              上游 Key {item.api_key}
                            </span>
                          ) : null}
                        </span>
                      </span>
                      <span
                        className='text-right font-mono text-sm tabular-nums'
                        aria-label={`${item.channels.length} 个渠道`}
                      >
                        {item.channels.length}
                      </span>
                      <span className='text-right font-mono text-sm tabular-nums'>
                        {item.settled_count}
                      </span>
                      <span
                        className='text-right font-mono text-sm tabular-nums'
                        aria-label={`未解析 ${item.unresolved_count}`}
                      >
                        {item.unresolved_count}
                      </span>
                      <span
                        className='text-right font-mono text-sm tabular-nums'
                        aria-label={`解析率 ${formatChannelMonitorResolutionRate(item.settled_count, item.unresolved_count)}`}
                      >
                        {formatChannelMonitorResolutionRate(
                          item.settled_count,
                          item.unresolved_count
                        )}
                      </span>
                      <span className='text-right font-mono font-semibold tabular-nums'>
                        {formatChannelMonitorCost(item.cost_cny)}
                      </span>
                    </summary>
                    <div className='bg-muted/20 border-t px-3 py-2'>
                      {item.channels.length === 0 ? (
                        <p className='text-muted-foreground py-2 text-xs'>
                          暂无渠道明细
                        </p>
                      ) : (
                        <Table className='min-w-[680px] table-fixed'>
                          <TableHeader>
                            <TableRow>
                              <TableHead className='w-[44%]'>
                                关联渠道
                              </TableHead>
                              <TableHead className='w-[20%] text-right'>
                                已结算成本
                              </TableHead>
                              <TableHead className='w-[12%] text-right'>
                                结算请求
                              </TableHead>
                              <TableHead className='w-[12%] text-right'>
                                未解析
                              </TableHead>
                              <TableHead className='w-[12%] text-right'>
                                解析率
                              </TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {item.channels.map((channel) => (
                              <TableRow key={channel.channel_id}>
                                <TableCell className='min-w-0'>
                                  <div className='min-w-0'>
                                    <span
                                      className='block truncate'
                                      title={channel.channel_name}
                                    >
                                      {channel.channel_name}
                                    </span>
                                    {channel.channel_remark ? (
                                      <span
                                        className='text-muted-foreground block truncate text-xs'
                                        title={channel.channel_remark}
                                      >
                                        备注：{channel.channel_remark}
                                      </span>
                                    ) : null}
                                  </div>
                                </TableCell>
                                <TableCell className='text-right font-mono tabular-nums'>
                                  {formatChannelMonitorCost(channel.cost_cny)}
                                </TableCell>
                                <TableCell className='text-right font-mono tabular-nums'>
                                  {channel.settled_count}
                                </TableCell>
                                <TableCell className='text-right font-mono tabular-nums'>
                                  {channel.unresolved_count}
                                </TableCell>
                                <TableCell className='text-right font-mono tabular-nums'>
                                  {formatChannelMonitorResolutionRate(
                                    channel.settled_count,
                                    channel.unresolved_count
                                  )}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      )}
                    </div>
                  </details>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
