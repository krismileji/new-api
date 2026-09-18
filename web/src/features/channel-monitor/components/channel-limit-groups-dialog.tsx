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
import { useMutation, useQueryClient } from '@tanstack/react-query'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
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
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'

import {
  channelLimitGroupsKey,
  channelLimitRuntimeLabel,
  channelLimitErrorMessage,
  deleteChannelLimitGroup,
  saveChannelLimitGroup,
  useChannelLimitGroups,
  type ChannelLimitGroup,
} from '../api-limit-groups'
import { emptyChannelLimitGroup } from '../lib/limit-group'
import type { ChannelMonitorItem } from '../types'
import { ChannelLimitGroupEditor } from './channel-limit-group-editor'

type Props = {
  channels: ChannelMonitorItem[]
  onOpenChange: (open: boolean) => void
}
export function ChannelLimitGroupsDialog(props: Props) {
  const query = useChannelLimitGroups()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<ChannelLimitGroup | null>(null)
  const [confirming, setConfirming] = useState<ChannelLimitGroup | null>(null)
  const mutation = useMutation({
    mutationFn: async (input: {
      group: ChannelLimitGroup
      action: 'toggle' | 'delete'
    }) => {
      if (input.action === 'delete') return deleteChannelLimitGroup(input.group)
      return saveChannelLimitGroup({
        ...input.group,
        enabled: !input.group.enabled,
      })
    },
    onSuccess: () => {
      setConfirming(null)
      void queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: channelLimitGroupsKey })
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!mutation.isPending && !editing) props.onOpenChange(open)
      }}
    >
      <DialogContent className='flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{editing ? '编辑共享限流组' : '共享限流组'}</DialogTitle>
          <DialogDescription>
            同一上游共用并发与 RPM，按资源等级严格预留额度。
          </DialogDescription>
        </DialogHeader>
        {editing ? (
          <ChannelLimitGroupEditor
            key={`${editing.id}-${editing.revision}`}
            group={editing}
            groups={query.data ?? []}
            channels={props.channels}
            onDone={() => setEditing(null)}
          />
        ) : (
          <>
            <div className='flex shrink-0 justify-end gap-2'>
              <Button
                variant='outline'
                disabled={query.isFetching}
                onClick={() => void query.refetch()}
              >
                刷新状态
              </Button>
              <Button
                disabled={
                  query.isPending || query.isError || mutation.isPending
                }
                onClick={() => setEditing(emptyChannelLimitGroup())}
              >
                新建共享限流组
              </Button>
            </div>
            {query.isPending && (
              <div role='status' className='flex items-center gap-2'>
                <Spinner />
                正在加载共享限流组
              </div>
            )}
            {(query.isError || mutation.isError) && (
              <Alert variant='destructive'>
                <AlertDescription>
                  {channelLimitErrorMessage(mutation.error ?? query.error)}
                </AlertDescription>
              </Alert>
            )}
            <div className='flex min-h-0 flex-col gap-3 overflow-y-auto'>
              {query.data?.length === 0 && (
                <Empty>
                  <EmptyHeader>
                    <EmptyTitle>暂无共享限流组</EmptyTitle>
                    <EmptyDescription>
                      选择共用上游额度的渠道，配置总量与高等级预留。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
              {query.data?.map((group) => (
                <Card key={group.id}>
                  <CardHeader>
                    <CardTitle>{group.name}</CardTitle>
                    <CardDescription>
                      #{group.id} · 版本 {group.revision} ·{' '}
                      {group.members.length} 个成员
                    </CardDescription>
                  </CardHeader>
                  <CardContent className='flex flex-col gap-3'>
                    <Badge variant='secondary'>
                      {channelLimitRuntimeLabel(group.runtime?.reason)}
                    </Badge>
                    <div className='grid gap-2 text-sm sm:grid-cols-3'>
                      <span>
                        组并发：{group.runtime?.active ?? '—'} /{' '}
                        {group.concurrency_limit || '不限'}
                      </span>
                      <span>
                        限流 RPM：{group.runtime?.rpm ?? '—'} /{' '}
                        {group.rpm_limit || '不限'}
                      </span>
                      <span>等待：{group.runtime?.waiting ?? '—'}</span>
                    </div>
                    <div className='text-muted-foreground flex flex-col gap-1 text-xs'>
                      {group.tiers.map((tier) => (
                        <p key={tier.priority}>
                          已用{' '}
                          {group.runtime?.tiers?.find(
                            (usage) => usage.priority === tier.priority
                          )?.active ?? '—'}{' '}
                          并发 /{' '}
                          {group.runtime?.tiers?.find(
                            (usage) => usage.priority === tier.priority
                          )?.rpm ?? '—'}{' '}
                          RPM · 优先级 {tier.priority}：预留{' '}
                          {tier.reserved_concurrency} 并发 / {tier.reserved_rpm}{' '}
                          RPM；成员{' '}
                          {group.members
                            .filter(
                              (member) => member.priority === tier.priority
                            )
                            .map(
                              (member) =>
                                props.channels.find(
                                  (channel) => channel.id === member.channel_id
                                )?.name ?? `#${member.channel_id}`
                            )
                            .join('、') || '暂无'}
                        </p>
                      ))}
                    </div>
                    {!group.enabled && (
                      <p className='text-muted-foreground text-xs'>
                        暂停期间不接收新请求，已有请求继续执行。可随时启用或编辑配置。
                      </p>
                    )}
                  </CardContent>
                  <CardFooter className='flex flex-wrap gap-2'>
                    <Button
                      variant='outline'
                      disabled={mutation.isPending}
                      onClick={() => {
                        mutation.reset()
                        setEditing(group)
                      }}
                    >
                      编辑
                    </Button>
                    <Button
                      variant='outline'
                      disabled={mutation.isPending}
                      onClick={() =>
                        mutation.mutate({ group, action: 'toggle' })
                      }
                    >
                      {group.enabled ? '暂停准入' : '启用'}
                    </Button>
                    <Button
                      variant='outline'
                      disabled={mutation.isPending}
                      onClick={() => setConfirming(group)}
                    >
                      删除
                    </Button>
                  </CardFooter>
                </Card>
              ))}
            </div>
          </>
        )}
        <AlertDialog
          open={!!confirming}
          onOpenChange={(open) => {
            if (!open && !mutation.isPending) setConfirming(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除共享限流组？</AlertDialogTitle>
              <AlertDialogDescription>
                删除后成员恢复单渠道限流，已有请求继续执行，已用并发和 RPM
                计数保留。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={mutation.isPending}>
                取消
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={mutation.isPending}
                onClick={() => {
                  if (confirming) {
                    mutation.mutate({ group: confirming, action: 'delete' })
                  }
                }}
              >
                确认删除
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  )
}
