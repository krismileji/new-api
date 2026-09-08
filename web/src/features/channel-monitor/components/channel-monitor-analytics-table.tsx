import {
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState, type ReactNode } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTokens } from '@/features/rankings/lib/format'

import { useChannelMonitorAnalytics } from '../hooks/use-channel-monitor-analytics'
import {
  getChannelMonitorAnalyticsChildGroupBy,
  type ChannelMonitorAnalyticsExpansionContext,
} from '../lib/analytics-expansion'
import { isChannelMonitorAnalyticsCoverageIncomplete } from '../lib/coverage'
import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
} from '../lib/format'
import type {
  ChannelMonitorAnalyticsChannel,
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsItem,
  ChannelMonitorAnalyticsMetric,
  ChannelMonitorAnalyticsQuery,
  ChannelMonitorAnalyticsSort,
} from '../types-analytics'
import { ChannelMonitorAnalyticsCoverage } from './channel-monitor-analytics-coverage'
import {
  ChannelMonitorSortableTableHead,
  type ChannelMonitorSortDirection,
} from './channel-monitor-sortable-table-head'

type ChannelMonitorAnalyticsTableProps = {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  items: readonly ChannelMonitorAnalyticsItem[]
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
  onSelect?: (item: ChannelMonitorAnalyticsItem) => void
  expandedKey?: string
}

const systemAPIKeySources = new Map([
  ['status_probe', '状态探测'],
  ['group_probe', '分组监控探测'],
  ['smart_probe', '智能调度探测'],
  ['manual_test', '手动测试'],
  ['model_detection', '模型检测'],
])

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
      item.user_name ||
      item.user_display_name ||
      (item.user_id && item.user_id > 0
        ? `用户 #${item.user_id}`
        : '未归属用户')
    )
  }
  if (groupBy === 'api_key' || groupBy === 'api_key_channel_model') {
    const source = systemAPIKeySources.get(item.api_key_key ?? '')
    if (!item.api_key_id && source) return source
    if (item.api_key_name) return item.api_key_name
    return item.api_key_id && item.api_key_id > 0
      ? `API Key #${item.api_key_id}`
      : '未识别 API Key'
  }
  if (groupBy === 'model') {
    return item.model_name && item.model_name !== 'unknown'
      ? item.model_name
      : '未知模型'
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

type AnalyticsTableHeaderProps = {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  sort?: ChannelMonitorAnalyticsSort
  direction?: ChannelMonitorSortDirection
  onSort?: (sort: ChannelMonitorAnalyticsSort) => void
}

function AnalyticsTableHeader(props: AnalyticsTableHeaderProps) {
  let primaryLabel = '维度'
  if (props.groupBy === 'user') primaryLabel = '用户'
  if (props.groupBy === 'api_key') primaryLabel = 'API Key'
  if (props.groupBy === 'model') primaryLabel = '模型'
  if (props.groupBy === 'channel') primaryLabel = '渠道'
  if (props.groupBy === 'channel_model') primaryLabel = '渠道'
  if (props.groupBy === 'api_key_channel_model') primaryLabel = 'API Key'
  const metricHead = (label: string, sort: ChannelMonitorAnalyticsSort) => {
    if (!props.onSort) {
      return <TableHead className='text-right'>{label}</TableHead>
    }
    return (
      <ChannelMonitorSortableTableHead
        label={label}
        align='right'
        className='min-w-24'
        direction={props.sort === sort ? props.direction : undefined}
        onSort={() => props.onSort?.(sort)}
      />
    )
  }
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
            {metricHead('上游尝试数', 'samples')}
            {metricHead('上游成功率', 'success_rate')}
            {metricHead('流式缓存利用率', 'cache_utilization')}
            {metricHead('缓存写入次数', 'cache_write')}
          </>
        ) : (
          <>
            {metricHead('成本', 'cost')}
            {metricHead('已结算', 'settled')}
            {metricHead('未解析', 'unresolved')}
            {metricHead('解析率', 'resolution_rate')}
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
  expandedKey?: string
}) {
  const item = props.item
  const selectable = props.onSelect != null
  const expanded = selectable && props.expandedKey === item.key
  const primaryLabel = getPrimaryLabel(props.groupBy, item, props.channels)
  const secondaryLabel = getSecondaryLabel(props.groupBy, item, props.channels)
  const action = selectable ? (
    <Button
      type='button'
      variant='ghost'
      className='h-auto max-w-full min-w-0 justify-start gap-1 px-1 py-1 text-left'
      onClick={() => props.onSelect?.(item)}
      aria-label={`查看${primaryLabel}明细`}
      aria-expanded={expanded}
    >
      <HugeiconsIcon
        icon={expanded ? ArrowDown01Icon : ArrowRight01Icon}
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
        {secondaryLabel ? (
          <span className='text-muted-foreground block truncate pl-6 text-xs'>
            {secondaryLabel}
          </span>
        ) : null}
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
      <AnalyticsTableMetricCells metric={props.metric} item={item} />
    </TableRow>
  )
}

function AnalyticsTableMetricCells(props: {
  metric: ChannelMonitorAnalyticsMetric
  item: ChannelMonitorAnalyticsItem
}) {
  if (props.metric === 'success') {
    return (
      <>
        <TableCell className='text-right font-mono tabular-nums'>
          {props.item.actual_sample_count}
        </TableCell>
        <TableCell className='text-right font-mono tabular-nums'>
          {formatRate(
            props.item.actual_success_rate,
            props.item.actual_sample_count
          )}
          <span className='text-muted-foreground block text-xs'>
            {props.item.actual_success_count} / {props.item.actual_sample_count}{' '}
            次
          </span>
        </TableCell>
        <TableCell className='text-right font-mono tabular-nums'>
          {formatRate(
            props.item.cache_utilization_rate,
            props.item.input_tokens
          )}
          <span
            className='text-muted-foreground block text-xs'
            title={`${(props.item.cache_read_tokens ?? 0).toLocaleString('zh-CN')} / ${(props.item.input_tokens ?? 0).toLocaleString('zh-CN')} Token`}
          >
            {formatTokens(props.item.cache_read_tokens)} /{' '}
            {formatTokens(props.item.input_tokens)} Token
          </span>
        </TableCell>
        <TableCell className='text-right font-mono tabular-nums'>
          {props.item.cache_write_request_count}
        </TableCell>
      </>
    )
  }
  return (
    <>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatChannelMonitorCost(
          (props.item.cost_nano_cny ?? 0) / 1_000_000_000
        )}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {props.item.settled_count ?? 0}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {props.item.unresolved_count ?? 0}
      </TableCell>
      <TableCell className='text-right font-mono tabular-nums'>
        {formatChannelMonitorResolutionRate(
          props.item.settled_count ?? 0,
          props.item.unresolved_count ?? 0
        )}
      </TableCell>
    </>
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
    const modelLabel = item.model_name || item.model_key
    return modelLabel
      ? `${channelLabel} · ${keyLabel} · 模型 ${modelLabel}`
      : `${channelLabel} · ${keyLabel}`
  }
  if (groupBy === 'api_key') {
    if (!item.api_key_id && systemAPIKeySources.has(item.api_key_key ?? '')) {
      return '系统探测 · 未使用 API Key'
    }
    return item.api_key_id && item.api_key_id > 0
      ? `Key ID ${item.api_key_id}`
      : 'Key ID 未知'
  }
  if (groupBy === 'user') {
    const labels: string[] = []
    if (item.user_id && item.user_id > 0) labels.push(`ID ${item.user_id}`)
    const displayName = item.user_display_name?.trim()
    const username = item.user_name?.trim()
    if (displayName && displayName !== username) {
      labels.push(displayName)
    }
    return labels.join(' · ') || '历史归属未知'
  }
  if (groupBy === 'channel' || groupBy === 'channel_model') {
    const channelID = item.channel_id ? `ID ${item.channel_id}` : '渠道未知'
    const channel = channels.get(item.channel_id ?? 0)
    return channel?.remark ? `${channelID} · ${channel.remark}` : channelID
  }
  if (groupBy === 'model') return ''
  if (groupBy === 'day') return item.key
  return channels.size > 0 ? '' : item.key
}

function queryFromExpansionContext(
  context: ChannelMonitorAnalyticsExpansionContext,
  groupBy: ChannelMonitorAnalyticsGroupBy,
  item?: ChannelMonitorAnalyticsItem,
  parentGroupBy?: ChannelMonitorAnalyticsGroupBy
): ChannelMonitorAnalyticsQuery {
  return {
    metric: context.metric,
    groupBy,
    from: context.from,
    to: context.to,
    channelId:
      context.channelId ??
      (parentGroupBy === 'channel' ? item?.channel_id : undefined),
    userId:
      context.userId ?? (parentGroupBy === 'user' ? item?.user_id : undefined),
    apiKeyId:
      context.apiKeyId ??
      (parentGroupBy === 'api_key' ? item?.api_key_id : undefined),
    apiKeyKey:
      context.apiKeyKey ??
      (parentGroupBy === 'api_key' ? item?.api_key_key : undefined),
    model:
      context.model ??
      (parentGroupBy === 'model'
        ? item?.model_name || item?.model_key
        : undefined),
    modelKey:
      context.modelKey ??
      (parentGroupBy === 'model' ? item?.model_key : undefined),
    search: context.search,
    sort: context.sort,
    direction: context.direction,
    page: 1,
    pageSize: 20,
  }
}

function getAnalyticsRowKey(
  groupBy: ChannelMonitorAnalyticsGroupBy,
  item: ChannelMonitorAnalyticsItem
) {
  if (groupBy === 'user') return `user:${item.user_id ?? 0}`
  if (groupBy === 'api_key') {
    return `key:${item.user_id ?? 0}:${item.api_key_id ?? 0}:${item.api_key_key ?? ''}`
  }
  if (groupBy === 'model') {
    return `model:${item.model_key ?? item.model_name ?? item.key}`
  }
  if (groupBy === 'channel') return `channel:${item.channel_id ?? 0}`
  return `${groupBy}:${item.key}:${item.channel_id ?? 0}:${item.user_id ?? 0}:${item.api_key_id ?? 0}:${item.api_key_key ?? ''}:${item.model_key ?? ''}`
}

function AnalyticsExpandableTableRow(props: {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  item: ChannelMonitorAnalyticsItem
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
  context: ChannelMonitorAnalyticsExpansionContext
  depth: number
  columnCount: number
}) {
  const childGroupBy = getChannelMonitorAnalyticsChildGroupBy(
    props.context.tab,
    props.groupBy
  )
  const [expanded, setExpanded] = useState(false)
  const [childPage, setChildPage] = useState(1)
  const childRequest = childGroupBy
    ? {
        ...queryFromExpansionContext(
          props.context,
          childGroupBy,
          props.item,
          props.groupBy
        ),
        page: childPage,
      }
    : null
  const childQuery = useChannelMonitorAnalytics(
    childRequest ?? queryFromExpansionContext(props.context, props.groupBy),
    expanded && childRequest != null
  )
  const childQueryResponse = childQuery.data?.data
  const childResponse =
    childGroupBy != null && childQueryResponse?.group_by === childGroupBy
      ? childQueryResponse
      : undefined
  const primaryLabel = getPrimaryLabel(
    props.groupBy,
    props.item,
    props.channels
  )
  const secondaryLabel = getSecondaryLabel(
    props.groupBy,
    props.item,
    props.channels
  )
  const childContext = childRequest
    ? {
        ...props.context,
        channelId: childRequest.channelId,
        userId: childRequest.userId,
        apiKeyId: childRequest.apiKeyId,
        apiKeyKey: childRequest.apiKeyKey,
        model: childRequest.model,
        modelKey: childRequest.modelKey,
      }
    : null
  let action: ReactNode
  if (childGroupBy) {
    action = (
      <Button
        type='button'
        variant='ghost'
        className='h-auto max-w-full min-w-0 justify-start gap-1 px-1 py-1 text-left'
        onClick={() => setExpanded((value) => !value)}
        aria-label={`查看${primaryLabel}明细`}
        aria-expanded={expanded}
      >
        <HugeiconsIcon
          icon={expanded ? ArrowDown01Icon : ArrowRight01Icon}
          aria-hidden='true'
          className='size-4 shrink-0'
        />
        <span className='min-w-0 truncate font-medium' title={primaryLabel}>
          {primaryLabel}
        </span>
      </Button>
    )
  } else {
    action = (
      <span className='block truncate font-medium' title={primaryLabel}>
        {primaryLabel}
      </span>
    )
  }
  let expandedRows: ReactNode = null
  if (expanded && childGroupBy) {
    if (childQuery.isLoading || (childQuery.isFetching && !childResponse)) {
      expandedRows = (
        <TableRow className='bg-muted/5'>
          <TableCell colSpan={props.columnCount}>
            <Skeleton className='h-10 w-full' />
          </TableCell>
        </TableRow>
      )
    } else if (childQuery.isError && !childResponse) {
      expandedRows = (
        <TableRow className='bg-muted/5'>
          <TableCell colSpan={props.columnCount}>
            <Alert variant='destructive'>
              <AlertTitle>明细加载失败</AlertTitle>
              <AlertDescription className='flex items-center justify-between gap-3'>
                <span>{childQuery.error?.message || '请稍后重试'}</span>
                {childPage > 1 ? (
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    aria-label={`${primaryLabel}明细上一页`}
                    onClick={() =>
                      setChildPage((page) => Math.max(1, page - 1))
                    }
                  >
                    上一页
                  </Button>
                ) : null}
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void childQuery.refetch()}
                  disabled={childQuery.isFetching}
                >
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    data-icon='inline-start'
                  />
                  重试
                </Button>
              </AlertDescription>
            </Alert>
          </TableCell>
        </TableRow>
      )
    } else if (childResponse?.coverage.status === 'unavailable') {
      expandedRows = null
    } else if (childResponse && childResponse.items.length > 0) {
      expandedRows = childResponse.items.map((item) => (
        <AnalyticsExpandableTableRow
          key={getAnalyticsRowKey(childGroupBy, item)}
          metric={props.metric}
          groupBy={childGroupBy}
          item={item}
          channels={props.channels}
          context={childContext ?? props.context}
          depth={props.depth + 1}
          columnCount={props.columnCount}
        />
      ))
    } else {
      expandedRows = (
        <TableRow className='bg-muted/5'>
          <TableCell
            colSpan={props.columnCount}
            className='text-muted-foreground text-xs'
          >
            暂无可展开的明细
          </TableCell>
        </TableRow>
      )
    }
  }

  const childPageCount = childResponse
    ? Math.max(1, Math.ceil(childResponse.total / childResponse.page_size))
    : 1

  return (
    <>
      <TableRow className={props.depth > 0 ? 'bg-muted/10' : undefined}>
        <TableCell
          className='min-w-48'
          style={{ paddingInlineStart: `${0.5 + props.depth * 1.25}rem` }}
        >
          {action}
          {secondaryLabel ? (
            <span className='text-muted-foreground block truncate pl-6 text-xs'>
              {secondaryLabel}
            </span>
          ) : null}
        </TableCell>
        <AnalyticsTableMetricCells metric={props.metric} item={props.item} />
      </TableRow>
      {expanded &&
      isChannelMonitorAnalyticsCoverageIncomplete(childResponse?.coverage) ? (
        <TableRow>
          <TableCell colSpan={props.columnCount}>
            <ChannelMonitorAnalyticsCoverage
              coverage={childResponse?.coverage}
              scope={`${primaryLabel}明细`}
            />
          </TableCell>
        </TableRow>
      ) : null}
      {expanded && childQuery.isError && childResponse ? (
        <TableRow>
          <TableCell colSpan={props.columnCount}>
            <Alert variant='destructive'>
              <AlertTitle>明细更新失败，保留上次结果</AlertTitle>
              <AlertDescription>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void childQuery.refetch()}
                >
                  重试
                </Button>
              </AlertDescription>
            </Alert>
          </TableCell>
        </TableRow>
      ) : null}
      {expandedRows}
      {expanded && childResponse && (childPageCount > 1 || childPage > 1) ? (
        <TableRow className='bg-muted/5'>
          <TableCell colSpan={props.columnCount}>
            <nav
              aria-label={`${primaryLabel}明细分页`}
              className='flex items-center justify-between gap-3 text-xs'
            >
              <span className='text-muted-foreground'>
                第 {childResponse.page} / {childPageCount} 页 · 共{' '}
                {childResponse.total} 条
              </span>
              <div className='flex items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  aria-label={`${primaryLabel}明细上一页`}
                  disabled={childPage <= 1 || childQuery.isFetching}
                  onClick={() => setChildPage((page) => Math.max(1, page - 1))}
                >
                  <HugeiconsIcon icon={ArrowLeft01Icon} />
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  aria-label={`${primaryLabel}明细下一页`}
                  disabled={
                    childPage >= childPageCount || childQuery.isFetching
                  }
                  onClick={() =>
                    setChildPage((page) => Math.min(childPageCount, page + 1))
                  }
                >
                  <HugeiconsIcon icon={ArrowRight01Icon} />
                </Button>
              </div>
            </nav>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}

type ChannelMonitorAnalyticsExpandableTableProps = {
  metric: ChannelMonitorAnalyticsMetric
  groupBy: ChannelMonitorAnalyticsGroupBy
  items: readonly ChannelMonitorAnalyticsItem[]
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
  context: ChannelMonitorAnalyticsExpansionContext
  onSort: (sort: ChannelMonitorAnalyticsSort) => void
}

export function ChannelMonitorAnalyticsExpandableTable(
  props: ChannelMonitorAnalyticsExpandableTableProps
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
  const columnCount =
    props.groupBy === 'channel_model' ||
    props.groupBy === 'api_key_channel_model'
      ? 6
      : 5
  return (
    <div className='shrink-0 overflow-x-auto rounded-lg border'>
      <Table className='min-w-[42rem]'>
        <AnalyticsTableHeader
          metric={props.metric}
          groupBy={props.groupBy}
          sort={props.context.sort}
          direction={props.context.direction}
          onSort={props.onSort}
        />
        <TableBody>
          {props.items.map((item) => (
            <AnalyticsExpandableTableRow
              key={getAnalyticsRowKey(props.groupBy, item)}
              metric={props.metric}
              groupBy={props.groupBy}
              item={item}
              channels={props.channels}
              context={props.context}
              depth={0}
              columnCount={columnCount}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  )
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
    <div className='shrink-0 overflow-x-auto rounded-lg border'>
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
              expandedKey={props.expandedKey}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
