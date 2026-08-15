/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'

import {
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import type { ChannelMonitorSuccessAPIKeyMetric } from '../types'
import {
  ChannelMonitorSortableTableHead,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'

type ChannelMonitorSuccessAPIKeyTableProps = {
  items: readonly ChannelMonitorSuccessAPIKeyMetric[]
}

type SuccessAPIKeySortKey =
  | 'api_key_name'
  | 'actual_sample_count'
  | 'actual_success_rate'
  | 'cache_utilization_rate'

type SuccessAPIKeySort = {
  key: SuccessAPIKeySortKey
  direction: ChannelMonitorSortDirection
}

const percentFormatter = new Intl.NumberFormat(undefined, {
  style: 'percent',
  maximumFractionDigits: 2,
})

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

function getSuccessAPIKeySortValue(
  item: ChannelMonitorSuccessAPIKeyMetric,
  key: SuccessAPIKeySortKey
) {
  if (key === 'api_key_name') return getAPIKeyName(item)
  if (key === 'actual_success_rate') {
    return item.actual_sample_count > 0 &&
      Number.isFinite(item.actual_success_rate)
      ? item.actual_success_rate
      : null
  }
  if (key === 'cache_utilization_rate') {
    return item.input_tokens > 0 && Number.isFinite(item.cache_utilization_rate)
      ? item.cache_utilization_rate
      : null
  }
  return item.actual_sample_count
}

function compareSuccessAPIKeys(
  first: ChannelMonitorSuccessAPIKeyMetric,
  second: ChannelMonitorSuccessAPIKeyMetric,
  sort: SuccessAPIKeySort
) {
  const firstValue = getSuccessAPIKeySortValue(first, sort.key)
  const secondValue = getSuccessAPIKeySortValue(second, sort.key)
  if (typeof firstValue === 'string' && typeof secondValue === 'string') {
    const result = firstValue.localeCompare(secondValue)
    if (result !== 0) return sort.direction === 'asc' ? result : -result
  } else {
    const firstNumber =
      typeof firstValue === 'number' && Number.isFinite(firstValue)
        ? firstValue
        : null
    const secondNumber =
      typeof secondValue === 'number' && Number.isFinite(secondValue)
        ? secondValue
        : null
    if (firstNumber == null && secondNumber != null) return 1
    if (firstNumber != null && secondNumber == null) return -1
    if (firstNumber != null && secondNumber != null) {
      const result = firstNumber - secondNumber
      if (result !== 0) return sort.direction === 'asc' ? result : -result
    }
  }
  if (sort.key === 'actual_success_rate') {
    const firstCacheUtilization = getSuccessAPIKeySortValue(
      first,
      'cache_utilization_rate'
    )
    const secondCacheUtilization = getSuccessAPIKeySortValue(
      second,
      'cache_utilization_rate'
    )
    const firstNumber =
      typeof firstCacheUtilization === 'number' ? firstCacheUtilization : -1
    const secondNumber =
      typeof secondCacheUtilization === 'number' ? secondCacheUtilization : -1
    if (firstNumber !== secondNumber) return secondNumber - firstNumber
    if (first.actual_sample_count !== second.actual_sample_count) {
      return second.actual_sample_count - first.actual_sample_count
    }
  }
  return (
    first.api_key_id - second.api_key_id ||
    getAPIKeyName(first).localeCompare(getAPIKeyName(second))
  )
}

function toggleSuccessAPIKeySort(
  current: SuccessAPIKeySort,
  key: SuccessAPIKeySortKey
): SuccessAPIKeySort {
  return {
    key,
    direction:
      current.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }
}

export function ChannelMonitorSuccessAPIKeyTable(
  props: ChannelMonitorSuccessAPIKeyTableProps
) {
  const [sort, setSort] = useState<SuccessAPIKeySort>({
    key: 'actual_success_rate',
    direction: 'desc',
  })
  const orderedItems = useMemo(
    () =>
      [...props.items].sort((first, second) =>
        compareSuccessAPIKeys(first, second, sort)
      ),
    [props.items, sort]
  )
  const sortDirection = (key: SuccessAPIKeySortKey) =>
    sort.key === key ? sort.direction : undefined

  if (props.items.length === 0) return null

  return (
    <section
      data-slot='channel-monitor-success-api-key-details'
      className='flex shrink-0 flex-col gap-2'
    >
      <h3 className='font-medium'>API Key 明细</h3>
      <div className='overflow-hidden rounded-lg border'>
        <Table className='min-w-[640px] table-fixed'>
          <TableHeader>
            <TableRow>
              <ChannelMonitorSortableTableHead
                label='API Key'
                className='w-[52%]'
                direction={sortDirection('api_key_name')}
                onSort={() =>
                  setSort((current) =>
                    toggleSuccessAPIKeySort(current, 'api_key_name')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='请求数'
                align='right'
                className='w-[16%]'
                direction={sortDirection('actual_sample_count')}
                onSort={() =>
                  setSort((current) =>
                    toggleSuccessAPIKeySort(current, 'actual_sample_count')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='成功率'
                align='right'
                className='w-[16%]'
                direction={sortDirection('actual_success_rate')}
                onSort={() =>
                  setSort((current) =>
                    toggleSuccessAPIKeySort(current, 'actual_success_rate')
                  )
                }
              />
              <ChannelMonitorSortableTableHead
                label='缓存利用率'
                align='right'
                className='w-[16%]'
                direction={sortDirection('cache_utilization_rate')}
                onSort={() =>
                  setSort((current) =>
                    toggleSuccessAPIKeySort(current, 'cache_utilization_rate')
                  )
                }
              />
            </TableRow>
          </TableHeader>
          <TableBody>
            {orderedItems.map((item) => {
              const successRateClassName =
                item.actual_sample_count > 0
                  ? getRateClassName(item.actual_success_rate)
                  : 'text-muted-foreground'
              const cacheUtilizationClassName =
                item.input_tokens > 0
                  ? 'text-foreground'
                  : 'text-muted-foreground'
              return (
                <TableRow key={`${item.api_key_id}:${item.api_key_name}`}>
                  <TableCell>
                    <div className='flex min-w-48 flex-col gap-0.5'>
                      <span className='font-medium'>{getAPIKeyName(item)}</span>
                      {item.api_key_id > 0 ? (
                        <span className='text-muted-foreground text-xs'>
                          ID {item.api_key_id}
                        </span>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {item.actual_sample_count}
                  </TableCell>
                  <TableCell
                    className={cn(
                      'text-right font-mono font-semibold tabular-nums',
                      successRateClassName
                    )}
                  >
                    {formatRate(
                      item.actual_success_rate,
                      item.actual_sample_count
                    )}
                  </TableCell>
                  <TableCell
                    className={cn(
                      'text-right font-mono font-semibold tabular-nums',
                      cacheUtilizationClassName
                    )}
                  >
                    {formatRate(item.cache_utilization_rate, item.input_tokens)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}
