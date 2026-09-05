import {
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Refresh01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { useChannelMonitorAnalytics } from '../hooks/use-channel-monitor-analytics'
import { shouldHandleChannelMonitorAnalyticsBackspace } from '../lib/analytics-navigation'
import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import { isChannelMonitorAnalyticsCoverageIncomplete } from '../lib/coverage'
import { formatChannelMonitorCost } from '../lib/format'
import type {
  ChannelMonitorAnalyticsChannel,
  ChannelMonitorAnalyticsGroupBy,
  ChannelMonitorAnalyticsItem,
  ChannelMonitorAnalyticsMetric,
  ChannelMonitorAnalyticsQuery,
  ChannelMonitorAnalyticsSummary,
} from '../types-analytics'
import { ChannelMonitorAnalyticsTable } from './channel-monitor-analytics-table'
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
}

type AnalyticsTab = 'channels' | 'api_keys'
type RangeDays = 1 | 7 | 30 | 90
type AnalyticsSelection = Pick<ChannelMonitorAnalyticsItem, 'key'> &
  Partial<Omit<ChannelMonitorAnalyticsItem, 'key'>>

const RANGE_OPTIONS: Array<{ value: string; label: string }> = [
  { value: '1', label: '今日' },
  { value: '7', label: '近 7 天' },
  { value: '30', label: '近 30 天' },
  { value: '90', label: '近 90 天' },
]

function getDateRange(days: RangeDays) {
  const today = formatChannelMonitorBeijingDate(new Date())
  const from = new Date(`${today}T00:00:00+08:00`)
  from.setUTCDate(from.getUTCDate() - days + 1)
  const fromDate = formatChannelMonitorBeijingDate(from)
  const to = new Date(`${today}T00:00:00+08:00`)
  to.setUTCDate(to.getUTCDate() + 1)
  return { from: fromDate, to: formatChannelMonitorBeijingDate(to), today }
}

function getGroupBy(
  tab: AnalyticsTab,
  channel: AnalyticsSelection | null,
  user: AnalyticsSelection | null,
  apiKey: AnalyticsSelection | null
): ChannelMonitorAnalyticsGroupBy {
  if (tab === 'api_keys') {
    return apiKey ? 'api_key_channel_model' : 'api_key'
  }
  if (apiKey) return 'api_key_channel_model'
  if (user) return 'api_key'
  if (channel) return 'user'
  return 'channel'
}

function AnalyticsSummary(props: {
  metric: ChannelMonitorAnalyticsMetric
  summary: ChannelMonitorAnalyticsSummary | undefined
}) {
  const summary = props.summary
  if (!summary) return null
  const values: Array<[string, string | number]> = []
  if (props.metric === 'success') {
    values.push(
      ['调用数', summary.actual_sample_count],
      [
        '成功率',
        formatRate(summary.actual_success_rate, summary.actual_sample_count),
      ],
      [
        '缓存利用率',
        formatRate(summary.cache_utilization_rate, summary.input_tokens),
      ],
      ['缓存写入', summary.cache_write_request_count]
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

function selectionLabel(
  tab: AnalyticsTab,
  channel: AnalyticsSelection | null,
  user: AnalyticsSelection | null,
  apiKey: AnalyticsSelection | null,
  channels: ReadonlyMap<number, ChannelMonitorAnalyticsChannel>
) {
  const labels: string[] = []
  if (tab === 'channels' && channel) {
    const channelInfo = channels.get(channel.channel_id ?? 0)
    labels.push(channelInfo?.name ?? `渠道 #${channel.channel_id}`)
  }
  if (user) {
    const userLabel = user.user_display_name || user.user_name
    const userID =
      user.user_id && user.user_id > 0 ? `用户 #${user.user_id}` : ''
    labels.push(userLabel || userID || '未归属用户')
  }
  if (apiKey) {
    labels.push(apiKey.api_key_name || `API Key #${apiKey.api_key_id ?? 0}`)
  }
  return labels.join(' > ')
}

export function ChannelMonitorAnalyticsDialog(
  props: ChannelMonitorAnalyticsDialogProps
) {
  const [tab, setTab] = useState<AnalyticsTab>('channels')
  const [rangeDays, setRangeDays] = useState<RangeDays>(1)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [selectedChannel, setSelectedChannel] =
    useState<AnalyticsSelection | null>(() =>
      props.initialChannelId
        ? {
            key: String(props.initialChannelId),
            channel_id: props.initialChannelId,
          }
        : null
    )
  const [selectedUser, setSelectedUser] = useState<AnalyticsSelection | null>(
    null
  )
  const [selectedAPIKey, setSelectedAPIKey] =
    useState<AnalyticsSelection | null>(null)
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
  const dateRange = getDateRange(rangeDays)
  const groupBy = getGroupBy(tab, selectedChannel, selectedUser, selectedAPIKey)
  const request: ChannelMonitorAnalyticsQuery = {
    metric: props.metric,
    groupBy,
    from: dateRange.from,
    to: dateRange.to,
    channelId: selectedChannel?.channel_id,
    userId: selectedUser?.user_id,
    apiKeyId: selectedAPIKey?.api_key_id,
    search: search || undefined,
    sort: props.metric === 'success' ? 'samples' : undefined,
    direction: 'desc',
    page,
    pageSize: 20,
  }
  const query = useChannelMonitorAnalytics(request, props.open)
  const queryResponse = query.data?.data
  const response =
    !query.isFetching && queryResponse?.group_by === groupBy
      ? queryResponse
      : undefined
  const coverage = response?.coverage
  const coverageIncomplete =
    isChannelMonitorAnalyticsCoverageIncomplete(coverage)
  const pageCount = response
    ? Math.max(1, Math.ceil(response.total / response.page_size))
    : 1
  const breadcrumb = selectionLabel(
    tab,
    selectedChannel,
    selectedUser,
    selectedAPIKey,
    channels
  )

  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  useEffect(() => {
    setPage(1)
  }, [groupBy, rangeDays, search])

  useEffect(() => {
    if (props.open && props.initialChannelId) {
      setSelectedChannel({
        key: String(props.initialChannelId),
        channel_id: props.initialChannelId,
      })
      setSelectedUser(null)
      setSelectedAPIKey(null)
      setPage(1)
    }
  }, [props.initialChannelId, props.open])

  useEffect(() => {
    if (!props.open) {
      setTab('channels')
      setRangeDays(1)
      setSearchInput('')
      setSearch('')
      setPage(1)
      setSelectedChannel(null)
      setSelectedUser(null)
      setSelectedAPIKey(null)
    }
  }, [props.open])

  const handleTabChange = (nextTab: AnalyticsTab) => {
    setTab(nextTab)
    setPage(1)
    setSelectedChannel(null)
    setSelectedUser(null)
    setSelectedAPIKey(null)
    setSearchInput('')
    setSearch('')
  }

  const handleSelect = (item: ChannelMonitorAnalyticsItem) => {
    if (tab === 'api_keys') {
      if (!selectedAPIKey) {
        setSelectedAPIKey(item)
        setPage(1)
      }
      return
    }
    if (!selectedChannel) {
      setSelectedChannel(item)
    } else if (!selectedUser) {
      setSelectedUser(item)
    } else if (!selectedAPIKey) {
      setSelectedAPIKey(item)
    }
    setPage(1)
  }

  const goBack = useCallback(() => {
    if (selectedAPIKey) setSelectedAPIKey(null)
    else if (selectedUser) setSelectedUser(null)
    else if (selectedChannel) setSelectedChannel(null)
    setPage(1)
  }, [selectedAPIKey, selectedChannel, selectedUser])

  useEffect(() => {
    if (!props.open) return

    const handleKeyDown = (event: KeyboardEvent) => {
      const hasSelection =
        selectedChannel != null ||
        selectedUser != null ||
        selectedAPIKey != null
      if (!shouldHandleChannelMonitorAnalyticsBackspace(event, hasSelection)) {
        return
      }
      event.preventDefault()
      goBack()
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [goBack, props.open, selectedAPIKey, selectedChannel, selectedUser])

  let table: ReactNode
  if (query.isLoading || (query.isFetching && !response)) {
    table = <Skeleton className='h-72 w-full' />
  } else if (query.isError) {
    table = (
      <Alert variant='destructive'>
        <AlertTitle>统计加载失败</AlertTitle>
        <AlertDescription className='flex items-center justify-between gap-3'>
          <span>请稍后重试</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void query.refetch()}
          >
            <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            重试
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else {
    table = (
      <ChannelMonitorAnalyticsTable
        metric={props.metric}
        groupBy={groupBy}
        items={response?.items ?? []}
        channels={channels}
        onSelect={
          !selectedAPIKey &&
          (tab === 'api_keys' ||
            selectedChannel == null ||
            selectedUser == null ||
            groupBy === 'api_key')
            ? handleSelect
            : undefined
        }
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
          <div className='flex flex-wrap items-center gap-2'>
            {selectedChannel || selectedUser || selectedAPIKey ? (
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={goBack}
                aria-label='返回上一级'
                title='返回上一级'
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} />
              </Button>
            ) : null}
            <DialogTitle>
              {props.metric === 'success' ? '成功率与缓存分析' : '渠道成本分析'}
            </DialogTitle>
          </div>
          <DialogDescription>
            {breadcrumb || '按北京时间查看当前范围汇总；明细按当前层分页读取'}
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
              {selectedChannel ? (
                <span className='text-muted-foreground text-xs'>
                  当前渠道内
                </span>
              ) : null}
            </div>
            <Select
              items={RANGE_OPTIONS}
              value={String(rangeDays)}
              onValueChange={(value) => {
                if (
                  value === '1' ||
                  value === '7' ||
                  value === '30' ||
                  value === '90'
                ) {
                  setRangeDays(Number(value) as RangeDays)
                }
              }}
            >
              <SelectTrigger
                size='sm'
                className='w-full sm:w-28'
                aria-label='统计时间范围'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {RANGE_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <AnalyticsSummary
            metric={props.metric}
            summary={response?.scope_summary ?? response?.summary}
          />
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder={
                tab === 'channels' && !selectedChannel
                  ? '搜索渠道、用户或 Key'
                  : '搜索名称或 ID'
              }
              aria-label='搜索分析明细'
              className='sm:max-w-xs'
            />
            <div className='text-muted-foreground flex items-center gap-2 text-xs'>
              <span>
                {response?.source === 'redis_daily'
                  ? '实时 Redis 日汇总'
                  : '数据库日汇总'}
              </span>
              {coverageIncomplete ? (
                <span className='text-warning'>覆盖不完整</span>
              ) : null}
              <span>共 {response?.total ?? 0} 条</span>
            </div>
          </div>
          {coverageIncomplete ? (
            <Alert>
              <AlertTitle>统计覆盖不完整</AlertTitle>
              <AlertDescription>
                {coverage?.reasons.join('、') || '数据仍在异步处理'}
              </AlertDescription>
            </Alert>
          ) : null}
          <div
            className={cn(
              'min-h-0',
              query.isFetching && 'opacity-70 transition-opacity'
            )}
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
                disabled={page <= 1 || query.isFetching}
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
                disabled={page >= pageCount || query.isFetching}
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
        </div>
      </DialogContent>
    </Dialog>
  )
}
