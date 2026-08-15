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

const HISTORY_LABELS = {
  range: '请求与缓存统计范围',
  date: '请求与缓存明细日期',
  chart: '每日成功率、缓存利用率与缓存写请求组合图',
} as const

type ChannelMonitorDailyInsightHistoryProps = {
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
  const dateOptions = useMemo(
    () =>
      [...props.items].reverse().map((item) => ({
        value: item.date,
        label: item.date,
      })),
    [props.items]
  )
  const chartSpec = useMemo(
    () => ({
      type: 'common' as const,
      seriesField: 'metric',
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
              metric: '缓存利用率',
              value: item.input_tokens > 0 ? item.cache_utilization_rate : null,
              selected: item.date === props.selectedDate,
            },
          ]),
        },
        {
          id: 'cache-write-daily',
          values: props.items.map((item) => ({
            date: item.date,
            metric: '缓存写请求',
            value: item.cache_write_request_count,
            selected: item.date === props.selectedDate,
          })),
        },
      ],
      series: [
        {
          type: 'bar' as const,
          id: 'success-cache',
          dataIndex: 0,
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
        },
        {
          type: 'line' as const,
          id: 'cache-write',
          dataIndex: 1,
          xField: 'date',
          yField: 'value',
          seriesField: 'metric',
          line: {
            style: {
              cursor: 'pointer',
              lineWidth: 2,
              opacity: (datum: { selected: boolean }) =>
                datum.selected ? 1 : 0.55,
            },
          },
          point: {
            visible: true,
            style: {
              cursor: 'pointer',
              fillOpacity: (datum: { selected: boolean }) =>
                datum.selected ? 1 : 0.55,
            },
          },
        },
      ],
      legends: { visible: true, orient: 'top' as const },
      tooltip: {
        mark: {
          title: { value: (datum: { date: string }) => datum.date },
          content: [
            {
              key: (datum: { metric: string }) => datum.metric,
              value: (datum: { metric: string; value: number | null }) => {
                if (datum.value == null) return '-'
                if (datum.metric === '缓存写请求') {
                  return `${datum.value} 次`
                }
                return percentFormatter.format(datum.value)
              },
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
          seriesId: ['success-cache'],
          min: 0,
          max: 1,
          title: { visible: true, text: '成功率 / 缓存利用率' },
          label: {
            formatMethod: (value: number | string) =>
              percentFormatter.format(Number(value)),
          },
        },
        {
          orient: 'right' as const,
          seriesId: ['cache-write'],
          min: 0,
          grid: { visible: false },
          title: { visible: true, text: '缓存写请求数' },
          label: {
            formatMethod: (value: number | string) =>
              String(Math.round(Number(value))),
          },
        },
      ],
    }),
    [props.items, props.selectedDate]
  )

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
            <SelectTrigger
              className='w-full sm:w-32'
              aria-label={HISTORY_LABELS.range}
            >
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
              aria-label={HISTORY_LABELS.date}
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
          ariaLabel={HISTORY_LABELS.chart}
          chartKey={`combined:${props.days}`}
          spec={chartSpec}
          onDateChange={props.onDateChange}
        />
      )}
    </section>
  )
}
