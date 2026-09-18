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
import { ArrowDown01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  useIsMutating,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { useId, useMemo, useState } from 'react'
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
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

import {
  acknowledgeUpstreamAutomation,
  automationsQueryKey,
  deleteUpstreamAutomation,
  runUpstreamAutomation,
  saveUpstreamAutomation,
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
import {
  upstreamEditorDialogClassName,
  upstreamEditorHeaderClassName,
} from './upstream-editor-layout'

type Confirmation = {
  task: UpstreamAutomation
  actionId: string
  state: ChannelMonitorCustomActionState
}

export default function UpstreamAutomationsDialog(props: {
  channels: ChannelMonitorItem[]
  onOpenChange: (open: boolean) => void
}) {
  const id = useId()
  const queryClient = useQueryClient()
  const query = useUpstreamAutomations()
  const busy = useIsMutating({ mutationKey: automationsQueryKey }) > 0
  const [editing, setEditing] = useState<UpstreamAutomation | null>(null)
  const [deleting, setDeleting] = useState<UpstreamAutomation | null>(null)
  const [confirming, setConfirming] = useState<Confirmation | null>(null)
  const [search, setSearch] = useState('')
  const channelNames = useMemo(
    () => new Map(props.channels.map((channel) => [channel.id, channel.name])),
    [props.channels]
  )
  const tasks = query.data?.tasks ?? []
  const keyword = search.trim().toLowerCase()
  const filteredTasks = tasks.filter(
    (task) =>
      `${task.name} ${task.base_url}`.toLowerCase().includes(keyword) ||
      task.channel_ids?.some((id) =>
        `${id} ${channelNames.get(id) ?? ''}`.toLowerCase().includes(keyword)
      )
  )
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
  const toggle = useMutation({
    mutationKey: automationsQueryKey,
    mutationFn: saveUpstreamAutomation,
    onError: handleChannelMonitorMutationError,
    onSuccess: async (saved) => {
      await queryClient.cancelQueries({ queryKey: automationsQueryKey })
      queryClient.setQueryData<NonNullable<typeof query.data>>(
        automationsQueryKey,
        (current) => {
          if (!current) return current
          return {
            ...current,
            tasks: current.tasks.map((task) =>
              task.id === saved.id ? saved : task
            ),
          }
        }
      )
      toast.success(saved.enabled ? '任务已启用' : '任务已暂停')
    },
    onSettled: refresh,
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
        className={
          editing
            ? upstreamEditorDialogClassName
            : channelMonitorDialogContentClassName(
                'flex h-[min(54rem,calc(100dvh-2rem))] flex-col gap-3 sm:max-w-5xl'
              )
        }
      >
        <DialogHeader
          className={editing ? upstreamEditorHeaderClassName : 'pr-8'}
        >
          <DialogTitle>
            {editing ? '编辑上游自动任务' : '上游自动任务'}
          </DialogTitle>
          <DialogDescription>
            {editing
              ? `${editing.name || '新自动任务'} · 配置指标来源与触发规则，保存和测试获取不会执行触发接口。`
              : '按上游账户独立查询和执行，一个任务可关联多个渠道。旧渠道规则会自动迁移并保留执行限制。'}
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
            <div className='flex flex-wrap items-center gap-2'>
              <Input
                type='search'
                aria-label='搜索上游任务'
                placeholder='搜索任务、地址或关联渠道'
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                className='min-w-0 flex-1 basis-56'
              />
              <span className='text-muted-foreground text-xs tabular-nums'>
                {keyword ? `${filteredTasks.length} / ` : ''}
                {tasks.length} 个任务
              </span>
              <Button
                size='sm'
                disabled={busy}
                onClick={() => setEditing(emptyUpstreamAutomation())}
              >
                新建自动任务
              </Button>
            </div>
            <div className='flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-1 pb-2'>
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
              {query.isSuccess &&
              tasks.length > 0 &&
              filteredTasks.length === 0 ? (
                <Empty>
                  <EmptyHeader>
                    <EmptyTitle>没有匹配的任务</EmptyTitle>
                    <EmptyDescription>
                      试试其他任务名称、地址或渠道。
                    </EmptyDescription>
                  </EmptyHeader>
                  <Button
                    size='sm'
                    variant='outline'
                    onClick={() => setSearch('')}
                  >
                    清空搜索
                  </Button>
                </Empty>
              ) : null}
              <div
                role='list'
                aria-label='上游自动任务列表'
                className='flex flex-col gap-2'
              >
                {filteredTasks.map((task) => {
                  const saving =
                    toggle.isPending && toggle.variables.id === task.id
                  const switchId = `${id}-${task.id}-enabled`
                  const running =
                    (task.state.lease_until ?? 0) > Date.now() / 1000
                  const pendingConfirmations =
                    task.custom_config.actions?.filter((action) => {
                      const state = task.state.actions?.[action.id]
                      return (
                        state?.attempt_id &&
                        (state.needs_confirmation || state.triggered)
                      )
                    }).length ?? 0
                  let statusText = task.enabled ? '已启用' : '已暂停'
                  if (running) statusText = '正在检查'
                  return (
                    <Collapsible
                      key={task.id}
                      defaultOpen={pendingConfirmations > 0}
                      render={<Card size='sm' role='listitem' />}
                    >
                      <CardHeader className='flex flex-col items-stretch justify-between gap-2 sm:flex-row sm:items-start'>
                        <div className='flex min-w-0 flex-1 flex-col gap-1'>
                          <div className='flex min-w-0 items-center gap-2'>
                            <CardTitle className='truncate' title={task.name}>
                              {task.name}
                            </CardTitle>
                            <Badge
                              variant={task.enabled ? 'secondary' : 'outline'}
                            >
                              {statusText}
                            </Badge>
                            {pendingConfirmations > 0 ? (
                              <Badge variant='destructive'>
                                {pendingConfirmations} 条待核对
                              </Badge>
                            ) : null}
                          </div>
                          <p
                            className='text-muted-foreground truncate text-xs'
                            title={task.base_url}
                          >
                            {task.base_url}
                          </p>
                        </div>
                        <div className='flex shrink-0 flex-wrap items-center gap-1'>
                          <div
                            className='flex items-center gap-2 px-2'
                            title={
                              running
                                ? '任务正在检查，完成后可切换启停'
                                : undefined
                            }
                          >
                            <Switch
                              id={switchId}
                              checked={task.enabled}
                              disabled={busy || running}
                              aria-labelledby={`${switchId}-label`}
                              aria-busy={saving}
                              onCheckedChange={(enabled) =>
                                toggle.mutate({ ...task, enabled })
                              }
                            />
                            <Label id={`${switchId}-label`} htmlFor={switchId}>
                              启用任务
                              <span className='sr-only'>：{task.name}</span>
                            </Label>
                            {saving ? (
                              <Spinner aria-label='正在保存任务状态' />
                            ) : null}
                          </div>
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
                      </CardHeader>
                      <CardContent className='flex min-w-0 flex-col gap-1'>
                        <div className='flex flex-wrap items-center gap-x-3 gap-y-1'>
                          <p
                            role='status'
                            className='min-w-0 flex-1 basis-48 truncate text-sm'
                            title={task.state.message || '等待首次检查'}
                          >
                            {task.state.message || '等待首次检查'}
                          </p>
                          <span className='text-muted-foreground text-xs'>
                            每 {task.interval_minutes} 分钟 ·{' '}
                            {task.channel_ids?.length ?? 0} 个渠道
                          </span>
                          <CollapsibleTrigger
                            className='group'
                            render={<Button size='sm' variant='ghost' />}
                          >
                            规则与记录（
                            {task.custom_config.actions?.length ?? 0}）
                            <HugeiconsIcon
                              icon={ArrowDown01Icon}
                              aria-hidden='true'
                              className='transition-transform group-aria-expanded:rotate-180'
                            />
                          </CollapsibleTrigger>
                        </div>
                        <CollapsibleContent className='flex min-w-0 flex-col gap-2 pt-2'>
                          <p className='text-sm break-all'>
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
                                className='bg-muted/40 flex min-w-0 flex-col gap-1 rounded-md p-2 text-sm'
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
                                    (action.enabled
                                      ? '等待检查'
                                      : '规则已关闭')}
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
                                (state.needs_confirmation ||
                                  state.triggered) ? (
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
                          {task.state.history?.length ? (
                            <details className='text-sm'>
                              <summary className='cursor-pointer py-1'>
                                最近检查记录
                              </summary>
                              <ol className='mt-2 space-y-2'>
                                {[...task.state.history]
                                  .reverse()
                                  .map((entry) => (
                                    <li
                                      key={entry.id}
                                      className='text-muted-foreground break-words'
                                    >
                                      <time className='mr-2 tabular-nums'>
                                        {new Date(
                                          entry.time * 1000
                                        ).toLocaleString('zh-CN')}
                                      </time>
                                      {entry.message}
                                    </li>
                                  ))}
                              </ol>
                            </details>
                          ) : null}
                        </CollapsibleContent>
                      </CardContent>
                    </Collapsible>
                  )
                })}
              </div>
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
