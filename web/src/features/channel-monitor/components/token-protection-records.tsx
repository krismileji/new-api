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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { handleChannelMonitorMutationError } from '../lib/error'
import {
  getTokenProtectionRecords,
  releaseTokenProtection,
  tokenProtectionQueryKey,
  type TokenProtectionRecord,
} from '../lib/token-protection'

export function TokenProtectionRecords() {
  const [page, setPage] = useState(1)
  const [releasing, setReleasing] = useState<TokenProtectionRecord | null>(null)
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: [...tokenProtectionQueryKey, 'records', page],
    queryFn: () => getTokenProtectionRecords(page),
    refetchInterval: 5000,
  })
  const release = useMutation({
    mutationFn: releaseTokenProtection,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      setReleasing(null)
      void queryClient.invalidateQueries({
        queryKey: [...tokenProtectionQueryKey, 'records'],
      })
      toast.success('已解除自动禁用')
    },
  })
  const pendingIds = new Set(query.data?.pending.map((record) => record.id))
  const records = [
    ...(query.data?.pending ?? []),
    ...(query.data?.records.filter((record) => !pendingIds.has(record.id)) ??
      []),
  ]
  return (
    <div className='flex min-h-0 flex-1 flex-col gap-4'>
      <div className='flex items-center justify-between gap-2'>
        <p className='text-muted-foreground text-sm'>
          仅管理员可解除。解除后，过期或额度不足的 Key 仍无法使用。
        </p>
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          刷新记录
        </Button>
      </div>
      <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto'>
        {query.isPending && <p role='status'>正在加载禁用记录…</p>}
        {query.isError && (
          <Alert variant='destructive'>
            <AlertDescription>禁用记录加载失败，请刷新重试。</AlertDescription>
          </Alert>
        )}
        {query.isSuccess && records.length === 0 && (
          <p className='text-muted-foreground py-8 text-center'>
            暂无自动禁用记录
          </p>
        )}
        {records.map((record) => (
          <article
            key={record.id}
            aria-label={`API Key ${record.token_id} 禁用记录`}
            className='flex flex-col gap-3 rounded-lg border p-4'
          >
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='flex flex-wrap items-center gap-2'>
                <span className='font-medium'>
                  {record.token_name || '未命名 Key'} · #{record.token_id}
                </span>
                <Badge
                  variant={record.released_at ? 'secondary' : 'destructive'}
                >
                  {record.released_at ? '已解除' : '已禁用'}
                </Badge>
              </div>
              {!record.released_at && (
                <Button
                  variant='outline'
                  size='sm'
                  disabled={pendingIds.has(record.id) || release.isPending}
                  onClick={() => setReleasing(record)}
                >
                  解除禁用
                </Button>
              )}
            </div>
            <p className='text-muted-foreground text-sm'>
              用户 #{record.user_id} · 渠道 #{record.channel_id} ·{' '}
              {new Date(record.created_at * 1000).toLocaleString()} · 已通知取消{' '}
              {record.canceled_requests} 个请求
            </p>
            <p className='text-sm'>
              命中规则：{record.rule_name} · 上游 {record.upstream_status} →
              返回 {record.response_status}
            </p>
            <p className='text-sm break-words'>{record.response_message}</p>
            <details className='text-muted-foreground text-sm'>
              <summary className='cursor-pointer'>查看触发错误</summary>
              <p className='mt-2 break-all'>请求：{record.request_id}</p>
              <pre className='mt-2 break-all whitespace-pre-wrap'>
                {record.error_summary}
              </pre>
            </details>
            {pendingIds.has(record.id) && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {record.persistence_error || '本机已拦截，禁用记录正在保存。'}
                </AlertDescription>
              </Alert>
            )}
            {!!record.released_at && (
              <p className='text-muted-foreground text-sm'>
                管理员 #{record.released_by} 于{' '}
                {new Date(record.released_at * 1000).toLocaleString()} 解除
              </p>
            )}
          </article>
        ))}
      </div>
      <div className='flex items-center justify-end gap-3 border-t pt-3'>
        <span className='text-muted-foreground text-sm'>
          第 {page} 页 · 共 {query.data?.total ?? 0} 条
        </span>
        <Button
          variant='outline'
          disabled={page <= 1 || query.isFetching}
          onClick={() => setPage((value) => value - 1)}
        >
          上一页
        </Button>
        <Button
          variant='outline'
          disabled={
            !query.data || page * 20 >= query.data.total || query.isFetching
          }
          onClick={() => setPage((value) => value + 1)}
        >
          下一页
        </Button>
      </div>
      <ConfirmDialog
        open={releasing !== null}
        onOpenChange={(open) => {
          if (!open && !release.isPending) setReleasing(null)
        }}
        title='解除 API Key 自动禁用'
        desc={`解除后，${releasing?.token_name || '该 Key'} 的新请求可以重新通过此项检查；已中断的请求不会恢复。`}
        confirmText='解除禁用'
        isLoading={release.isPending}
        handleConfirm={() => {
          if (releasing) release.mutate(releasing.id)
        }}
      />
    </div>
  )
}
