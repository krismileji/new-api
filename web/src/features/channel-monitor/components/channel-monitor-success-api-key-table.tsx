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

import { cn } from '@/lib/utils'

import type {
  ChannelMonitorSuccessAPIKeyMetric,
  ChannelMonitorSuccessSummary,
} from '../types'
import {
  ChannelMonitorSortableTableHead,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'

type ChannelMonitorSuccessAPIKeyTableProps = {
  items: readonly ChannelMonitorSuccessAPIKeyMetric[]
}

type SuccessAPIKeyItem = ChannelMonitorSuccessAPIKeyMetric & {
  display_name: string
}

type SuccessAPIKeyUserGroup = {
  user_id: number
  username: string
  display_name: string
  items: SuccessAPIKeyItem[]
  summary: ChannelMonitorSuccessSummary
}

type SuccessAPIKeySortKey =
  | 'user_name'
  | 'api_key_count'
  | 'actual_sample_count'
  | 'actual_success_rate'
  | 'cache_utilization_rate'

type SuccessAPIKeySort = {
  key: SuccessAPIKeySortKey
  direction: ChannelMonitorSortDirection
}

const successAPIKeyGridClassName =
  'grid grid-cols-[minmax(0,2.1fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.9fr)_minmax(4.5rem,0.9fr)_minmax(5rem,0.9fr)]'
const successAPIKeyItemGridClassName =
  'grid grid-cols-[minmax(0,2.9fr)_minmax(4.5rem,0.9fr)_minmax(4.5rem,0.9fr)_minmax(5rem,0.9fr)]'
const percentFormatter = new Intl.NumberFormat(undefined, {
  style: 'percent',
  maximumFractionDigits: 2,
})

function emptySummary(): ChannelMonitorSuccessSummary {
  return {
    actual_success_count: 0,
    actual_failure_count: 0,
    actual_sample_count: 0,
    actual_success_rate: 0,
    final_success_count: 0,
    final_failure_count: 0,
    final_sample_count: 0,
    final_success_rate: 0,
    cache_hit_count: 0,
    cache_sample_count: 0,
    cache_hit_rate: 0,
    cache_read_tokens: 0,
    input_tokens: 0,
    cache_utilization_rate: 0,
  }
}

function addSummary(
  target: ChannelMonitorSuccessSummary,
  source: ChannelMonitorSuccessSummary
) {
  target.actual_success_count += source.actual_success_count
  target.actual_failure_count += source.actual_failure_count
  target.final_success_count += source.final_success_count
  target.final_failure_count += source.final_failure_count
  target.cache_hit_count += source.cache_hit_count
  target.cache_sample_count += source.cache_sample_count
  target.cache_read_tokens += source.cache_read_tokens
  target.input_tokens += source.input_tokens
  target.actual_sample_count =
    target.actual_success_count + target.actual_failure_count
  target.final_sample_count =
    target.final_success_count + target.final_failure_count
  target.actual_success_rate =
    target.actual_sample_count > 0
      ? target.actual_success_count / target.actual_sample_count
      : 0
  target.final_success_rate =
    target.final_sample_count > 0
      ? target.final_success_count / target.final_sample_count
      : 0
  target.cache_hit_rate =
    target.cache_sample_count > 0
      ? target.cache_hit_count / target.cache_sample_count
      : 0
  target.cache_utilization_rate =
    target.input_tokens > 0 ? target.cache_read_tokens / target.input_tokens : 0
}

function formatRate(rate: number, sampleCount: number) {
  return sampleCount > 0 && Number.isFinite(rate)
    ? percentFormatter.format(rate)
    : '-'
}

function getRateClassName(rate: number) {
  if (rate >= 0.9) return 'text-success'
  if (rate >= 0.7) return 'text-warning'
  return 'text-destructive'
}

function getAPIKeyName(item: ChannelMonitorSuccessAPIKeyMetric) {
  if (item.api_key_name) return item.api_key_name
  if (item.api_key_id > 0) return `未命名 API Key #${item.api_key_id}`
  return '未识别 API Key'
}

function getUserName(item: ChannelMonitorSuccessAPIKeyMetric) {
  if (item.user_display_name) return item.user_display_name
  if (item.username) return item.username
  if ((item.user_id ?? 0) > 0) return `用户 #${item.user_id}`
  return '未归属用户'
}

function getSortValue(
  summary: ChannelMonitorSuccessSummary,
  key: Exclude<SuccessAPIKeySortKey, 'user_name' | 'api_key_count'>
) {
  if (key === 'actual_sample_count') return summary.actual_sample_count
  if (key === 'actual_success_rate') {
    return summary.actual_sample_count > 0 ? summary.actual_success_rate : null
  }
  return summary.input_tokens > 0 ? summary.cache_utilization_rate : null
}

function compareNumbers(
  first: number | null,
  second: number | null,
  direction: ChannelMonitorSortDirection
) {
  if (first == null && second != null) return 1
  if (first != null && second == null) return -1
  if (first == null || second == null || first === second) return 0
  return direction === 'asc' ? first - second : second - first
}

function compareItems(
  first: SuccessAPIKeyItem,
  second: SuccessAPIKeyItem,
  sort: SuccessAPIKeySort
) {
  if (sort.key === 'user_name' || sort.key === 'api_key_count') {
    return (
      second.actual_sample_count - first.actual_sample_count ||
      first.display_name.localeCompare(second.display_name)
    )
  }
  const result = compareNumbers(
    getSortValue(first, sort.key),
    getSortValue(second, sort.key),
    sort.direction
  )
  return result || first.display_name.localeCompare(second.display_name)
}

function getUserSortValue(
  group: SuccessAPIKeyUserGroup,
  key: SuccessAPIKeySortKey
) {
  if (key === 'user_name') return group.display_name
  if (key === 'api_key_count') return group.items.length
  return getSortValue(group.summary, key)
}

function compareGroups(
  first: SuccessAPIKeyUserGroup,
  second: SuccessAPIKeyUserGroup,
  sort: SuccessAPIKeySort
) {
  const firstValue = getUserSortValue(first, sort.key)
  const secondValue = getUserSortValue(second, sort.key)
  if (typeof firstValue === 'string' && typeof secondValue === 'string') {
    const result = firstValue.localeCompare(secondValue)
    if (result !== 0) return sort.direction === 'asc' ? result : -result
  } else {
    const result = compareNumbers(
      typeof firstValue === 'number' ? firstValue : null,
      typeof secondValue === 'number' ? secondValue : null,
      sort.direction
    )
    if (result !== 0) return result
  }
  return (
    first.user_id - second.user_id ||
    first.display_name.localeCompare(second.display_name)
  )
}

function toggleSort(current: SuccessAPIKeySort, key: SuccessAPIKeySortKey) {
  return {
    key,
    direction:
      current.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  } satisfies SuccessAPIKeySort
}

function SuccessAPIKeyItemRow(props: { item: SuccessAPIKeyItem }) {
  const item = props.item
  const successRateClassName =
    item.actual_sample_count > 0
      ? getRateClassName(item.actual_success_rate)
      : 'text-muted-foreground'
  const cacheUtilizationClassName =
    item.input_tokens > 0 ? 'text-foreground' : 'text-muted-foreground'
  return (
    <div
      className={cn(successAPIKeyItemGridClassName, 'items-center px-3 py-3')}
    >
      <span className='flex min-w-0 items-center pl-7'>
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
        </span>
      </span>
      <span className='text-right font-mono text-sm tabular-nums'>
        {item.actual_sample_count}
      </span>
      <span
        className={cn(
          'text-right font-mono text-sm font-semibold tabular-nums',
          successRateClassName
        )}
      >
        {formatRate(item.actual_success_rate, item.actual_sample_count)}
      </span>
      <span
        className={cn(
          'text-right font-mono text-sm font-semibold tabular-nums',
          cacheUtilizationClassName
        )}
      >
        {formatRate(item.cache_utilization_rate, item.input_tokens)}
      </span>
    </div>
  )
}

export function ChannelMonitorSuccessAPIKeyTable(
  props: ChannelMonitorSuccessAPIKeyTableProps
) {
  const [sort, setSort] = useState<SuccessAPIKeySort>({
    key: 'actual_success_rate',
    direction: 'desc',
  })
  const userGroups = useMemo(() => {
    const groups = new Map<string, SuccessAPIKeyUserGroup>()
    for (const item of props.items) {
      const userId = item.user_id ?? 0
      const key = userId > 0 ? `user:${userId}` : 'unassigned'
      let group = groups.get(key)
      if (!group) {
        group = {
          user_id: userId,
          username: item.username ?? '',
          display_name: getUserName(item),
          items: [],
          summary: emptySummary(),
        }
        groups.set(key, group)
      }
      group.items.push({ ...item, display_name: getAPIKeyName(item) })
      addSummary(group.summary, item)
    }
    return [...groups.values()]
      .map((group) => ({
        ...group,
        items: [...group.items].sort((first, second) =>
          compareItems(first, second, sort)
        ),
      }))
      .sort((first, second) => compareGroups(first, second, sort))
  }, [props.items, sort])
  const sortDirection = (key: SuccessAPIKeySortKey) =>
    sort.key === key ? sort.direction : undefined

  if (props.items.length === 0) return null

  return (
    <section
      data-slot='channel-monitor-success-api-key-details'
      className='flex shrink-0 flex-col gap-2'
    >
      <div className='flex items-baseline justify-between gap-3'>
        <h3 className='font-medium'>API Key 明细</h3>
        <span className='text-muted-foreground text-xs'>先按用户分组</span>
      </div>
      <div className='max-h-[min(36rem,60dvh)] overflow-auto rounded-lg border'>
        <div className='min-w-0'>
          <div
            className={cn(
              successAPIKeyGridClassName,
              'bg-muted/30 min-h-10 items-center border-b px-3'
            )}
          >
            <ChannelMonitorSortableTableHead
              label='用户'
              direction={sortDirection('user_name')}
              onSort={() =>
                setSort((current) => toggleSort(current, 'user_name'))
              }
            />
            <ChannelMonitorSortableTableHead
              label='API Key 数'
              align='right'
              direction={sortDirection('api_key_count')}
              onSort={() =>
                setSort((current) => toggleSort(current, 'api_key_count'))
              }
            />
            <ChannelMonitorSortableTableHead
              label='请求数'
              align='right'
              direction={sortDirection('actual_sample_count')}
              onSort={() =>
                setSort((current) => toggleSort(current, 'actual_sample_count'))
              }
            />
            <ChannelMonitorSortableTableHead
              label='成功率'
              align='right'
              direction={sortDirection('actual_success_rate')}
              onSort={() =>
                setSort((current) => toggleSort(current, 'actual_success_rate'))
              }
            />
            <ChannelMonitorSortableTableHead
              label='缓存利用率'
              align='right'
              direction={sortDirection('cache_utilization_rate')}
              onSort={() =>
                setSort((current) =>
                  toggleSort(current, 'cache_utilization_rate')
                )
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
                    successAPIKeyGridClassName,
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
                    {group.summary.actual_sample_count}
                  </span>
                  <span className='text-right font-mono text-sm font-semibold tabular-nums'>
                    {formatRate(
                      group.summary.actual_success_rate,
                      group.summary.actual_sample_count
                    )}
                  </span>
                  <span className='text-right font-mono text-sm font-semibold tabular-nums'>
                    {formatRate(
                      group.summary.cache_utilization_rate,
                      group.summary.input_tokens
                    )}
                  </span>
                </summary>
                <div className='bg-muted/10 border-t'>
                  <div
                    className={cn(
                      successAPIKeyItemGridClassName,
                      'bg-muted/20 min-h-9 items-center border-b px-3 text-xs font-medium'
                    )}
                  >
                    <span className='pl-7'>API Key</span>
                    <span className='text-right'>请求数</span>
                    <span className='text-right'>成功率</span>
                    <span className='text-right'>缓存利用率</span>
                  </div>
                  <div className='divide-border divide-y'>
                    {group.items.map((item) => (
                      <SuccessAPIKeyItemRow
                        key={`${item.api_key_id}:${item.display_name}`}
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
    </section>
  )
}
