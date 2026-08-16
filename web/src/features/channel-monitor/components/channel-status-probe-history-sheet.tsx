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
import {
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Refresh01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useMemo, useState, type ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { useIsMobile } from '@/hooks/use-mobile'
import { formatTimestampToDate } from '@/lib/format'

import { getChannelStatusProbeExecutions } from '../api'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
  getChannelStatusProbeHistoryLatestExecutionKey,
} from '../lib/query-options'
import { isChannelStatusProbeActive } from '../lib/status-probe'
import type {
  ChannelStatusProbeChannel,
  ChannelStatusProbeExecution,
  ChannelStatusProbeResult,
  ChannelStatusProbeTrigger,
} from '../types'

type ChannelStatusProbeHistorySheetProps = {
  channel: ChannelStatusProbeChannel
  open: boolean
  actionPending: boolean
  onOpenChange: (open: boolean) => void
  onOpenConfig: () => void
  onRun: () => void
}

const RESULT_OPTIONS: Array<{
  value: null | ChannelStatusProbeResult
  label: string
}> = [
  { value: null, label: '全部结果' },
  { value: 'success', label: '成功' },
  { value: 'upstream_failure', label: '上游失败' },
  { value: 'rate_limited', label: '限流' },
  { value: 'local_failure', label: '本地失败' },
  { value: 'skipped', label: '跳过' },
  { value: 'canceled', label: '取消' },
]

const TRIGGER_OPTIONS: Array<{
  value: null | ChannelStatusProbeTrigger
  label: string
}> = [
  { value: null, label: '全部触发方式' },
  { value: 'scheduled', label: '周期' },
  { value: 'manual', label: '手动' },
]

const RESULT_LABEL: Record<ChannelStatusProbeResult, string> = {
  success: '成功',
  upstream_failure: '上游失败',
  rate_limited: '限流',
  local_failure: '本地失败',
  skipped: '跳过',
  canceled: '取消',
}

function resultVariant(result: ChannelStatusProbeResult) {
  if (result === 'success') return 'secondary' as const
  if (result === 'upstream_failure') return 'destructive' as const
  if (result === 'rate_limited' || result === 'local_failure') {
    return 'warning' as const
  }
  return 'outline' as const
}

function formatMetric(value: number | null, unit: 'ms' | 'tps') {
  if (value == null) return '-'
  if (unit === 'tps') return value.toFixed(value >= 100 ? 0 : 1)
  if (value >= 1000) return `${(value / 1000).toFixed(2)} s`
  return `${Math.round(value)} ms`
}

function ChannelStatusProbeExecutionRow(props: {
  execution: ChannelStatusProbeExecution
}) {
  const execution = props.execution
  return (
    <article className='border-b px-4 py-4 last:border-b-0'>
      <div className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='truncate font-medium' title={execution.model_name}>
            {execution.model_name}
          </div>
          <div className='text-muted-foreground mt-0.5 text-xs tabular-nums'>
            {formatTimestampToDate(execution.finished_at)} ·{' '}
            {execution.trigger === 'manual' ? '手动触发' : '周期触发'}
          </div>
        </div>
        <Badge variant={resultVariant(execution.result)}>
          {RESULT_LABEL[execution.result]}
        </Badge>
      </div>

      <dl className='mt-3 grid grid-cols-3 gap-3'>
        <div>
          <dt className='text-muted-foreground text-xs'>首字</dt>
          <dd className='mt-0.5 font-mono font-medium tabular-nums'>
            {formatMetric(execution.first_token_ms, 'ms')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>TPS</dt>
          <dd className='mt-0.5 font-mono font-medium tabular-nums'>
            {formatMetric(execution.tps, 'tps')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>总响应</dt>
          <dd className='mt-0.5 font-mono font-medium tabular-nums'>
            {formatMetric(execution.response_time_ms, 'ms')}
          </dd>
        </div>
      </dl>

      <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
        <span>端点 {execution.endpoint || '-'}</span>
        <span>{execution.stream ? '流式' : '非流式'}</span>
        {execution.usage_available ? (
          <span>
            Token 输入 {execution.input_tokens} / 输出 {execution.output_tokens}{' '}
            / 总计 {execution.total_tokens}
          </span>
        ) : (
          <span>Usage 不可用</span>
        )}
      </div>
      {execution.usage_available &&
        (execution.cached_tokens > 0 ||
          execution.cache_write_tokens > 0 ||
          execution.reasoning_tokens > 0) && (
          <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 text-xs'>
            <span>缓存读取 {execution.cached_tokens}</span>
            <span>缓存写入 {execution.cache_write_tokens}</span>
            <span>推理 {execution.reasoning_tokens}</span>
          </div>
        )}
      <div className='text-muted-foreground mt-2 text-xs'>
        样本：{execution.sample_message || execution.sample_status}
      </div>
      {execution.error_message && (
        <details className='mt-2 text-xs'>
          <summary className='text-destructive cursor-pointer font-medium'>
            查看错误摘要
            {execution.error_code ? ` · ${execution.error_code}` : ''}
          </summary>
          <p className='text-muted-foreground mt-1 break-words whitespace-pre-wrap'>
            {execution.error_message}
          </p>
        </details>
      )}
    </article>
  )
}

export function ChannelStatusProbeHistorySheet(
  props: ChannelStatusProbeHistorySheetProps
) {
  const isMobile = useIsMobile()
  const [page, setPage] = useState(1)
  const [modelName, setModelName] = useState('')
  const [result, setResult] = useState<'' | ChannelStatusProbeResult>('')
  const [trigger, setTrigger] = useState<'' | ChannelStatusProbeTrigger>('')
  const pageSize = 20
  const probeActive = isChannelStatusProbeActive(props.channel)
  const query = useQuery({
    queryKey: [
      'channel-monitor',
      'status-probe',
      'executions',
      props.channel.id,
      getChannelStatusProbeHistoryLatestExecutionKey(
        page,
        props.channel.latest?.execution_id ?? 0
      ),
      { page, pageSize, modelName, result, trigger },
    ],
    queryFn: () =>
      getChannelStatusProbeExecutions({
        channelId: props.channel.id,
        page,
        pageSize,
        model: modelName,
        result,
        trigger,
      }),
    placeholderData: keepPreviousData,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: () => probeActive,
    refetchInterval: () => getChannelMonitorActiveRefetchInterval(probeActive),
  })
  const total = query.data?.data.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const modelOptions = useMemo<Array<{ value: string | null; label: string }>>(
    () => [
      { value: null, label: '全部模型' },
      ...(props.channel.config?.models ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    ],
    [props.channel.config?.models]
  )
  let executionContent: ReactNode
  if (query.isLoading) {
    executionContent = (
      <div className='flex flex-col gap-4 p-4'>
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className='h-32 w-full' />
        ))}
      </div>
    )
  } else if (query.isError) {
    executionContent = (
      <Empty className='min-h-64'>
        <EmptyHeader>
          <EmptyTitle>执行记录加载失败</EmptyTitle>
          <EmptyDescription>请检查服务状态后重试</EmptyDescription>
        </EmptyHeader>
        <Button type='button' variant='outline' onClick={() => query.refetch()}>
          <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
          重试
        </Button>
      </Empty>
    )
  } else if (query.data?.data.items.length) {
    executionContent = query.data.data.items.map((execution) => (
      <ChannelStatusProbeExecutionRow
        key={execution.id}
        execution={execution}
      />
    ))
  } else {
    executionContent = (
      <Empty className='min-h-64'>
        <EmptyHeader>
          <EmptyTitle>暂无执行记录</EmptyTitle>
          <EmptyDescription>保存配置后可立即检测一次</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  const content = (
    <div className='flex min-h-0 flex-1 flex-col'>
      <div className='flex flex-wrap items-center gap-2 border-y px-4 py-3'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onOpenConfig}
        >
          <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
          配置
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.actionPending || probeActive || !props.channel.config}
          onClick={props.onRun}
        >
          <HugeiconsIcon
            icon={Refresh01Icon}
            data-icon='inline-start'
            className={props.channel.running ? 'animate-spin' : undefined}
          />
          立即检测
        </Button>
        <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
          {props.channel.config
            ? `${props.channel.config.models.length} 个模型 · 每 ${props.channel.config.interval_seconds} 秒`
            : '尚未配置'}
        </span>
      </div>

      <div
        className='flex flex-col gap-2 border-b p-4 sm:flex-row sm:flex-wrap sm:justify-end'
        data-slot='status-probe-history-filters'
      >
        <Select
          items={modelOptions}
          value={modelName || null}
          onValueChange={(value) => {
            setModelName(value ?? '')
            setPage(1)
          }}
        >
          <SelectTrigger
            className='w-full sm:w-48'
            aria-label='按模型筛选执行记录'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {modelOptions.map((option) => (
                <SelectItem key={option.value ?? 'all'} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Select
          items={RESULT_OPTIONS}
          value={result || null}
          onValueChange={(value) => {
            setResult(value ?? '')
            setPage(1)
          }}
        >
          <SelectTrigger
            className='w-full sm:w-40'
            aria-label='按结果筛选执行记录'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {RESULT_OPTIONS.map((option) => (
                <SelectItem key={option.value ?? 'all'} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Select
          items={TRIGGER_OPTIONS}
          value={trigger || null}
          onValueChange={(value) => {
            setTrigger(value ?? '')
            setPage(1)
          }}
        >
          <SelectTrigger
            className='w-full sm:w-40'
            aria-label='按触发方式筛选执行记录'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {TRIGGER_OPTIONS.map((option) => (
                <SelectItem key={option.value ?? 'all'} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Button
          type='button'
          variant='outline'
          size='icon'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
          aria-label='刷新状态探测记录'
        >
          <HugeiconsIcon
            icon={Refresh01Icon}
            className={query.isFetching ? 'animate-spin' : undefined}
          />
        </Button>
      </div>

      <div className='min-h-0 flex-1 overflow-y-auto'>
        {probeActive && (
          <div className='flex items-center gap-2 border-b px-4 py-3 text-sm'>
            <Spinner />
            <span>
              {props.channel.running ? '本轮检测正在执行' : '立即检测正在排队'}
            </span>
          </div>
        )}
        {executionContent}
      </div>

      <div className='flex shrink-0 items-center justify-between gap-3 border-t px-4 py-3'>
        <span className='text-muted-foreground text-xs tabular-nums'>
          共 {total} 条 · 第 {page}/{totalPages} 页
        </span>
        <div className='flex items-center gap-1'>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
            aria-label='上一页'
          >
            <HugeiconsIcon icon={ArrowLeft01Icon} />
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            disabled={page >= totalPages || query.isFetching}
            onClick={() => setPage((current) => current + 1)}
            aria-label='下一页'
          >
            <HugeiconsIcon icon={ArrowRight01Icon} />
          </Button>
        </div>
      </div>
    </div>
  )

  if (isMobile) {
    return (
      <Drawer open={props.open} onOpenChange={props.onOpenChange}>
        <DrawerContent className='max-h-[96dvh]'>
          <DrawerHeader className='text-left'>
            <DrawerTitle>状态探测记录</DrawerTitle>
            <DrawerDescription>
              {props.channel.name} · ID {props.channel.id}
            </DrawerDescription>
          </DrawerHeader>
          {content}
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>状态探测记录</SheetTitle>
          <SheetDescription>
            {props.channel.name} · ID {props.channel.id}
          </SheetDescription>
        </SheetHeader>
        {content}
      </SheetContent>
    </Sheet>
  )
}
