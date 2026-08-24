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
import type {
  ChannelMonitorCostAPIKey,
  ChannelMonitorCostAPIKeyChannel,
} from '../types'
import {
  ChannelMonitorSortButton,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'

type ChannelMonitorAPIKeyCostTableProps = {
  items: readonly ChannelMonitorCostAPIKey[]
}

type APIKeyCostSortKey =
  | 'user_name'
  | 'api_key_count'
  | 'channel_count'
  | 'settled_count'
  | 'unresolved_count'
  | 'resolution_rate'
  | 'cost_cny'

type APIKeyCostSort = {
  key: APIKeyCostSortKey
  direction: ChannelMonitorSortDirection
}

type APIKeyCostItem = ChannelMonitorCostAPIKey & {
  display_name: string
  channels: ChannelMonitorCostAPIKeyChannel[]
}

type APIKeyCostUserGroup = {
  user_id: number
  username: string
  display_name: string
  items: APIKeyCostItem[]
  channel_count: number
  settled_count: number
  unresolved_count: number
  cost_cny: number
}

const apiKeyCostGridClassName =
  'grid grid-cols-[minmax(0,2.2fr)_minmax(4.5rem,0.7fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)_minmax(5rem,0.8fr)_minmax(8rem,1.2fr)]'
const apiKeyCostItemGridClassName =
  'grid grid-cols-[minmax(0,2.2fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)_minmax(5rem,0.8fr)_minmax(8rem,1.2fr)]'

function getAPIKeyName(item: ChannelMonitorCostAPIKey) {
  if (item.api_key_name) return item.api_key_name
  if (item.api_key_id > 0) return `未命名 API Key #${item.api_key_id}`
  if (item.api_key) return `上游 Key ${item.api_key}`
  return '未识别 API Key'
}

function getUserName(item: ChannelMonitorCostAPIKey) {
  if (item.user_display_name) return item.user_display_name
  if (item.username) return item.username
  const userId = item.user_id ?? 0
  if (userId > 0) return `用户 #${userId}`
  return '未归属用户'
}

function getUserSortValue(group: APIKeyCostUserGroup, key: APIKeyCostSortKey) {
  if (key === 'user_name') return group.display_name
  if (key === 'api_key_count') return group.items.length
  if (key === 'channel_count') return group.channel_count
  if (key === 'resolution_rate') {
    const total = group.settled_count + group.unresolved_count
    return total > 0 ? group.settled_count / total : null
  }
  return group[key]
}

function compareUserGroups(
  first: APIKeyCostUserGroup,
  second: APIKeyCostUserGroup,
  sort: APIKeyCostSort
) {
  const firstValue = getUserSortValue(first, sort.key)
  const secondValue = getUserSortValue(second, sort.key)
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
  return (
    first.user_id - second.user_id ||
    first.display_name.localeCompare(second.display_name)
  )
}

function toggleSort(
  current: APIKeyCostSort,
  key: APIKeyCostSortKey
): APIKeyCostSort {
  return {
    key,
    direction:
      current.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }
}

function filterAPIKeyChannels(
  channels: readonly ChannelMonitorCostAPIKeyChannel[]
) {
  return channels.filter(
    (channel) => channel.settled_count > 0 || channel.unresolved_count > 0
  )
}

function APIKeyCostChannels(props: { item: APIKeyCostItem }) {
  if (props.item.channels.length === 0) {
    return <p className='text-muted-foreground py-2 text-xs'>暂无渠道明细</p>
  }

  return (
    <Table className='w-full table-fixed [&_td]:overflow-hidden [&_td]:text-ellipsis'>
      <TableHeader>
        <TableRow>
          <TableHead className='w-[44%]'>关联渠道</TableHead>
          <TableHead className='w-[20%] text-right'>已结算成本</TableHead>
          <TableHead className='w-[12%] text-right'>结算请求</TableHead>
          <TableHead className='w-[12%] text-right'>未解析</TableHead>
          <TableHead className='w-[12%] text-right'>解析率</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.item.channels.map((channel) => (
          <TableRow key={channel.channel_id}>
            <TableCell className='min-w-0'>
              <div className='min-w-0'>
                <span className='block truncate' title={channel.channel_name}>
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
  )
}

function APIKeyCostItemRow(props: { item: APIKeyCostItem }) {
  const item = props.item
  return (
    <details className='group/key'>
      <summary
        className={cn(
          apiKeyCostItemGridClassName,
          'hover:bg-muted/40 focus-visible:ring-ring/50 min-w-0 cursor-pointer list-none items-center px-3 py-3 outline-none focus-visible:ring-3 [&::-webkit-details-marker]:hidden'
        )}
      >
        <span className='flex min-w-0 items-center pl-1'>
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            aria-hidden='true'
            className='text-muted-foreground mr-1 size-4 shrink-0 transition-transform group-open/key:rotate-90'
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
          className='min-w-0 truncate text-right font-mono text-sm tabular-nums'
          aria-label={`${item.channels.length} 个渠道`}
        >
          {item.channels.length}
        </span>
        <span className='min-w-0 truncate text-right font-mono text-sm tabular-nums'>
          {item.settled_count}
        </span>
        <span
          className='min-w-0 truncate text-right font-mono text-sm tabular-nums'
          aria-label={`未解析 ${item.unresolved_count}`}
        >
          {item.unresolved_count}
        </span>
        <span
          className='min-w-0 truncate text-right font-mono text-sm tabular-nums'
          aria-label={`解析率 ${formatChannelMonitorResolutionRate(item.settled_count, item.unresolved_count)}`}
        >
          {formatChannelMonitorResolutionRate(
            item.settled_count,
            item.unresolved_count
          )}
        </span>
        <span className='min-w-0 truncate text-right font-mono font-semibold tabular-nums'>
          {formatChannelMonitorCost(item.cost_cny)}
        </span>
      </summary>
      <div className='bg-muted/20 border-t px-3 py-2'>
        <APIKeyCostChannels item={item} />
      </div>
    </details>
  )
}

export function ChannelMonitorAPIKeyCostTable(
  props: ChannelMonitorAPIKeyCostTableProps
) {
  const [sort, setSort] = useState<APIKeyCostSort>({
    key: 'cost_cny',
    direction: 'desc',
  })
  const userGroups = useMemo(() => {
    const groups = new Map<
      string,
      APIKeyCostUserGroup & { channel_ids: Set<number> }
    >()
    for (const item of props.items) {
      if (item.settled_count <= 0 && item.unresolved_count <= 0) continue
      const userId = item.user_id ?? 0
      const username = item.username ?? ''
      const key = userId > 0 ? `user:${userId}` : 'unassigned'
      const existingGroup = groups.get(key)
      const group = existingGroup ?? {
        user_id: userId,
        username,
        display_name: getUserName(item),
        items: [],
        channel_ids: new Set<number>(),
        channel_count: 0,
        settled_count: 0,
        unresolved_count: 0,
        cost_cny: 0,
      }
      if (!existingGroup) groups.set(key, group)
      const normalizedItem: APIKeyCostItem = {
        ...item,
        display_name: getAPIKeyName(item),
        channels: filterAPIKeyChannels(item.channels ?? []),
      }
      group.items.push(normalizedItem)
      group.settled_count += item.settled_count
      group.unresolved_count += item.unresolved_count
      group.cost_cny += item.cost_cny
      for (const channel of normalizedItem.channels) {
        group.channel_ids.add(channel.channel_id)
      }
    }
    const normalizedGroups: APIKeyCostUserGroup[] = [...groups.values()].map(
      (group) => ({
        user_id: group.user_id,
        username: group.username,
        display_name: group.display_name,
        channel_count: group.channel_ids.size,
        settled_count: group.settled_count,
        unresolved_count: group.unresolved_count,
        cost_cny: group.cost_cny,
        items: [...group.items].sort(
          (first, second) =>
            second.cost_cny - first.cost_cny ||
            first.display_name.localeCompare(second.display_name)
        ),
      })
    )
    return normalizedGroups.sort((first, second) =>
      compareUserGroups(first, second, sort)
    )
  }, [props.items, sort])
  const sortDirection = (key: APIKeyCostSortKey) =>
    sort.key === key ? sort.direction : undefined

  return (
    <section
      className='flex min-w-0 flex-col gap-2'
      aria-labelledby='api-key-cost-title'
    >
      <div className='flex items-baseline justify-between gap-3'>
        <h3 id='api-key-cost-title' className='text-sm font-medium'>
          API Key 成本明细
        </h3>
        <span className='text-muted-foreground text-xs'>先按用户分组</span>
      </div>
      {userGroups.length === 0 ? (
        <Empty className='min-h-32 border'>
          <EmptyHeader>
            <EmptyTitle>暂无 API Key 成本尝试</EmptyTitle>
            <EmptyDescription>
              上游请求结算或进入未解析后开始记录
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='max-h-[min(36rem,60dvh)] overflow-auto rounded-md border'>
          <div className='min-w-0'>
            <div
              className={cn(
                apiKeyCostGridClassName,
                'bg-muted/30 min-h-10 items-center border-b px-3'
              )}
            >
              <ChannelMonitorSortButton
                label='用户'
                direction={sortDirection('user_name')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'user_name'))
                }
              />
              <ChannelMonitorSortButton
                label='API Key 数'
                align='right'
                direction={sortDirection('api_key_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'api_key_count'))
                }
              />
              <ChannelMonitorSortButton
                label='关联渠道'
                align='right'
                direction={sortDirection('channel_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'channel_count'))
                }
              />
              <ChannelMonitorSortButton
                label='结算请求'
                align='right'
                direction={sortDirection('settled_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'settled_count'))
                }
              />
              <ChannelMonitorSortButton
                label='未解析'
                align='right'
                direction={sortDirection('unresolved_count')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'unresolved_count'))
                }
              />
              <ChannelMonitorSortButton
                label='解析率'
                align='right'
                direction={sortDirection('resolution_rate')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'resolution_rate'))
                }
              />
              <ChannelMonitorSortButton
                label='结算成本'
                align='right'
                direction={sortDirection('cost_cny')}
                subtleUnsortedIcon
                onSort={() =>
                  setSort((current) => toggleSort(current, 'cost_cny'))
                }
              />
            </div>
            <div className='divide-border divide-y'>
              {userGroups.map((group) => (
                <details
                  key={`${group.user_id}:${group.display_name}`}
                  className='group/user'
                >
                  <summary
                    className={cn(
                      apiKeyCostGridClassName,
                      'hover:bg-muted/40 focus-visible:ring-ring/50 min-w-0 cursor-pointer list-none items-center px-3 py-3 outline-none focus-visible:ring-3 [&::-webkit-details-marker]:hidden'
                    )}
                  >
                    <span className='flex min-w-0 items-center pl-1'>
                      <HugeiconsIcon
                        icon={ArrowRight01Icon}
                        aria-hidden='true'
                        className='text-muted-foreground mr-1 size-4 shrink-0 transition-transform group-open/user:rotate-90'
                      />
                      <span className='min-w-0 flex-1'>
                        <span
                          className='block truncate font-medium'
                          title={group.display_name}
                        >
                          {group.display_name}
                        </span>
                        {group.username &&
                        group.username !== group.display_name ? (
                          <span className='text-muted-foreground block truncate text-xs'>
                            @{group.username}
                          </span>
                        ) : null}
                      </span>
                    </span>
                    <span className='text-right font-mono text-sm tabular-nums'>
                      {group.items.length}
                    </span>
                    <span className='text-right font-mono text-sm tabular-nums'>
                      {group.channel_count}
                    </span>
                    <span className='text-right font-mono text-sm tabular-nums'>
                      {group.settled_count}
                    </span>
                    <span className='text-right font-mono text-sm tabular-nums'>
                      {group.unresolved_count}
                    </span>
                    <span className='text-right font-mono text-sm tabular-nums'>
                      {formatChannelMonitorResolutionRate(
                        group.settled_count,
                        group.unresolved_count
                      )}
                    </span>
                    <span className='text-right font-mono font-semibold tabular-nums'>
                      {formatChannelMonitorCost(group.cost_cny)}
                    </span>
                  </summary>
                  <div className='bg-muted/10 border-t'>
                    <div
                      className={cn(
                        apiKeyCostItemGridClassName,
                        'bg-muted/20 min-h-9 items-center border-b px-3 text-xs font-medium'
                      )}
                    >
                      <span className='pl-7'>API Key</span>
                      <span className='text-right'>关联渠道</span>
                      <span className='text-right'>结算请求</span>
                      <span className='text-right'>未解析</span>
                      <span className='text-right'>解析率</span>
                      <span className='text-right'>结算成本</span>
                    </div>
                    <div className='divide-border divide-y'>
                      {group.items.map((item) => (
                        <APIKeyCostItemRow
                          key={`${item.api_key_id}:${item.id}:${item.display_name}`}
                          item={item}
                        />
                      ))}
                    </div>
                  </div>
                </details>
              ))}
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
