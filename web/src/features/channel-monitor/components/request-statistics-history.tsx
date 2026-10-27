import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useState } from 'react'

import { DataTablePagination, DataTableView } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { formatNumber, formatPercent } from '@/lib/format'

import { formatChannelMonitorBeijingDate } from '../lib/cost-date'
import {
  getRequestStatsHistory,
  requestStatsQueryKey,
  type RequestStatsDisable,
} from '../lib/request-statistics'

const mailLabels: Record<string, string> = {
  pending: '等待通知',
  sending: '发送中',
  sent: '已通知',
  retry: '等待重试',
  skipped: '已跳过',
  failed: '通知失败',
}
const columns: ColumnDef<RequestStatsDisable>[] = [
  {
    id: 'day',
    header: '判定日期（北京）',
    cell: ({ row }) =>
      formatChannelMonitorBeijingDate(new Date(row.original.day_start * 1000)),
  },
  {
    id: 'key',
    header: 'API Key',
    cell: ({ row }) =>
      `${row.original.api_key_name || 'API Key'}（#${row.original.api_key_id}）`,
  },
  {
    id: 'policy',
    header: '判定配置',
    cell: ({ row }) =>
      `< ${row.original.low_input_threshold_k}K · ≥ ${row.original.disable_rate_percent}% · 至少 ${formatNumber(row.original.minimum_known_input_requests)} 次 · v${row.original.config_revision}`,
  },
  {
    id: 'sample',
    header: '低输入 / 有效样本',
    cell: ({ row }) =>
      `${formatNumber(row.original.low_input_count)} / ${formatNumber(row.original.known_input_request_count)}（${formatPercent((row.original.low_input_count / row.original.known_input_request_count) * 100)}）`,
  },
  {
    id: 'restore',
    header: '恢复记录',
    cell: ({ row }) =>
      row.original.restored_at
        ? `${formatChannelMonitorBeijingDate(new Date(row.original.restored_at * 1000))} · ${row.original.restored_by === 'admin' ? '管理员' : '用户自行恢复'}`
        : '尚未恢复',
  },
  {
    id: 'mail',
    header: '邮件',
    cell: ({ row }) => (
      <span title={row.original.mail_last_error}>
        {mailLabels[row.original.mail_status] || row.original.mail_status} ·{' '}
        {row.original.mail_attempts} 次
        {row.original.mail_last_error
          ? `（${row.original.mail_last_error}）`
          : ''}
      </span>
    ),
  },
]

export function RequestStatisticsHistory(props: {
  target: { name: string; user_id: number; api_key_id?: number }
  onClose: () => void
}) {
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const query = {
    user_id: props.target.user_id,
    api_key_id: props.target.api_key_id,
    page: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
  }
  const history = useQuery({
    queryKey: [...requestStatsQueryKey, 'history', query],
    queryFn: ({ signal }) => getRequestStatsHistory(query, signal),
  })
  const table = useReactTable({
    data: history.data?.items ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: history.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={`${props.target.name} · 累计禁用历史`}
      description='仅包含本功能成功执行的禁用，与选中的统计日期无关。'
      contentClassName='sm:max-w-6xl'
      bodyClassName='space-y-3'
    >
      {history.isError ? (
        <ErrorState
          title='禁用历史加载失败'
          onRetry={() => void history.refetch()}
        />
      ) : (
        <>
          <DataTableView
            table={table}
            isLoading={history.isPending}
            tableContainerClassName='overflow-x-auto'
            tableClassName='min-w-[64rem]'
            emptyTitle='暂无自动禁用记录'
          />
          <DataTablePagination table={table} compact />
        </>
      )}
    </Dialog>
  )
}
