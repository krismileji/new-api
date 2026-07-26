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
import { useMemo } from 'react'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import type { ChannelMonitorSuccessAPIKeyMetric } from '../types'

type ChannelMonitorSuccessAPIKeyTableProps = {
  items: readonly ChannelMonitorSuccessAPIKeyMetric[]
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

export function ChannelMonitorSuccessAPIKeyTable(
  props: ChannelMonitorSuccessAPIKeyTableProps
) {
  const orderedItems = useMemo(
    () =>
      [...props.items].sort((first, second) => {
        const firstSuccessRate =
          first.actual_sample_count > 0 &&
          Number.isFinite(first.actual_success_rate)
            ? first.actual_success_rate
            : -1
        const secondSuccessRate =
          second.actual_sample_count > 0 &&
          Number.isFinite(second.actual_success_rate)
            ? second.actual_success_rate
            : -1
        if (firstSuccessRate !== secondSuccessRate) {
          return secondSuccessRate - firstSuccessRate
        }

        const firstCacheRate =
          first.cache_sample_count > 0 && Number.isFinite(first.cache_hit_rate)
            ? first.cache_hit_rate
            : -1
        const secondCacheRate =
          second.cache_sample_count > 0 &&
          Number.isFinite(second.cache_hit_rate)
            ? second.cache_hit_rate
            : -1
        if (firstCacheRate !== secondCacheRate) {
          return secondCacheRate - firstCacheRate
        }
        if (first.actual_sample_count !== second.actual_sample_count) {
          return second.actual_sample_count - first.actual_sample_count
        }
        if (first.api_key_id !== second.api_key_id) {
          return first.api_key_id - second.api_key_id
        }
        return first.api_key_name.localeCompare(second.api_key_name)
      }),
    [props.items]
  )

  if (props.items.length === 0) return null

  return (
    <section
      data-slot='channel-monitor-success-api-key-details'
      className='flex shrink-0 flex-col gap-2'
    >
      <h3 className='font-medium'>API Key 明细</h3>
      <div className='overflow-auto rounded-lg border'>
        <Table className='min-w-[640px]'>
          <TableHeader>
            <TableRow>
              <TableHead>API Key</TableHead>
              <TableHead className='text-right'>请求数</TableHead>
              <TableHead className='text-right'>成功率</TableHead>
              <TableHead className='text-right'>缓存率</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {orderedItems.map((item) => {
              const successRateClassName =
                item.actual_sample_count > 0
                  ? getRateClassName(item.actual_success_rate)
                  : 'text-muted-foreground'
              const cacheRateClassName =
                item.cache_sample_count > 0
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
                      cacheRateClassName
                    )}
                  >
                    {formatRate(item.cache_hit_rate, item.cache_sample_count)}
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
