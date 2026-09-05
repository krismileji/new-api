import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import { Button } from '@/components/ui/button'
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
} from '../lib/format'
import type {
  ChannelMonitorAnalyticsChannel,
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsItem,
  ChannelMonitorAnalyticsMetric,
} from '../types-analytics'

type ChannelMonitorAnalyticsTableProps = {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  items: readonly ChannelMonitorAnalyticsItem[]
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
  onSelect?: (item: ChannelMonitorAnalyticsItem) => void
}

function formatRate(value: number, denominator: number) {
  if (denominator <= 0 || !Number.isFinite(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

function getPrimaryLabel(
  groupBy: ChannelMonitorAnalyticsGroupBy,
  item: ChannelMonitorAnalyticsItem,
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
) {
  if (groupBy === 'channel' || groupBy === 'channel_model') {
    return (
      channels.get(item.channel_id ?? 0)?.name ??
      `渠道 #${item.channel_id ?? 0}`
    )
  }
  if (groupBy === 'user') {
    return (
      item.user_display_name ||
      item.user_name ||
      (item.user_id && item.user_id > 0
        ? `用户 #${item.user_id}`
        : '未归属用户')
    )
  }
  if (groupBy === 'api_key' || groupBy === 'api_key_channel_model') {
    if (item.api_key_name) return item.api_key_name
    return item.api_key_id && item.api_key_id > 0
      ? `API Key #${item.api_key_id}`
      : '未识别 API Key'
  }
  if (groupBy === 'model') {
    return item.model_name || item.model_key || '未知模型'
  }
  if (groupBy === 'day') {
    return item.day_start ? formatDay(item.day_start) : item.key
  }
  return item.key
}

function formatDay(timestamp: number) {
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(timestamp * 1000))
}

function AnalyticsTableHeader(props: {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
}) {
  let primaryLabel = '维度'
  if (props.groupBy === 'channel_model') primaryLabel = '渠道'
  if (props.groupBy === 'api_key_channel_model') primaryLabel = 'API Key'
  return (
    <TableHeader className='bg-muted/30'>
      <TableRow>
        <TableHead className='min-w-48'>{primaryLabel}</TableHead>
        {props.groupBy === 'channel_model' ||
        props.groupBy === 'api_key_channel_model' ? (
          <TableHead className='min-w-40'>模型</TableHead>
        ) : null}
        {props.metric === 'success' ? (
          <>
            <TableHead className='text-right'>调用数</TableHead>
            <TableHead className='text-right'>成功率</TableHead>
            <TableHead className='text-right'>缓存利用率</TableHead>
            <TableHead className='text-right'>缓存写入</TableHead>
          </>
        ) : (
          <>
            <TableHead className='text-right'>成本</TableHead>
            <TableHead className='text-right'>已结算</TableHead>
            <TableHead className='text-right'>未解析</TableHead>
            <TableHead className='text-right'>解析率</TableHead>
          </>
        )}
      </TableRow>
    </TableHeader>
  )
}

function AnalyticsTableRow(props: {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  item: ChannelMonitorAnalyticsItem
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
  onSelect?: (item: ChannelMonitorAnalyticsItem) => void
}) {
  const item = props.item
  const selectable = props.onSelect != null
  const primaryLabel = getPrimaryLabel(props.groupBy, item, props.channels)
  const action = selectable ? (
    <Button
      type='button'
      variant='ghost'
      className='h-auto max-w-full min-w-0 justify-start gap-1 px-1 py-1 text-left'
      onClick={() => props.onSelect?.(item)}
      aria-label={`查看${primaryLabel}明细`}
    >
      <HugeiconsIcon
        icon={ArrowRight01Icon}
        aria-hidden='true'
        className='size-4 shrink-0'
      />
      <span className='min-w-0 truncate font-medium' title={primaryLabel}>
        {primaryLabel}
      </span>
    </Button>
  ) : (
    <span className='block truncate font-medium' title={primaryLabel}>
      {primaryLabel}
    </span>
  )
  return (
    <TableRow>
      <TableCell className='min-w-48'>
        {action}
        <span className='text-muted-foreground block truncate pl-6 text-xs'>
          {getSecondaryLabel(props.groupBy, item, props.channels)}
        </span>
      </TableCell>
      {props.groupBy === 'channel_model' ||
      props.groupBy === 'api_key_channel_model' ? (
        <TableCell
          className='max-w-56 truncate'
          title={item.model_name || item.model_key}
        >
          {item.model_name || item.model_key || '未知模型'}
        </TableCell>
      ) : null}
      {props.metric === 'success' ? (
        <>
          <TableCell className='text-right font-mono tabular-nums'>
            {item.actual_sample_count}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {formatRate(item.actual_success_rate, item.actual_sample_count)}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {formatRate(item.cache_utilization_rate, item.input_tokens)}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {item.cache_write_request_count}
          </TableCell>
        </>
      ) : (
        <>
          <TableCell className='text-right font-mono tabular-nums'>
            {formatChannelMonitorCost(
              (item.cost_nano_cny ?? 0) / 1_000_000_000
            )}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {item.settled_count ?? 0}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {item.unresolved_count ?? 0}
          </TableCell>
          <TableCell className='text-right font-mono tabular-nums'>
            {formatChannelMonitorResolutionRate(
              item.settled_count ?? 0,
              item.unresolved_count ?? 0
            )}
          </TableCell>
        </>
      )}
    </TableRow>
  )
}

function getSecondaryLabel(
  groupBy: ChannelMonitorAnalyticsGroupBy,
  item: ChannelMonitorAnalyticsItem,
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
) {
  if (groupBy === 'api_key_channel_model') {
    const channelLabel =
      item.channel_id && item.channel_id > 0
        ? (channels.get(item.channel_id)?.name ?? `渠道 #${item.channel_id}`)
        : '渠道未知'
    const keyLabel =
      item.api_key_id && item.api_key_id > 0
        ? `Key ID ${item.api_key_id}`
        : 'Key ID 未知'
    return `${channelLabel} · ${keyLabel}`
  }
  if (groupBy === 'api_key') {
    return item.api_key_id && item.api_key_id > 0
      ? `ID ${item.api_key_id}`
      : 'ID 未知'
  }
  if (groupBy === 'user') {
    return item.user_id && item.user_id > 0
      ? `ID ${item.user_id}`
      : '历史归属未知'
  }
  if (groupBy === 'channel' || groupBy === 'channel_model') {
    const channelID = item.channel_id ? `ID ${item.channel_id}` : '渠道未知'
    const channel = channels.get(item.channel_id ?? 0)
    return channel?.remark ? `${channelID} · ${channel.remark}` : channelID
  }
  if (groupBy === 'model') return item.model_key || '模型标识未知'
  if (groupBy === 'day') return item.key
  return channels.size > 0 ? '' : item.key
}

export function ChannelMonitorAnalyticsTable(
  props: ChannelMonitorAnalyticsTableProps
) {
  if (props.items.length === 0) {
    return (
      <Empty className='min-h-48 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={ArrowRight01Icon} />
          </EmptyMedia>
          <EmptyTitle>暂无统计数据</EmptyTitle>
          <EmptyDescription>当前范围没有可展示的明细</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className='overflow-x-auto rounded-lg border'>
      <Table className='min-w-[42rem]'>
        <AnalyticsTableHeader metric={props.metric} groupBy={props.groupBy} />
        <TableBody>
          {props.items.map((item) => (
            <AnalyticsTableRow
              key={`${item.key}:${item.channel_id ?? 0}:${item.user_id ?? 0}:${item.api_key_id ?? 0}:${item.model_key ?? ''}`}
              metric={props.metric}
              groupBy={props.groupBy}
              item={item}
              channels={props.channels}
              onSelect={props.onSelect}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
