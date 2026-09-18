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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { UpstreamConfigDialog } from './upstream-config-dialog'
import {
  upstreamEditorDialogClassName,
  upstreamEditorHeaderClassName,
} from './upstream-editor-layout'

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
  const [search, setSearch] = useState('')
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
  const channelNames = useMemo(
    () => new Map(props.channels.map((channel) => [channel.id, channel.name])),
    [props.channels]
  )
  const keyword = search.trim().toLowerCase()
  const filteredAccounts = accounts.filter(
    (account) =>
      `${account.name} ${account.upstream.base_url}`
        .toLowerCase()
        .includes(keyword) ||
      account.channel_ids.some((id) =>
        `${id} ${channelNames.get(id) ?? ''}`.toLowerCase().includes(keyword)
      )
  )
  const configuredChannel = props.channels.find((channel) =>
    configuring?.channel_ids.includes(channel.id)
  )
  return (
    <>
      <Dialog open onOpenChange={props.onOpenChange}>
        <DialogContent
          className={
            editing
              ? upstreamEditorDialogClassName
              : channelMonitorDialogContentClassName(
                  'flex h-[min(52rem,90dvh)] flex-col gap-3 sm:max-w-5xl'
                )
          }
        >
          <DialogHeader
            className={editing ? upstreamEditorHeaderClassName : 'pr-8'}
          >
            <DialogTitle>{editing ? '管理账户关联' : '上游账户'}</DialogTitle>
            <DialogDescription>
              {editing
                ? `${editing.account?.name || '新上游账户'} · 选择关联渠道，预览配置差异后确认。`
                : '同一余额池配置一次。渠道分别保留倍率、分组和请求统计。'}
            </DialogDescription>
          </DialogHeader>
          {!editing ? (
            <div className='flex flex-wrap items-center gap-2'>
              <Input
                type='search'
                aria-label='搜索上游账户'
                placeholder='搜索账户、地址或关联渠道'
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                className='min-w-0 flex-1 basis-56'
              />
              <span className='text-muted-foreground text-xs tabular-nums'>
                {keyword ? `${filteredAccounts.length} / ` : ''}
                {accounts.length} 个账户
              </span>
              <Button
                size='sm'
                disabled={busy || query.isPending || query.isError}
                onClick={() => setEditing({})}
              >
                从渠道创建账户
              </Button>
            </div>
          ) : null}
          <div
            className={
              editing
                ? 'flex min-h-0 flex-1 flex-col overflow-hidden'
                : 'min-h-0 flex-1 overflow-y-auto px-1'
            }
          >
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
              <div className='flex flex-col gap-2 pb-1'>
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
                {query.isSuccess &&
                accounts.length > 0 &&
                filteredAccounts.length === 0 ? (
                  <Empty>
                    <EmptyHeader>
                      <EmptyTitle>没有匹配的账户</EmptyTitle>
                      <EmptyDescription>
                        试试其他账户名称、地址或渠道。
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
                  aria-label='上游账户列表'
                  className='flex flex-col gap-2'
                >
                  {filteredAccounts.map((account) => (
                    <Collapsible
                      key={account.id}
                      render={<Card size='sm' role='listitem' />}
                    >
                      <CardHeader className='flex flex-col items-stretch justify-between gap-2 sm:flex-row sm:items-start'>
                        <div className='flex min-w-0 flex-1 flex-col gap-1'>
                          <CardTitle className='truncate' title={account.name}>
                            {account.name}
                          </CardTitle>
                          <p
                            className='text-muted-foreground truncate text-xs'
                            title={account.upstream.base_url}
                          >
                            {account.upstream.base_url}
                          </p>
                        </div>
                        <div className='min-w-0 text-sm'>
                          {account.balance === null ? (
                            <p
                              className='text-muted-foreground max-w-80 truncate text-xs'
                              title={account.last_balance_error}
                            >
                              余额待同步
                              {account.last_balance_error
                                ? `：${account.last_balance_error}`
                                : ''}
                            </p>
                          ) : (
                            <ChannelMonitorBalanceCell
                              balance={account.balance}
                              enabled={account.upstream.balance_sync_enabled}
                              warning={
                                account.upstream.balance_warning_threshold
                              }
                              error={account.last_balance_error}
                              estimate={account.balance_estimate}
                            />
                          )}
                        </div>
                      </CardHeader>
                      <CardContent>
                        <div className='flex flex-wrap items-center gap-x-3 gap-y-2'>
                          <p className='text-muted-foreground text-xs'>
                            {account.refresh_interval_minutes
                              ? `每 ${account.refresh_interval_minutes} 分钟刷新余额`
                              : '定时余额刷新已关闭'}
                          </p>
                          <CollapsibleTrigger
                            className='group'
                            render={<Button variant='ghost' size='sm' />}
                            disabled={account.channel_ids.length === 0}
                          >
                            关联渠道（{account.channel_ids.length}）
                            <HugeiconsIcon
                              icon={ArrowDown01Icon}
                              aria-hidden='true'
                              className='transition-transform group-aria-expanded:rotate-180'
                            />
                          </CollapsibleTrigger>
                          <div className='flex flex-wrap gap-1 sm:ml-auto'>
                            <Button
                              size='sm'
                              variant='outline'
                              disabled={busy || !account.channel_ids.length}
                              onClick={() => poll.mutate(account.id)}
                            >
                              刷新余额
                            </Button>
                            <Button
                              size='sm'
                              variant='outline'
                              disabled={busy || !account.channel_ids.length}
                              onClick={() => setConfiguring(account)}
                            >
                              编辑共享配置
                            </Button>
                            <Button
                              size='sm'
                              variant='outline'
                              disabled={busy}
                              onClick={() => setEditing({ account })}
                            >
                              管理关联
                            </Button>
                            {account.channel_ids.length === 0 ? (
                              <Button
                                size='sm'
                                variant='outline'
                                disabled={busy}
                                onClick={() => remove.mutate(account)}
                              >
                                删除空账户
                              </Button>
                            ) : null}
                          </div>
                        </div>
                        <CollapsibleContent>
                          <div className='flex flex-wrap gap-1.5 pt-3'>
                            {account.channel_ids.map((channelId) => (
                              <Badge
                                key={channelId}
                                variant='outline'
                                className='h-auto max-w-full break-all whitespace-normal'
                              >
                                {channelNames.get(channelId) ?? `#${channelId}`}
                              </Badge>
                            ))}
                          </div>
                        </CollapsibleContent>
                      </CardContent>
                    </Collapsible>
                  ))}
                </div>
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
