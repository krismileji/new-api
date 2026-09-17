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
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
  deleteUpstreamAccount,
  refreshUpstreamAccount,
  useUpstreamAccounts,
  type UpstreamAccount,
} from '../api-upstream-accounts'
import { handleChannelMonitorMutationError } from '../lib/error'
import type { ChannelMonitorItem } from '../types'
import { ChannelMonitorBalanceCell } from './channel-monitor-balance-cell'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { UpstreamAccountEditor } from './upstream-account-editor'
import { UpstreamAccountTaskMerge } from './upstream-account-task-merge'
import { UpstreamConfigDialog } from './upstream-config-dialog'

export default function UpstreamAccountsDialog(props: {
  channels: ChannelMonitorItem[]
  onOpenChange: (open: boolean) => void
}) {
  const query = useUpstreamAccounts()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<{ account?: UpstreamAccount } | null>(
    null
  )
  const [configuring, setConfiguring] = useState<UpstreamAccount | null>(null)
  const [merging, setMerging] = useState<UpstreamAccount | null>(null)
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
  }
  const poll = useMutation({
    mutationFn: refreshUpstreamAccount,
    onError: handleChannelMonitorMutationError,
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: deleteUpstreamAccount,
    onError: handleChannelMonitorMutationError,
    onSuccess: refresh,
  })
  const busy = poll.isPending || remove.isPending
  const accounts = query.data ?? []
  const configuredChannel = props.channels.find((channel) =>
    configuring?.channel_ids.includes(channel.id)
  )
  return (
    <>
      <Dialog open onOpenChange={props.onOpenChange}>
        <DialogContent
          className={channelMonitorDialogContentClassName(
            'flex h-[min(52rem,90dvh)] flex-col sm:max-w-3xl'
          )}
        >
          <DialogHeader>
            <DialogTitle>上游账户</DialogTitle>
            <DialogDescription>
              同一余额池配置一次。渠道分别保留倍率、分组和请求统计。
            </DialogDescription>
          </DialogHeader>
          <div className='min-h-0 flex-1 overflow-y-auto px-1'>
            {query.isPending ? <Spinner aria-label='加载上游账户' /> : null}
            {query.isError ? (
              <Alert variant='destructive'>
                <AlertDescription>
                  账户加载失败。
                  <Button variant='link' onClick={() => void query.refetch()}>
                    重试
                  </Button>
                </AlertDescription>
              </Alert>
            ) : null}
            {editing ? (
              <UpstreamAccountEditor
                account={editing.account}
                channels={props.channels}
                onClose={() => setEditing(null)}
                onSaved={() => {
                  setEditing(null)
                  refresh()
                  toast.success('上游账户已保存，请刷新账户余额')
                }}
              />
            ) : (
              <div className='flex flex-col gap-4'>
                {merging ? (
                  <UpstreamAccountTaskMerge
                    key={merging.id}
                    account={merging}
                    channels={props.channels}
                    onClose={() => setMerging(null)}
                  />
                ) : null}
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <p className='text-muted-foreground text-sm'>
                    {accounts.length} 个账户 · 每个余额池单独列示
                  </p>
                  <Button
                    disabled={busy || query.isPending || query.isError}
                    onClick={() => setEditing({})}
                  >
                    从渠道创建账户
                  </Button>
                </div>
                {!query.isPending && !query.isError && accounts.length === 0 ? (
                  <Empty>
                    <EmptyHeader>
                      <EmptyTitle>尚无上游账户</EmptyTitle>
                      <EmptyDescription>
                        选择一个已配置渠道创建账户，再批量关联共用余额的渠道。
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : null}
                {accounts.map((account) => (
                  <Card key={account.id}>
                    <CardHeader>
                      <CardTitle className='flex flex-wrap items-center gap-2'>
                        {account.name}
                        <Badge variant='secondary'>
                          {account.channel_ids.length} 个渠道
                        </Badge>
                      </CardTitle>
                    </CardHeader>
                    <CardContent className='flex flex-col gap-3'>
                      <p className='text-muted-foreground text-sm break-all'>
                        {account.upstream.base_url}
                      </p>
                      {account.balance === null ? (
                        <p>
                          余额待同步
                          {account.last_balance_error
                            ? `：${account.last_balance_error}`
                            : ''}
                        </p>
                      ) : (
                        <ChannelMonitorBalanceCell
                          balance={account.balance}
                          enabled={account.upstream.balance_sync_enabled}
                          warning={account.upstream.balance_warning_threshold}
                          error={account.last_balance_error}
                          estimate={account.balance_estimate}
                        />
                      )}
                      <p className='text-muted-foreground text-sm'>
                        {account.refresh_interval_minutes
                          ? `每 ${account.refresh_interval_minutes} 分钟刷新余额`
                          : '定时余额刷新已关闭'}
                      </p>
                      <div className='flex flex-wrap gap-2'>
                        {account.channel_ids.map((channelId) => (
                          <Badge key={channelId} variant='outline'>
                            {props.channels.find(
                              (channel) => channel.id === channelId
                            )?.name ?? `#${channelId}`}
                          </Badge>
                        ))}
                      </div>
                      <div className='flex flex-wrap gap-2'>
                        <Button
                          variant='outline'
                          disabled={busy || !account.channel_ids.length}
                          onClick={() => poll.mutate(account.id)}
                        >
                          刷新余额
                        </Button>
                        <Button
                          variant='outline'
                          disabled={busy || !account.channel_ids.length}
                          onClick={() => setConfiguring(account)}
                        >
                          编辑共享配置
                        </Button>
                        <Button
                          variant='outline'
                          disabled={busy}
                          onClick={() => setMerging(account)}
                        >
                          合并关联任务
                        </Button>
                        <Button
                          variant='outline'
                          disabled={busy}
                          onClick={() => setEditing({ account })}
                        >
                          管理关联
                        </Button>
                        {account.channel_ids.length === 0 ? (
                          <Button
                            variant='outline'
                            disabled={busy}
                            onClick={() => remove.mutate(account)}
                          >
                            删除空账户
                          </Button>
                        ) : null}
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
      {configuring && configuredChannel ? (
        <UpstreamConfigDialog
          channel={configuredChannel}
          account={configuring}
          open
          onOpenChange={(open) => {
            if (!open) {
              setConfiguring(null)
              refresh()
            }
          }}
        />
      ) : null}
    </>
  )
}
