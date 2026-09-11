import {
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { useChannelMonitorAnalytics } from '../hooks/use-channel-monitor-analytics'
import type { ChannelMonitorAnalyticsExpansionContext } from '../lib/analytics-expansion'
import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import { isChannelMonitorAnalyticsCoverageIncomplete } from '../lib/coverage'
import { formatChannelMonitorCost } from '../lib/format'
import type { ChannelMonitorSuccessMode } from '../types'
import type {
  ChannelMonitorAnalyticsChannel,
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsMetric,
  ChannelMonitorAnalyticsQuery,
  ChannelMonitorAnalyticsSort,
  ChannelMonitorAnalyticsSummary,
} from '../types-analytics'
import { ChannelMonitorAnalyticsCoverage } from './channel-monitor-analytics-coverage'
import { ChannelMonitorAnalyticsFailures } from './channel-monitor-analytics-failures'
import {
  ChannelMonitorAnalyticsPerformanceMeasurement,
  ChannelMonitorAnalyticsPerformanceOutput,
} from './channel-monitor-analytics-performance'
import { ChannelMonitorAnalyticsExpandableTable } from './channel-monitor-analytics-table'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'

type ChannelMonitorAnalyticsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  metric: ChannelMonitorAnalyticsMetric
  channels: readonly {
    id: number
    name: string
    remark?: string | null
    channel_remark?: string | null
  }[]
  initialChannelId?: number
  initialModel?: string
  initialGroup?: string
  rangeMinutes?: number
  successMode?: ChannelMonitorSuccessMode
}

type AnalyticsTab = 'channels' | 'api_keys'

function getDefaultAnalyticsSort(
  metric: ChannelMonitorAnalyticsMetric
): ChannelMonitorAnalyticsSort {
  return metric === 'cost' ? 'cost' : 'samples'
}

function addBeijingDays(date: string, days: number) {
  const value = new Date(`${date}T00:00:00+08:00`)
  value.setUTCDate(value.getUTCDate() + days)
  return formatChannelMonitorBeijingDate(value)
}

function getDateRange(from: string, through: string) {
  return {
    from,
    to: addBeijingDays(through, 1),
    today: formatChannelMonitorBeijingDate(new Date()),
  }
}

function getPresetDateRange(days: 1 | 7 | 30 | 90) {
  const today = formatChannelMonitorBeijingDate(new Date())
  return {
    from: addBeijingDays(today, -days + 1),
    through: today,
  }
}

function formatDateRangeLabel(from: string, through: string, today: string) {
  if (from === today && through === today) return '当日'
  if (from === through) return from
  return `${from} ~ ${through}`
}

function ChannelMonitorAnalyticsDateRangeControl(props: {
  from: string
  through: string
  today: string
  onChange: (range: { from: string; through: string }) => void
}) {
  const [open, setOpen] = useState(false)
  const [draftFrom, setDraftFrom] = useState(props.from)
  const [draftThrough, setDraftThrough] = useState(props.through)
  const label = formatDateRangeLabel(props.from, props.through, props.today)

  const openDraft = (nextOpen: boolean) => {
    if (nextOpen) {
      setDraftFrom(props.from)
      setDraftThrough(props.through)
    }
    setOpen(nextOpen)
  }

  const applyRange = (range: { from: string; through: string }) => {
    if (!range.from || !range.through || range.from > range.through) return
    if (range.through > props.today) return
    const span =
      Math.floor(
        (new Date(`${range.through}T00:00:00+08:00`).getTime() -
          new Date(`${range.from}T00:00:00+08:00`).getTime()) /
          86_400_000
      ) + 1
    if (span > 90) return
    props.onChange(range)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={openDraft}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='w-full justify-start font-normal tabular-nums sm:w-auto'
            aria-label='统计日期范围'
          />
        }
      >
        {label}
      </PopoverTrigger>
      <PopoverContent
        align='end'
        className='w-[min(26rem,calc(100vw-2rem))] p-3'
      >
        <div className='flex flex-col gap-3'>
          <div className='text-muted-foreground text-xs'>
            选择单日或日期范围（最多 90 天）
          </div>
          <div className='grid gap-2 sm:grid-cols-2'>
            <label className='flex flex-col gap-1 text-xs'>
              开始日期
              <Input
                type='date'
                value={draftFrom}
                max={props.today}
                onChange={(event) => setDraftFrom(event.target.value)}
              />
            </label>
            <label className='flex flex-col gap-1 text-xs'>
              结束日期
              <Input
                type='date'
                value={draftThrough}
                max={props.today}
                onChange={(event) => setDraftThrough(event.target.value)}
              />
            </label>
          </div>
          <div className='flex flex-wrap gap-1.5'>
            {[1, 7, 30, 90].map((days) => (
              <Button
                key={days}
                type='button'
                variant='secondary'
                size='sm'
                className='h-7 flex-1 px-2 text-xs'
                onClick={() => {
                  const range = getPresetDateRange(days as 1 | 7 | 30 | 90)
                  setDraftFrom(range.from)
                  setDraftThrough(range.through)
                  applyRange(range)
                }}
              >
                {days === 1 ? '当日' : `近 ${days} 天`}
              </Button>
            ))}
          </div>
          <div className='flex justify-end'>
            <Button
              type='button'
              size='sm'
              onClick={() =>
                applyRange({ from: draftFrom, through: draftThrough })
              }
            >
              应用
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function AnalyticsSummary(props: {
  metric: ChannelMonitorAnalyticsMetric
  summary: ChannelMonitorAnalyticsSummary | undefined
  successMode?: ChannelMonitorSuccessMode
}) {
  const summary = props.summary
  if (!summary) return null
  const values: Array<[string, ReactNode]> = []
  if (props.metric === 'performance') {
    values.push(
      ['上游尝试数', summary.actual_sample_count],
      [
        '平均首字',
        <ChannelMonitorAnalyticsPerformanceMeasurement
          key='first-token'
          metric='first_token'
          summary={summary}
        />,
      ],
      [
        '平均 TPS',
        <ChannelMonitorAnalyticsPerformanceMeasurement
          key='tps'
          metric='tps'
          summary={summary}
        />,
      ],
      [
        '测速输出',
        <ChannelMonitorAnalyticsPerformanceOutput
          key='output'
          summary={summary}
        />,
      ]
    )
  } else if (props.metric === 'success') {
    const final = props.successMode === 'final'
    const sampleCount = final
      ? summary.final_sample_count
      : summary.actual_sample_count
    values.push(
      [final ? '最终请求数' : '上游尝试数', sampleCount],
      [
        final ? '最终成功率' : '上游成功率',
        formatRate(
          final ? summary.final_success_rate : summary.actual_success_rate,
          sampleCount
        ),
      ],
      [
        '流式缓存利用率',
        formatRate(summary.cache_utilization_rate, summary.input_tokens),
      ],
      ['缓存写入次数', summary.cache_write_request_count]
    )
  } else {
    const settled = summary.settled_count ?? 0
    const unresolved = summary.unresolved_count ?? 0
    values.push(
      [
        '成本',
        formatChannelMonitorCost((summary.cost_nano_cny ?? 0) / 1_000_000_000),
      ],
      ['已结算', settled],
      ['未解析', unresolved],
      [
        '解析率',
        formatRate(
          settled / Math.max(settled + unresolved, 1),
          settled + unresolved
        ),
      ]
    )
  }
  return (
    <div className='bg-border grid shrink-0 grid-cols-2 gap-px overflow-hidden rounded-lg border sm:grid-cols-4'>
      {values.map(([label, value]) => (
        <div
          key={String(label)}
          className='bg-background flex min-h-16 flex-col justify-center gap-1 px-3 py-2'
        >
          <span className='text-muted-foreground text-xs'>{label}</span>
          <span className='font-mono text-base font-semibold tabular-nums'>
            {value}
          </span>
        </div>
      ))}
    </div>
  )
}

function formatRate(value: number, denominator: number) {
  if (denominator <= 0 || !Number.isFinite(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

export function ChannelMonitorAnalyticsDialog(
  props: ChannelMonitorAnalyticsDialogProps
) {
  const [tab, setTab] = useState<AnalyticsTab>('channels')
  const initialDate = formatChannelMonitorBeijingDate(new Date())
  const [dateFrom, setDateFrom] = useState(initialDate)
  const [dateThrough, setDateThrough] = useState(initialDate)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [sort, setSort] = useState<ChannelMonitorAnalyticsSort>(() =>
    getDefaultAnalyticsSort(props.metric)
  )
  const [direction, setDirection] = useState<'asc' | 'desc'>('desc')
  const channels = useMemo(
    () =>
      new Map<number, ChannelMonitorAnalyticsChannel>(
        props.channels.map((channel) => [
          channel.id,
          {
            name: channel.name,
            remark: channel.channel_remark || channel.remark || '',
          },
        ])
      ),
    [props.channels]
  )
  const dateRange = getDateRange(dateFrom, dateThrough)
  let rootGroupBy: ChannelMonitorAnalyticsGroupBy = 'channel'
  if (tab === 'api_keys') {
    rootGroupBy = 'user'
  }
  const rootRequest: ChannelMonitorAnalyticsQuery = {
    metric: props.metric,
    groupBy: rootGroupBy,
    from: props.rangeMinutes == null ? dateRange.from : undefined,
    to: props.rangeMinutes == null ? dateRange.to : undefined,
    minutes: props.rangeMinutes,
    group: props.initialGroup,
    successMode: props.successMode,
    channelId: props.initialChannelId,
    model: props.initialModel,
    search: search || undefined,
    sort,
    direction,
    page,
    pageSize: 20,
  }
  const rootQuery = useChannelMonitorAnalytics(rootRequest, props.open)
  const rootQueryResponse = rootQuery.data?.data
  const response =
    rootQueryResponse?.group_by === rootGroupBy ? rootQueryResponse : undefined
  const coverage = response?.coverage
  const coverageIncomplete =
    isChannelMonitorAnalyticsCoverageIncomplete(coverage)
  const pageCount = response
    ? Math.max(1, Math.ceil(response.total / response.page_size))
    : 1
  const expansionContext: ChannelMonitorAnalyticsExpansionContext = {
    tab,
    metric: props.metric,
    from: rootRequest.from,
    to: rootRequest.to,
    minutes: props.rangeMinutes,
    group: props.initialGroup,
    successMode: props.successMode,
    channelId: props.initialChannelId,
    model: props.initialModel,
    search: search || undefined,
    sort,
    direction,
  }

  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  useEffect(() => {
    setPage(1)
  }, [rootGroupBy, dateFrom, dateThrough, search, sort, direction])

  useEffect(() => {
    setSort(getDefaultAnalyticsSort(props.metric))
    setDirection('desc')
    const today = formatChannelMonitorBeijingDate(new Date())
    setDateFrom(today)
    setDateThrough(today)
  }, [props.metric, props.open])

  useEffect(() => {
    if (!props.open) {
      setTab('channels')
      const today = formatChannelMonitorBeijingDate(new Date())
      setDateFrom(today)
      setDateThrough(today)
      setSearchInput('')
      setSearch('')
      setPage(1)
    }
  }, [props.open])

  const handleTabChange = (nextTab: AnalyticsTab) => {
    setTab(nextTab)
    setPage(1)
    setSearchInput('')
    setSearch('')
  }

  const handleSort = (nextSort: ChannelMonitorAnalyticsSort) => {
    if (sort === nextSort) {
      setDirection((value) => (value === 'desc' ? 'asc' : 'desc'))
      return
    }
    setSort(nextSort)
    setDirection('desc')
  }

  const scopeLabels: string[] = []
  if (props.rangeMinutes != null) {
    scopeLabels.push(`近${props.rangeMinutes}分钟`)
  }
  if (props.initialGroup) scopeLabels.push(`分组 ${props.initialGroup}`)
  if (props.initialChannelId != null) {
    scopeLabels.push(
      channels.get(props.initialChannelId)?.name ??
        `渠道 #${props.initialChannelId}`
    )
  }
  if (props.initialModel) scopeLabels.push(`模型 ${props.initialModel}`)
  if (props.rangeMinutes != null && props.metric === 'success') {
    scopeLabels.push(
      props.successMode === 'final' ? '最终结果口径' : '上游尝试口径'
    )
  }
  let sourceLabel = '历史日汇总'
  if (response?.source === 'redis_daily') sourceLabel = '今日实时汇总'
  if (response?.source === 'redis_minutes') sourceLabel = '分钟实时汇总'
  let title = '渠道成本分析'
  let description =
    '成本为已结算的渠道成本，包含业务、探测和模型检测。未解析记录的金额尚不能确定；未归属用户可能包含系统探测和历史记录。'
  if (props.metric === 'performance') {
    title = '性能分析'
    description =
      '首字延迟越低越快，TPS 越高越快。首字按有效样本平均；TPS = 总输出 Token ÷ 总生成时间。仅统计业务上游调用，重试分别计数；缺失指标的请求不计入对应平均值。'
  } else if (props.metric === 'success') {
    title = '成功率与缓存分析'
    description = `${props.successMode === 'final' ? '成功率按请求最终结果统计。' : '成功率按实际派发的上游尝试统计，包含重试。'}缓存利用率按流式请求的输入 Token 加权；缓存写入次数包含流式和非流式请求。`
  }

  let table: ReactNode
  if (rootQuery.isLoading || (rootQuery.isFetching && !response)) {
    table = <Skeleton className='h-72 w-full' />
  } else if (rootQuery.isError && !response) {
    table = (
      <Alert variant='destructive'>
        <AlertTitle>统计加载失败</AlertTitle>
        <AlertDescription className='flex items-center justify-between gap-3'>
          <span>{rootQuery.error?.message || '请稍后重试'}</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void rootQuery.refetch()}
          >
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重试
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else if (coverage?.status === 'unavailable') {
    table = null
  } else {
    table = (
      <ChannelMonitorAnalyticsExpandableTable
        key={JSON.stringify(rootRequest)}
        metric={props.metric}
        groupBy={rootGroupBy}
        items={response?.items ?? []}
        channels={channels}
        context={expansionContext}
        onSort={handleSort}
      />
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'flex flex-col sm:max-w-6xl'
        )}
      >
        <DialogHeader className='shrink-0 pr-10'>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className='[overflow-wrap:anywhere] break-words'>
            {scopeLabels.length > 0
              ? scopeLabels.join(' · ')
              : '按北京时间查看全部用户及 API Key 的汇总'}
          </DialogDescription>
        </DialogHeader>
        <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pr-1'>
          <div className='flex flex-col gap-2 border-b pb-3 sm:flex-row sm:items-center sm:justify-between'>
            <div className='flex flex-wrap items-center gap-2'>
              <Button
                type='button'
                variant={tab === 'channels' ? 'secondary' : 'outline'}
                size='sm'
                onClick={() => handleTabChange('channels')}
              >
                渠道汇总
              </Button>
              <Button
                type='button'
                variant={tab === 'api_keys' ? 'secondary' : 'outline'}
                size='sm'
                onClick={() => handleTabChange('api_keys')}
              >
                API Key 明细
              </Button>
              {props.initialChannelId != null ? (
                <span className='text-muted-foreground text-xs'>
                  当前渠道内
                </span>
              ) : null}
            </div>
            {props.rangeMinutes != null ? (
              <span
                aria-label='统计时间范围'
                className='text-muted-foreground shrink-0 text-sm tabular-nums'
              >
                近{props.rangeMinutes}分钟
              </span>
            ) : (
              <ChannelMonitorAnalyticsDateRangeControl
                from={dateFrom}
                through={dateThrough}
                today={dateRange.today}
                onChange={(range) => {
                  setDateFrom(range.from)
                  setDateThrough(range.through)
                  setPage(1)
                }}
              />
            )}
          </div>
          <AnalyticsSummary
            metric={props.metric}
            successMode={props.successMode}
            summary={
              coverage?.status === 'unavailable'
                ? undefined
                : (response?.scope_summary ?? response?.summary)
            }
          />
          <p className='text-muted-foreground text-xs'>{description}</p>
          {rootQuery.isError && response ? (
            <Alert variant='destructive'>
              <AlertTitle>统计更新失败，保留上次结果</AlertTitle>
              <AlertDescription>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void rootQuery.refetch()}
                >
                  重试
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder='搜索用户、API Key、模型、渠道名称或 ID'
              aria-label='搜索分析明细'
              className='sm:max-w-xs'
            />
            <div className='text-muted-foreground flex items-center gap-2 text-xs'>
              <span>
                {sourceLabel}
                {response?.source === 'redis_and_database_daily'
                  ? ' · 含今日实时数据'
                  : null}
              </span>
              {coverageIncomplete ? (
                <span className='text-warning'>覆盖不完整</span>
              ) : null}
              {coverage?.status !== 'unavailable' ? (
                <span>共 {response?.total ?? 0} 条</span>
              ) : null}
            </div>
          </div>
          {response?.processed_at ? (
            <span className='text-muted-foreground text-xs'>
              数据更新于{' '}
              {new Date(response.processed_at * 1000).toLocaleString('zh-CN', {
                timeZone: 'Asia/Shanghai',
                hour12: false,
              })}
              （北京时间）
            </span>
          ) : null}
          <ChannelMonitorAnalyticsCoverage coverage={coverage} />
          <div
            className={cn(
              'min-h-0 shrink-0',
              rootQuery.isFetching && 'opacity-70 transition-opacity'
            )}
            aria-busy={rootQuery.isFetching}
          >
            {table}
          </div>
          <div className='flex items-center justify-between gap-3 border-t pt-3'>
            <span className='text-muted-foreground text-xs'>
              第 {response?.page ?? page} / {pageCount} 页
            </span>
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1 || rootQuery.isFetching}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
                aria-label='上一页'
                title='上一页'
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= pageCount || rootQuery.isFetching}
                onClick={() =>
                  setPage((value) => Math.min(pageCount, value + 1))
                }
                aria-label='下一页'
                title='下一页'
              >
                <HugeiconsIcon icon={ArrowRight01Icon} />
              </Button>
            </div>
          </div>
          {props.metric === 'success' &&
          props.rangeMinutes != null &&
          props.initialChannelId != null &&
          !search &&
          response &&
          coverage?.status !== 'unavailable' ? (
            <ChannelMonitorAnalyticsFailures
              categories={response.failure_categories ?? []}
              mode={props.successMode ?? 'actual'}
              failureCount={
                props.successMode === 'final'
                  ? response.scope_summary.final_failure_count
                  : response.scope_summary.actual_failure_count
              }
              truncated={response.failure_categories_truncated}
            />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
