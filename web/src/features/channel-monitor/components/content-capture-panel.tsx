import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useMemo, useState } from 'react'

import { DataTablePagination, DataTableView } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { formatNumber, formatTimestamp } from '@/lib/format'

import {
  contentCaptureQueryKey,
  getContentCapture,
  type ContentCaptureSummary,
} from '../lib/content-capture'
import { ContentCaptureRecordDialog } from './content-capture-record-dialog'
import { ContentCaptureSettings } from './content-capture-settings'

type UserTotal = {
  user_id: number
  username: string
  count: number
  tokens: number
  models: number
}

function summarizeUsers(items: ContentCaptureSummary[]): UserTotal[] {
  const users = new Map<
    number,
    UserTotal & { tokenIds: Set<number>; modelNames: Set<string> }
  >()
  for (const item of items) {
    const user = users.get(item.user_id) ?? {
      user_id: item.user_id,
      username: item.username,
      count: 0,
      tokens: 0,
      models: 0,
      tokenIds: new Set<number>(),
      modelNames: new Set<string>(),
    }
    user.count += 1
    user.tokenIds.add(item.token_id)
    user.modelNames.add(item.model)
    users.set(item.user_id, user)
  }
  return [...users.values()]
    .map((user) => ({
      user_id: user.user_id,
      username: user.username,
      count: user.count,
      tokens: user.tokenIds.size,
      models: user.modelNames.size,
    }))
    .sort((a, b) => b.count - a.count)
}

function matchesSearch(item: ContentCaptureSummary, search: string): boolean {
  if (!search) return true
  return [
    item.username,
    String(item.user_id),
    item.token_name,
    String(item.token_id),
    item.model,
    item.ip,
    item.request_preview,
  ].some((value) => value.toLowerCase().includes(search))
}

export function ContentCapturePanel() {
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const capture = useQuery({
    queryKey: contentCaptureQueryKey,
    queryFn: ({ signal }) => getContentCapture(signal),
    refetchInterval: (query) =>
      query.state.data?.config.enabled && !selected ? 5000 : false,
  })
  const items = useMemo(() => capture.data?.items ?? [], [capture.data])
  const keyword = search.trim().toLowerCase()
  const rows = useMemo(
    () => items.filter((item) => matchesSearch(item, keyword)),
    [items, keyword]
  )
  const users = useMemo(() => summarizeUsers(items), [items])
  const columns = useMemo<ColumnDef<ContentCaptureSummary>[]>(
    () => [
      {
        id: 'time',
        header: '时间',
        cell: ({ row }) => formatTimestamp(row.original.created_at),
      },
      {
        id: 'user',
        header: '用户 / API Key',
        cell: ({ row }) => (
          <span className='break-all'>
            {row.original.username || '-'}（#{row.original.user_id}）
            <br />
            <span className='text-muted-foreground text-xs'>
              {row.original.token_name || '-'}（#{row.original.token_id}）
            </span>
          </span>
        ),
      },
      {
        id: 'model',
        header: '模型',
        cell: ({ row }) => (
          <span className='break-all'>{row.original.model || '-'}</span>
        ),
      },
      {
        id: 'status',
        header: '状态',
        cell: ({ row }) =>
          `${row.original.status_code}${row.original.is_stream ? ' 流式' : ''}`,
      },
      {
        id: 'size',
        header: '请求 / 响应字节',
        cell: ({ row }) =>
          `${formatNumber(row.original.request_size)} / ${formatNumber(row.original.response_size)}`,
      },
      {
        id: 'preview',
        header: '请求预览',
        cell: ({ row }) => (
          <span className='line-clamp-2 max-w-96 text-xs break-all'>
            {row.original.request_preview}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '操作',
        cell: ({ row }) => (
          <Button
            variant='link'
            onClick={() => setSelected(row.original.id)}
            aria-label={`查看请求 ${row.original.id}`}
          >
            查看
          </Button>
        ),
      },
    ],
    []
  )
  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageIndex: 0, pageSize: 20 } },
    autoResetPageIndex: false,
  })

  if (capture.isPending) return <LoadingState message='正在加载抓取状态…' />
  if (capture.isError || !capture.data) {
    return (
      <ErrorState
        title='抓取状态加载失败'
        description='该功能需要启用 Redis。'
        onRetry={() => void capture.refetch()}
      />
    )
  }
  return (
    <div className='space-y-4'>
      <ContentCaptureSettings
        key={`${capture.data.config.enabled}-${capture.data.config.started_at}`}
        config={capture.data.config}
      />
      {users.length > 0 && (
        <section className='space-y-2'>
          <h3 className='font-medium'>按用户汇总（当前已记录）</h3>
          <div className='flex flex-wrap gap-2'>
            {users.slice(0, 20).map((user) => (
              <Button
                key={user.user_id}
                variant='outline'
                size='sm'
                onClick={() => {
                  setSearch(String(user.user_id))
                  table.setPageIndex(0)
                }}
              >
                {user.username || '-'}（#{user.user_id}）
                {formatNumber(user.count)} 条 · {user.tokens} Key ·{' '}
                {user.models} 模型
              </Button>
            ))}
          </div>
        </section>
      )}
      <div className='flex flex-wrap items-end gap-3'>
        <label className='space-y-1 text-sm'>
          筛选
          <Input
            aria-label='筛选抓取记录'
            placeholder='用户、Key、模型、IP 或请求内容'
            value={search}
            maxLength={255}
            className='w-80'
            onChange={(event) => {
              setSearch(event.target.value)
              table.setPageIndex(0)
            }}
          />
        </label>
        <Button
          variant='outline'
          disabled={capture.isFetching}
          onClick={() => void capture.refetch()}
        >
          刷新记录
        </Button>
        <span className='text-muted-foreground text-sm'>
          共 {formatNumber(rows.length)} / {formatNumber(items.length)} 条
        </span>
      </div>
      <DataTableView
        table={table}
        isLoading={false}
        tableContainerClassName='overflow-x-auto'
        tableClassName='min-w-[64rem]'
        emptyTitle='暂无抓取记录'
        emptyDescription={
          capture.data.config.enabled
            ? '新的中继请求完成后会出现在这里。'
            : '开启抓取后开始记录。'
        }
      />
      <DataTablePagination table={table} compact />
      {selected && (
        <ContentCaptureRecordDialog
          id={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  )
}
