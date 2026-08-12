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
import { ChartAverageIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

import type {
  ChannelMonitorSuccessAPIKeyMetric,
  ChannelMonitorTodaySuccessResult,
} from '../types'

type ChannelMonitorTodaySuccessCardProps = {
  result: ChannelMonitorTodaySuccessResult | undefined
  isLoading: boolean
  isError: boolean
  onOpen: () => void
}

const percentFormatter = new Intl.NumberFormat(undefined, {
  style: 'percent',
  maximumFractionDigits: 2,
})

const EMPTY_CACHE_WRITE_ITEMS: ChannelMonitorTodaySuccessResult['cache_write_items'] =
  []
const EMPTY_API_KEY_ITEMS: ChannelMonitorTodaySuccessResult['api_key_items'] =
  []
const ALL_API_KEYS_VALUE = 'all'

function formatRate(rate: number, sampleCount: number) {
  return sampleCount > 0 ? percentFormatter.format(rate) : '-'
}

function getAPIKeyValue(item: ChannelMonitorSuccessAPIKeyMetric) {
  return item.api_key_id > 0
    ? `id:${item.api_key_id}`
    : `name:${encodeURIComponent(item.api_key_name || 'unknown')}`
}

function getAPIKeyName(item: ChannelMonitorSuccessAPIKeyMetric) {
  if (item.api_key_name) return item.api_key_name
  if (item.api_key_id > 0) return `未命名 API Key #${item.api_key_id}`
  return '未识别 API Key'
}

function getAPIKeyOptionLabel(item: ChannelMonitorSuccessAPIKeyMetric) {
  const name = getAPIKeyName(item)
  if (item.api_key_name && item.api_key_id > 0) {
    return `${name} · ID ${item.api_key_id}`
  }
  return name
}

export function ChannelMonitorTodaySuccessCard(
  props: ChannelMonitorTodaySuccessCardProps
) {
  const summary = props.result?.summary
  const apiKeyItems = props.result?.api_key_items ?? EMPTY_API_KEY_ITEMS
  const [selectedAPIKeyValue, setSelectedAPIKeyValue] =
    useState(ALL_API_KEYS_VALUE)
  const apiKeyOptions = useMemo(
    () => [
      { value: ALL_API_KEYS_VALUE, label: '全部 API Key' },
      ...apiKeyItems.map((item) => ({
        value: getAPIKeyValue(item),
        label: getAPIKeyOptionLabel(item),
      })),
    ],
    [apiKeyItems]
  )
  const selectedAPIKeyMetric = apiKeyItems.find(
    (item) => getAPIKeyValue(item) === selectedAPIKeyValue
  )
  const effectiveAPIKeyValue = selectedAPIKeyMetric
    ? selectedAPIKeyValue
    : ALL_API_KEYS_VALUE
  const cacheMetric = selectedAPIKeyMetric ?? summary
  const cacheScopeLabel = selectedAPIKeyMetric
    ? getAPIKeyName(selectedAPIKeyMetric)
    : '全部 API Key'
  const metricsAvailable = props.result?.success_metrics_available ?? false
  const successRate =
    !props.isLoading && !props.isError && metricsAvailable && summary
      ? formatRate(summary.actual_success_rate, summary.actual_sample_count)
      : '-'
  const cacheRate =
    !props.isLoading && !props.isError && metricsAvailable && cacheMetric
      ? formatRate(cacheMetric.cache_hit_rate, cacheMetric.cache_sample_count)
      : '-'
  const cacheWriteMetricsAvailable =
    props.result?.cache_write_metrics_available ?? false
  const cacheWriteItems =
    props.result?.cache_write_items ?? EMPTY_CACHE_WRITE_ITEMS
  const hasCacheWriteMetrics =
    !props.isLoading && !props.isError && cacheWriteMetricsAvailable
  const cacheWriteChannelCount = hasCacheWriteMetrics
    ? cacheWriteItems.length
    : null
  const cacheWriteRequestCount = hasCacheWriteMetrics
    ? cacheWriteItems.reduce((total, item) => total + item.request_count, 0)
    : null
  const cacheWriteChannelLabel =
    cacheWriteChannelCount == null ? '-' : `${cacheWriteChannelCount} 个`
  const cacheWriteRequestLabel =
    cacheWriteRequestCount == null ? '-' : `${cacheWriteRequestCount} 次`

  let description = '按北京时间统计真实调用结果'
  if (props.isLoading) {
    description = '正在统计今日请求'
  } else if (props.isError) {
    description = '今日请求统计加载失败'
  } else if (!metricsAvailable) {
    description = '日志统计未开启'
  } else if (!summary || summary.actual_sample_count === 0) {
    description = '今日暂无请求数据'
  } else {
    description = `${summary.actual_sample_count} 次请求 · ${cacheMetric?.cache_sample_count ?? 0} 次缓存样本`
  }

  return (
    <Card size='sm' className='h-full gap-0 py-0'>
      <CardHeader
        className='hover:bg-muted/50 focus-visible:ring-ring/50 cursor-pointer rounded-t-xl py-3 transition-colors outline-none focus-visible:ring-3 focus-visible:ring-inset'
        role='button'
        tabIndex={0}
        onClick={props.onOpen}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            props.onOpen()
          }
        }}
        aria-label={`查看今日成功率、缓存率和缓存写明细，成功率 ${successRate}，缓存率 ${cacheRate}，缓存率口径 ${cacheScopeLabel}，缓存写渠道 ${cacheWriteChannelLabel}，缓存写请求 ${cacheWriteRequestLabel}`}
      >
        <CardDescription>今日成功率 / 缓存率</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>
          {props.isLoading ? (
            <Skeleton className='h-7 w-28' />
          ) : (
            <span className='flex items-baseline gap-2'>
              <span>{successRate}</span>
              <span className='text-muted-foreground text-base font-normal'>
                /
              </span>
              <span data-slot='today-cache-rate'>{cacheRate}</span>
            </span>
          )}
        </CardTitle>
        <CardAction>
          <span className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={ChartAverageIcon} aria-hidden='true' />
          </span>
        </CardAction>
        <CardDescription className='flex flex-col gap-1'>
          <span>{description}</span>
          <span className='flex flex-wrap gap-x-3 gap-y-1'>
            <span>
              缓存写渠道{' '}
              <span className='text-foreground font-mono font-medium tabular-nums'>
                {cacheWriteChannelLabel}
              </span>
            </span>
            <span>
              缓存写请求{' '}
              <span className='text-foreground font-mono font-medium tabular-nums'>
                {cacheWriteRequestLabel}
              </span>
            </span>
          </span>
        </CardDescription>
      </CardHeader>
      <CardContent className='bg-muted/20 mt-auto border-t py-2.5'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='text-muted-foreground shrink-0 text-xs'>
            缓存率口径
          </span>
          <Select
            items={apiKeyOptions}
            value={effectiveAPIKeyValue}
            disabled={
              props.isLoading ||
              props.isError ||
              !metricsAvailable ||
              apiKeyItems.length === 0
            }
            onValueChange={(value) => {
              if (value) setSelectedAPIKeyValue(value)
            }}
          >
            <SelectTrigger
              size='sm'
              className='min-w-0 flex-1'
              aria-label='选择缓存率 API Key'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent align='end' alignItemWithTrigger={false}>
              <SelectGroup>
                {apiKeyOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
      </CardContent>
    </Card>
  )
}
