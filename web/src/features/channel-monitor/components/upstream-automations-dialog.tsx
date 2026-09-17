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
  useIsMutating,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'

import {
  acknowledgeUpstreamAutomation,
  automationsQueryKey,
  deleteUpstreamAutomation,
  runUpstreamAutomation,
  useUpstreamAutomations,
  type UpstreamAutomation,
} from '../api-automations'
import { emptyUpstreamAutomation } from '../lib/automation'
import { handleChannelMonitorMutationError } from '../lib/error'
import type {
  ChannelMonitorCustomActionState,
  ChannelMonitorItem,
} from '../types'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { UpstreamAutomationEditor } from './upstream-automation-editor'

type Confirmation = {
  task: UpstreamAutomation
  actionId: string
  state: ChannelMonitorCustomActionState
}

export default function UpstreamAutomationsDialog(props: {
  channels: ChannelMonitorItem[]
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const query = useUpstreamAutomations()
  const busy = useIsMutating({ mutationKey: automationsQueryKey }) > 0
  const [editing, setEditing] = useState<UpstreamAutomation | null>(null)
  const [deleting, setDeleting] = useState<UpstreamAutomation | null>(null)
  const [confirming, setConfirming] = useState<Confirmation | null>(null)
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: automationsQueryKey })
    void queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
  }
  const run = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: runUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      toast.success('已安排立即检查，仍遵守触发条件、冷却和次数限制')
      refresh()
    },
  })
  const remove = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: deleteUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      setDeleting(null)
      refresh()
    },
  })
  const acknowledge = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: acknowledgeUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      setConfirming(null)
      refresh()
    },
  })

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!busy) props.onOpenChange(open)
      }}
    >
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'flex h-[min(54rem,calc(100dvh-2rem))] max-w-5xl flex-col sm:max-w-5xl'
        )}
      >
        <DialogHeader>
          <DialogTitle>
            {editing ? '编辑上游自动任务' : '上游自动任务'}
          </DialogTitle>
          <DialogDescription>
            按上游账户独立查询和执行，一个任务可关联多个渠道。旧渠道规则会自动迁移并保留执行限制。
          </DialogDescription>
        </DialogHeader>
        {editing ? (
          <UpstreamAutomationEditor
            key={editing.id || 'new'}
            task={editing}
            channels={props.channels}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null)
              refresh()
            }}
          />
        ) : (
          <>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <p className='text-muted-foreground text-sm'>
                每个上游账户配置一份任务，避免多个渠道重复重置。
              </p>
              <Button
                disabled={busy}
                onClick={() => setEditing(emptyUpstreamAutomation())}
              >
                新建自动任务
              </Button>
            </div>
            <div
              className='min-h-0 flex-1 space-y-4 overflow-y-auto px-1 pb-2'
              aria-label='上游自动任务列表'
            >
              {query.isPending ? (
                <div role='status' className='flex items-center gap-2 py-8'>
                  <Spinner />
                  正在加载任务
                </div>
              ) : null}
              {query.isError ? (
                <Alert variant='destructive'>
                  <AlertDescription>
                    任务加载失败：{query.error.message}
                    <Button
                      variant='outline'
                      onClick={() => void query.refetch()}
                    >
                      重试
                    </Button>
                  </AlertDescription>
                </Alert>
              ) : null}
              {query.data?.migrationWarning ? (
                <Alert variant='destructive'>
                  <AlertDescription>
                    部分旧规则尚未迁移，已有独立任务仍可运行：
                    {query.data.migrationWarning}
                  </AlertDescription>
                </Alert>
              ) : null}
              {query.isSuccess && query.data.tasks.length === 0 ? (
                <Empty className='border'>
                  <EmptyHeader>
                    <EmptyTitle>尚无上游自动任务</EmptyTitle>
                    <EmptyDescription>
                      添加余额或倍率查询和触发接口；无需先创建渠道。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : null}
              {query.data?.tasks.map((task) => {
                const running =
                  (task.state.lease_until ?? 0) > Date.now() / 1000
                let statusText = task.enabled ? '已启用' : '已暂停'
                if (running) statusText = '正在检查'
                return (
                  <Card key={task.id} className='gap-4'>
                    <CardHeader>
                      <div className='flex flex-wrap items-start justify-between gap-3'>
                        <div className='min-w-0 space-y-1.5'>
                          <CardTitle className='break-all'>
                            {task.name}
                          </CardTitle>
                          <CardDescription className='break-all'>
                            {task.base_url} · 每 {task.interval_minutes}{' '}
                            分钟检查 · 关联 {task.channel_ids?.length ?? 0}{' '}
                            个渠道
                          </CardDescription>
                        </div>
                        <Badge variant={task.enabled ? 'secondary' : 'outline'}>
                          {statusText}
                        </Badge>
                      </div>
                    </CardHeader>
                    <CardContent className='space-y-3'>
                      <p role='status' className='text-sm break-words'>
                        {task.state.message || '等待首次检查'}
                      </p>
                      <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs tabular-nums'>
                        <span>
                          上次检查：
                          {task.state.last_check
                            ? new Date(
                                task.state.last_check * 1000
                              ).toLocaleString('zh-CN')
                            : '尚未检查'}
                        </span>
                        <span>
                          下次检查：
                          {task.enabled && task.state.next_check
                            ? new Date(
                                task.state.next_check * 1000
                              ).toLocaleString('zh-CN')
                            : '—'}
                        </span>
                        {task.state.balance !== undefined ? (
                          <span>余额 {task.state.balance}</span>
                        ) : null}
                        {task.state.ratio !== undefined ? (
                          <span>倍率 {task.state.ratio}</span>
                        ) : null}
                      </div>
                      {task.custom_config.actions?.map((action) => {
                        const state = task.state.actions?.[action.id]
                        return (
                          <div
                            key={action.id}
                            className='bg-muted/40 space-y-1 rounded-md p-3 text-sm'
                          >
                            <p className='font-medium break-all'>
                              {action.name} ·{' '}
                              {action.trigger_mode === 'repeat'
                                ? '持续满足'
                                : '首次满足'}
                            </p>
                            <p className='text-muted-foreground break-words'>
                              {state?.skip_reason ||
                                state?.message ||
                                (action.enabled ? '等待检查' : '规则已关闭')}
                            </p>
                            {state?.last_attempt ? (
                              <p className='text-muted-foreground text-xs'>
                                最近调用：
                                {new Date(
                                  state.last_attempt * 1000
                                ).toLocaleString('zh-CN')}{' '}
                                · {state.day} 已调用 {state.attempts}/
                                {action.daily_limit} 次
                              </p>
                            ) : null}
                            {state?.attempt_id &&
                            (state.needs_confirmation || state.triggered) ? (
                              <Button
                                size='sm'
                                variant='outline'
                                disabled={busy || running}
                                onClick={() =>
                                  setConfirming({
                                    task,
                                    actionId: action.id,
                                    state,
                                  })
                                }
                              >
                                核对结果并解除触发限制
                              </Button>
                            ) : null}
                          </div>
                        )
                      })}
                      <div className='flex flex-wrap gap-2'>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={busy || running || !task.enabled}
                          onClick={() => run.mutate(task)}
                        >
                          立即检查
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={busy || running}
                          onClick={() => setEditing(task)}
                        >
                          编辑任务
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={busy || running}
                          onClick={() => setDeleting(task)}
                        >
                          删除任务
                        </Button>
                      </div>
                      {task.state.history?.length ? (
                        <details className='text-sm'>
                          <summary className='cursor-pointer py-1'>
                            最近检查记录
                          </summary>
                          <ol className='mt-2 space-y-2'>
                            {[...task.state.history].reverse().map((entry) => (
                              <li
                                key={entry.id}
                                className='text-muted-foreground break-words'
                              >
                                <time className='mr-2 tabular-nums'>
                                  {new Date(entry.time * 1000).toLocaleString(
                                    'zh-CN'
                                  )}
                                </time>
                                {entry.message}
                              </li>
                            ))}
                          </ol>
                        </details>
                      ) : null}
                    </CardContent>
                  </Card>
                )
              })}
            </div>
          </>
        )}
        <AlertDialog
          open={deleting !== null || confirming !== null}
          onOpenChange={(open) => {
            if (!open && !busy) {
              setDeleting(null)
              setConfirming(null)
            }
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {deleting ? '删除上游自动任务' : '确认上次接口结果'}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {deleting
                  ? `删除“${deleting.name}”及其运行记录？关联渠道的配置和状态将保留。`
                  : '请先核对上游是否已经执行操作。确认后解除待确认和已触发状态，后续检查可能再次调用接口；今日次数与冷却时间保留。'}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
              <AlertDialogAction
                disabled={busy}
                onClick={(event) => {
                  event.preventDefault()
                  if (deleting) remove.mutate(deleting)
                  else if (confirming) acknowledge.mutate(confirming)
                }}
              >
                {deleting ? '删除任务' : '已核对，解除限制'}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  )
}
