import { useQuery } from '@tanstack/react-query'
import type { OnChangeFn, PaginationState } from '@tanstack/react-table'
import { useCallback, useState } from 'react'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatNumber, formatPercent } from '@/lib/format'

import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import {
  getRequestStats,
  getRequestStatsSettings,
  requestStatsQueryKey,
  type RequestStatsQuery,
  type RequestStatsRow,
} from '../lib/request-statistics'
import { ContentCapturePanel } from './content-capture-panel'
import { RequestStatisticsHistory } from './request-statistics-history'
import { RequestStatisticsSettings } from './request-statistics-settings'
import { RequestStatisticsTable } from './request-statistics-table'

type View = {
  date: string
  search: string
  sort: RequestStatsQuery['sort']
  direction: RequestStatsQuery['direction']
  path: RequestStatsRow[]
  pagination: PaginationState
}

function requestStatsLevel(
  path: RequestStatsRow[]
): RequestStatsQuery['level'] {
  if (path.length === 0) return 'user'
  if (path.length === 1) return 'api_key'
  return 'channel'
}

export default function RequestStatisticsDialog(props: {
  onOpenChange: (open: boolean) => void
}) {
  const today = formatChannelMonitorBeijingDate(new Date())
  const [view, setView] = useState<View>({
    date: today,
    search: '',
    sort: 'low_input_rate',
    direction: 'desc',
    path: [],
    pagination: { pageIndex: 0, pageSize: 20 },
  })
  const [search, setSearch] = useState('')
  const [tab, setTab] = useState('statistics')
  const [history, setHistory] = useState<{
    name: string
    user_id: number
    api_key_id?: number
  } | null>(null)
  const query: RequestStatsQuery = {
    date: view.date,
    search: view.search,
    sort: view.sort,
    direction: view.direction,
    level: requestStatsLevel(view.path),
    user_id: view.path[0]?.id,
    api_key_id: view.path[1]?.id,
    page: view.pagination.pageIndex + 1,
    page_size: view.pagination.pageSize,
  }
  const settings = useQuery({
    queryKey: [...requestStatsQueryKey, 'settings'],
    queryFn: getRequestStatsSettings,
  })
  const stats = useQuery({
    queryKey: [...requestStatsQueryKey, 'rows', query],
    queryFn: ({ signal }) => getRequestStats(query, signal),
    enabled: tab === 'statistics',
    refetchInterval:
      tab === 'statistics' && view.date === today && !history ? 15000 : false,
  })
  const drill = useCallback((row: RequestStatsRow) => {
    setView((value) => ({
      ...value,
      path: [...value.path, row],
      sort:
        value.path.length === 1 &&
        value.sort === 'low_input_auto_disabled_count'
          ? 'low_input_rate'
          : value.sort,
      search: '',
      pagination: { ...value.pagination, pageIndex: 0 },
    }))
    setSearch('')
  }, [])
  const showHistory = useCallback(
    (row: RequestStatsRow) => {
      setHistory({
        name: row.name,
        user_id: row.user_id,
        api_key_id: view.path.length === 1 ? row.api_key_id : undefined,
      })
    },
    [view.path.length]
  )
  const paginate: OnChangeFn<PaginationState> = useCallback(
    (update) =>
      setView((value) => ({
        ...value,
        pagination:
          typeof update === 'function' ? update(value.pagination) : update,
      })),
    []
  )
  const summary = stats.data?.summary
  return (
    <Dialog
      open
      onOpenChange={props.onOpenChange}
      title='用户请求统计'
      description='按北京时间自然日统计实际派发的业务上游尝试，重试分别计数。此数据用于排查疑似蒸馏，不代表已经认定。'
      contentClassName='sm:max-w-7xl'
      contentHeight='min(44rem, calc(100dvh - 12rem))'
      bodyClassName='space-y-4'
    >
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value='statistics'>请求统计</TabsTrigger>
          <TabsTrigger value='settings'>统计与自动禁用设置</TabsTrigger>
          <TabsTrigger value='content-capture'>请求内容抓取</TabsTrigger>
        </TabsList>
        <TabsContent value='statistics' className='space-y-4'>
          <form
            className='flex flex-wrap items-end gap-3'
            onSubmit={(event) => {
              event.preventDefault()
              setView((value) => ({
                ...value,
                search,
                pagination: { ...value.pagination, pageIndex: 0 },
              }))
            }}
          >
            <label className='space-y-1 text-sm'>
              统计日期（北京）
              <Input
                type='date'
                aria-label='统计日期'
                value={view.date}
                max={today}
                required
                onChange={(event) => {
                  const date = event.target.value
                  if (date) {
                    setView((value) => ({
                      ...value,
                      date,
                      pagination: { ...value.pagination, pageIndex: 0 },
                    }))
                  }
                }}
              />
            </label>
            <label className='space-y-1 text-sm'>
              名称或 ID
              <Input
                aria-label='搜索名称或 ID'
                value={search}
                maxLength={255}
                onChange={(event) => setSearch(event.target.value)}
              />
            </label>
            <label className='space-y-1 text-sm'>
              排序
              <NativeSelect
                aria-label='排序字段'
                value={view.sort}
                onChange={(event) =>
                  setView((value) => ({
                    ...value,
                    sort: event.target.value as View['sort'],
                    pagination: { ...value.pagination, pageIndex: 0 },
                  }))
                }
              >
                <option value='low_input_rate'>低输入占比</option>
                <option value='request_count'>请求数</option>
                {query.level !== 'channel' && (
                  <option value='low_input_auto_disabled_count'>
                    累计禁用次数
                  </option>
                )}
              </NativeSelect>
            </label>
            <label className='space-y-1 text-sm'>
              方向
              <NativeSelect
                aria-label='排序方向'
                value={view.direction}
                onChange={(event) =>
                  setView((value) => ({
                    ...value,
                    direction: event.target.value as View['direction'],
                    pagination: { ...value.pagination, pageIndex: 0 },
                  }))
                }
              >
                <option value='desc'>从高到低</option>
                <option value='asc'>从低到高</option>
              </NativeSelect>
            </label>
            <Button type='submit'>搜索</Button>
            <Button
              type='button'
              variant='outline'
              disabled={stats.isFetching}
              onClick={() => void stats.refetch()}
            >
              刷新统计
            </Button>
          </form>
          <nav
            aria-label='统计层级'
            className='flex flex-wrap items-center gap-1'
          >
            <Button
              variant='link'
              onClick={() => {
                setView((value) => ({
                  ...value,
                  path: [],
                  search: '',
                  pagination: { ...value.pagination, pageIndex: 0 },
                }))
                setSearch('')
              }}
              aria-current={!view.path.length ? 'page' : undefined}
            >
              全部用户
            </Button>
            {view.path.map((row, index) => (
              <Button
                key={row.id}
                variant='link'
                className='max-w-80 truncate'
                aria-current={
                  index === view.path.length - 1 ? 'page' : undefined
                }
                onClick={() => {
                  setView((value) => ({
                    ...value,
                    path: value.path.slice(0, index + 1),
                    search: '',
                    pagination: { ...value.pagination, pageIndex: 0 },
                  }))
                  setSearch('')
                }}
              >
                {row.name}（#{row.id}）
              </Button>
            ))}
          </nav>
          {stats.isError && (
            <ErrorState
              title='请求统计加载失败'
              description='统计来源暂时不可用，请重试。'
              onRetry={() => void stats.refetch()}
            />
          )}
          {stats.isPending && <LoadingState message='正在加载请求统计…' />}
          {stats.data && !stats.isPending && !stats.isError && (
            <>
              {stats.data.coverage_partial && (
                <Alert>
                  <AlertDescription>
                    所选日期包含功能启用前或尚未完整恢复的数据。旧记录缺少输入
                    Token 或流式类型时按未知统计，不补为 0。
                  </AlertDescription>
                </Alert>
              )}
              <p className='text-muted-foreground text-sm'>
                当前阈值：严格小于 {stats.data.low_input_threshold_k}
                K。低输入占比 = 低输入请求 /
                有效输入样本；未知输入不参与占比。当前摘要覆盖完整搜索结果。
              </p>
              {summary && (
                <dl className='grid grid-cols-2 gap-3 rounded-lg border p-4 sm:grid-cols-4 lg:grid-cols-8'>
                  {(
                    [
                      ['总请求', formatNumber(summary.request_count)],
                      ['流式', formatNumber(summary.stream_request_count)],
                      [
                        '非流式',
                        formatNumber(summary.non_stream_request_count),
                      ],
                      [
                        '有效输入样本',
                        formatNumber(summary.known_input_request_count),
                      ],
                      [
                        '未知输入',
                        formatNumber(summary.unknown_input_request_count),
                      ],
                      ['低输入请求', formatNumber(summary.low_input_count)],
                      [
                        '低输入占比',
                        summary.low_input_rate === null
                          ? '暂无可判定样本'
                          : formatPercent(summary.low_input_rate),
                      ],
                      [
                        '累计禁用',
                        formatNumber(summary.low_input_auto_disabled_count),
                      ],
                    ] as const
                  ).map(([label, value]) => (
                    <div key={label}>
                      <dt className='text-muted-foreground text-xs'>{label}</dt>
                      <dd className='mt-1 font-medium tabular-nums'>{value}</dd>
                    </div>
                  ))}
                </dl>
              )}
              {summary && summary.unclassified_stream_request_count > 0 && (
                <p className='text-muted-foreground text-sm'>
                  另有 {formatNumber(summary.unclassified_stream_request_count)}{' '}
                  次旧请求无法区分流式类型。
                </p>
              )}
              <RequestStatisticsTable
                rows={stats.data.items}
                total={stats.data.total}
                level={query.level}
                thresholdK={stats.data.low_input_threshold_k}
                pagination={view.pagination}
                onPaginationChange={paginate}
                onDrill={drill}
                onHistory={showHistory}
                loading={stats.isFetching && !stats.data}
              />
              <p className='text-muted-foreground text-xs'>
                数据来源：
                {stats.data.source === 'database_daily'
                  ? '数据库日汇总'
                  : '实时日汇总'}
                。当天数据每 15 秒刷新。累计禁用不受统计日期影响。
              </p>
            </>
          )}
        </TabsContent>
        <TabsContent value='settings' className='pt-3'>
          {settings.isError && (
            <ErrorState
              title='统计配置加载失败'
              onRetry={() => void settings.refetch()}
            />
          )}
          {settings.isPending && <LoadingState message='正在加载统计配置…' />}
          {settings.data && !settings.isPending && !settings.isError && (
            <RequestStatisticsSettings
              key={settings.data.revision}
              settings={settings.data}
            />
          )}
        </TabsContent>
        <TabsContent value='content-capture' className='pt-3'>
          {tab === 'content-capture' && <ContentCapturePanel />}
        </TabsContent>
      </Tabs>
      {history && (
        <RequestStatisticsHistory
          target={history}
          onClose={() => setHistory(null)}
        />
      )}
    </Dialog>
  )
}
