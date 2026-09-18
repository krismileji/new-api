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
import { Button } from '@/components/ui/button'
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
  deleteChannelMonitorVariableGroup,
  useChannelMonitorVariableGroups,
  variableGroupsQueryKey,
  type ChannelMonitorVariableGroup,
} from '../api-variable-groups'
import { handleChannelMonitorMutationError } from '../lib/error'
import { emptyVariableGroup } from '../lib/variable-group'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { ChannelMonitorVariableGroupEditor } from './channel-monitor-variable-group-editor'
import {
  upstreamEditorDialogClassName,
  upstreamEditorHeaderClassName,
} from './upstream-editor-layout'

type Props = {
  onOpenChange: (open: boolean) => void
  initialGroup?: ChannelMonitorVariableGroup
  onSelect?: (group: ChannelMonitorVariableGroup) => void
}

export function ChannelMonitorVariableGroupsDialog(props: Props) {
  const queryClient = useQueryClient()
  const busy = useIsMutating({ mutationKey: variableGroupsQueryKey }) > 0
  const groups = useChannelMonitorVariableGroups()
  const [editing, setEditing] = useState<ChannelMonitorVariableGroup | null>(
    props.initialGroup ?? null
  )
  const [deleting, setDeleting] = useState<ChannelMonitorVariableGroup | null>(
    null
  )
  const remove = useMutation({
    mutationKey: variableGroupsQueryKey,
    mutationFn: deleteChannelMonitorVariableGroup,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => {
      setDeleting(null)
      void queryClient.invalidateQueries({ queryKey: variableGroupsQueryKey })
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
                'flex h-[min(52rem,calc(100dvh-2rem))] max-w-4xl flex-col sm:max-w-4xl'
              )
        }
      >
        <DialogHeader
          className={editing ? upstreamEditorHeaderClassName : 'pr-8'}
        >
          <DialogTitle>
            {editing ? '编辑共享请求与变量' : '共享请求与变量'}
          </DialogTitle>
          <DialogDescription>
            {editing
              ? `${editing.name || '新共享配置'} · 请求与响应变量可并排配置，保存后由引用渠道共用。`
              : '集中配置上游请求，多个 API Key 对应的渠道可引用同一份变量。'}
          </DialogDescription>
        </DialogHeader>
        {editing ? (
          <ChannelMonitorVariableGroupEditor
            key={editing.id}
            group={editing}
            onCancel={() => setEditing(null)}
            onSaved={(saved) => {
              queryClient.setQueryData<ChannelMonitorVariableGroup[]>(
                variableGroupsQueryKey,
                (current) => [
                  ...(current ?? []).filter((group) => group.id !== saved.id),
                  saved,
                ]
              )
              void queryClient.invalidateQueries({
                queryKey: variableGroupsQueryKey,
              })
              void queryClient.invalidateQueries({
                queryKey: ['channel-monitor'],
              })
              if (props.onSelect) {
                props.onSelect(saved)
                props.onOpenChange(false)
              } else setEditing(null)
            }}
          />
        ) : (
          <>
            <div className='flex justify-end'>
              <Button onClick={() => setEditing(emptyVariableGroup())}>
                新建共享配置
              </Button>
            </div>
            <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto'>
              {groups.isPending ? (
                <div role='status' className='flex items-center gap-2'>
                  <Spinner />
                  正在加载共享配置
                </div>
              ) : null}
              {groups.isError ? (
                <Alert variant='destructive'>
                  <AlertDescription>
                    共享配置加载失败
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void groups.refetch()}
                    >
                      重试
                    </Button>
                  </AlertDescription>
                </Alert>
              ) : null}
              {groups.isSuccess && groups.data.length === 0 ? (
                <Empty className='border'>
                  <EmptyHeader>
                    <EmptyTitle>尚无共享配置</EmptyTitle>
                    <EmptyDescription>
                      新建一份请求与变量配置，再到各渠道的上游配置中选择引用。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : null}
              {groups.data?.map((group) => (
                <section
                  key={group.id}
                  aria-label={group.name}
                  className='flex flex-col gap-3 rounded-lg border p-4 sm:flex-row sm:items-center sm:justify-between'
                >
                  <div className='flex min-w-0 flex-col gap-1'>
                    <h3 className='font-medium break-words'>{group.name}</h3>
                    <p className='text-muted-foreground text-sm break-all'>
                      {group.base_url}
                    </p>
                    <p className='text-muted-foreground text-xs'>
                      {group.variable_requests.length} 个请求 ·{' '}
                      {group.variable_requests.reduce(
                        (sum, request) => sum + request.variables.length,
                        0
                      )}{' '}
                      个变量
                    </p>
                  </div>
                  <div className='flex shrink-0 gap-2'>
                    {props.onSelect ? (
                      <Button
                        variant='outline'
                        onClick={() => {
                          props.onSelect?.(group)
                          props.onOpenChange(false)
                        }}
                      >
                        使用此配置
                      </Button>
                    ) : null}
                    <Button variant='outline' onClick={() => setEditing(group)}>
                      编辑
                    </Button>
                    <Button variant='ghost' onClick={() => setDeleting(group)}>
                      删除
                    </Button>
                  </div>
                </section>
              ))}
            </div>
          </>
        )}
        <AlertDialog
          open={deleting !== null}
          onOpenChange={(open) => {
            if (!open && !remove.isPending) setDeleting(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除共享配置</AlertDialogTitle>
              <AlertDialogDescription>
                确定删除“{deleting?.name}”？仍被渠道引用的配置无法删除。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={remove.isPending}>
                取消
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={remove.isPending}
                onClick={(event) => {
                  event.preventDefault()
                  if (deleting) remove.mutate(deleting)
                }}
              >
                删除
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  )
}
