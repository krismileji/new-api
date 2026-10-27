import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type OnChangeFn,
  type PaginationState,
} from '@tanstack/react-table'
import { useMemo } from 'react'

import { DataTablePagination, DataTableView } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { formatNumber, formatPercent } from '@/lib/format'

import type {
  RequestStatsQuery,
  RequestStatsRow,
} from '../lib/request-statistics'

function requestStatsRowLabel(level: RequestStatsQuery['level']): string {
  if (level === 'user') return '用户'
  if (level === 'api_key') return 'API Key'
  return '渠道'
}

export function RequestStatisticsTable(props: {
  rows: RequestStatsRow[]
  total: number
  level: RequestStatsQuery['level']
  thresholdK: number
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
  onDrill: (row: RequestStatsRow) => void
  onHistory: (row: RequestStatsRow) => void
  loading: boolean
}) {
  const { level, thresholdK, onDrill, onHistory } = props
  const columns = useMemo<ColumnDef<RequestStatsRow>[]>(
    () => [
      {
        id: 'name',
        header: requestStatsRowLabel(level),
        cell: ({ row }) =>
          level === 'channel' ? (
            <span className='break-all'>
              {row.original.name}（#{row.original.id}）
            </span>
          ) : (
            <Button
              variant='link'
              className='h-auto max-w-72 text-left break-all whitespace-normal'
              onClick={() => onDrill(row.original)}
            >
              {row.original.name}（#{row.original.id}）
            </Button>
          ),
      },
      ...(
        [
          ['request_count', '请求数'],
          ['stream_request_count', '流式'],
          ['non_stream_request_count', '非流式'],
          ['known_input_request_count', '有效输入样本'],
          ['unknown_input_request_count', '未知输入'],
          ['low_input_count', `< ${thresholdK}K 请求`],
        ] as const
      ).map(([key, header]) => ({
        accessorKey: key,
        header,
        cell: ({ row }: { row: { original: RequestStatsRow } }) =>
          formatNumber(row.original[key]),
      })),
      {
        id: 'rate',
        header: '低输入占比',
        cell: ({ row }) =>
          row.original.low_input_rate === null
            ? '暂无可判定样本'
            : formatPercent(row.original.low_input_rate),
      },
      ...(level === 'channel'
        ? []
        : [
            {
              id: 'disabled',
              header: '累计禁用次数',
              cell: ({ row }: { row: { original: RequestStatsRow } }) => (
                <Button
                  variant='link'
                  onClick={() => onHistory(row.original)}
                  aria-label={`查看 ${row.original.name} 的禁用历史`}
                >
                  {formatNumber(row.original.low_input_auto_disabled_count)}
                </Button>
              ),
            },
          ]),
    ],
    [level, thresholdK, onDrill, onHistory]
  )
  const table = useReactTable({
    data: props.rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: props.total,
    state: { pagination: props.pagination },
    onPaginationChange: props.onPaginationChange,
  })
  return (
    <div className='space-y-3'>
      <DataTableView
        table={table}
        isLoading={props.loading}
        tableContainerClassName='overflow-x-auto'
        tableClassName='min-w-[64rem]'
        emptyTitle='当天没有符合条件的请求'
        emptyDescription='请选择其他日期或调整搜索条件。'
      />
      <DataTablePagination table={table} compact />
    </div>
  )
}
