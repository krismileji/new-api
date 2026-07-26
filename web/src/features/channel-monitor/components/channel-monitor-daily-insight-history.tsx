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
import { useMemo } from 'react'

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

import type { ChannelMonitorDailyInsightDay } from '../types'
import { ChannelMonitorDailyBarChart } from './channel-monitor-daily-bar-chart'

const DAILY_RANGE_OPTIONS = [
  { value: '7', label: '近 7 天' },
  { value: '30', label: '近 30 天' },
  { value: '90', label: '近 90 天' },
]

const percentFormatter = new Intl.NumberFormat(undefined, {
  style: 'percent',
  maximumFractionDigits: 2,
})

type ChannelMonitorDailyInsightHistoryProps = {
  kind: 'success-cache' | 'cache-write'
  days: number
  selectedDate: string
  items: readonly ChannelMonitorDailyInsightDay[]
  loading: boolean
  onDaysChange: (days: number) => void
  onDateChange: (date: string) => void
}

export function ChannelMonitorDailyInsightHistory(
  props: ChannelMonitorDailyInsightHistoryProps
) {
  const labels =
    props.kind === 'success-cache'
      ? {
          range: '成功率与缓存率统计范围',
          date: '成功率与缓存率明细日期',
          chart: '每日成功率与缓存率柱状图',
        }
      : {
          range: '缓存写统计范围',
          date: '缓存写明细日期',
          chart: '每日缓存写请求柱状图',
        }
  const dateOptions = useMemo(
    () =>
      [...props.items].reverse().map((item) => ({
        value: item.date,
        label: item.date,
      })),
    [props.items]
  )
  const chartSpec = useMemo(() => {
    if (props.kind === 'cache-write') {
      return {
        type: 'bar' as const,
        data: [
          {
            id: 'cache-write-daily',
            values: props.items.map((item) => ({
              date: item.date,
              value: item.cache_write_request_count,
              selected: item.date === props.selectedDate,
            })),
          },
        ],
        xField: 'date',
        yField: 'value',
        bar: {
          style: {
            cornerRadius: [4, 4, 0, 0],
            cursor: 'pointer',
            fillOpacity: (datum: { selected: boolean }) =>
              datum.selected ? 1 : 0.55,
          },
        },
        legends: { visible: false },
        tooltip: {
          mark: {
            title: { value: (datum: { date: string }) => datum.date },
            content: [
              {
                key: '写入请求数',
                value: (datum: { value: number }) => `${datum.value} 次`,
              },
            ],
          },
        },
        axes: [
          {
            orient: 'bottom' as const,
            label: { autoHide: true },
            tick: { visible: false },
          },
          { orient: 'left' as const, label: { formatMethod: Math.round } },
        ],
      }
    }

    return {
      type: 'bar' as const,
      data: [
        {
          id: 'success-cache-daily',
          values: props.items.flatMap((item) => [
            {
              date: item.date,
              metric: '成功率',
              value: item.request_count > 0 ? item.success_rate : null,
              selected: item.date === props.selectedDate,
            },
            {
              date: item.date,
              metric: '缓存率',
              value: item.cache_sample_count > 0 ? item.cache_rate : null,
              selected: item.date === props.selectedDate,
            },
          ]),
        },
      ],
      xField: 'date',
      yField: 'value',
      seriesField: 'metric',
      bar: {
        style: {
          cornerRadius: [4, 4, 0, 0],
          cursor: 'pointer',
          fillOpacity: (datum: { selected: boolean }) =>
            datum.selected ? 1 : 0.55,
        },
      },
      legends: { visible: true, orient: 'top' as const },
      tooltip: {
        mark: {
          title: { value: (datum: { date: string }) => datum.date },
          content: [
            {
              key: (datum: { metric: string }) => datum.metric,
              value: (datum: { value: number | null }) =>
                datum.value == null
                  ? '-'
                  : percentFormatter.format(datum.value),
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom' as const,
          label: { autoHide: true },
          tick: { visible: false },
        },
        {
          orient: 'left' as const,
          min: 0,
          max: 1,
          label: {
            formatMethod: (value: number | string) =>
              percentFormatter.format(Number(value)),
          },
        },
      ],
    }
  }, [props.items, props.kind, props.selectedDate])

  return (
    <section className='flex shrink-0 flex-col gap-3'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <h3 className='text-sm font-medium'>按日趋势</h3>
        <div className='grid grid-cols-2 gap-2 sm:flex'>
          <Select
            items={DAILY_RANGE_OPTIONS}
            value={String(props.days)}
            onValueChange={(value) => {
              const days = Number(value)
              if (Number.isInteger(days)) props.onDaysChange(days)
            }}
          >
            <SelectTrigger className='w-full sm:w-32' aria-label={labels.range}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {DAILY_RANGE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            items={dateOptions}
            value={props.selectedDate}
            disabled={dateOptions.length === 0}
            onValueChange={(value) => {
              if (value) props.onDateChange(value)
            }}
          >
            <SelectTrigger
              className='w-full font-mono sm:w-36'
              aria-label={labels.date}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {dateOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </div>
      {props.loading ? (
        <Skeleton className='h-48 w-full sm:h-56' />
      ) : (
        <ChannelMonitorDailyBarChart
          ariaLabel={labels.chart}
          chartKey={`${props.kind}:${props.days}`}
          spec={chartSpec}
          onDateChange={props.onDateChange}
        />
      )}
    </section>
  )
}
