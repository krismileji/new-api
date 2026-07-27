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
  Refresh01Icon,
  Search01Icon,
  Settings02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'

import {
  clearChannelMonitorSmartScheduleStability,
  runChannelMonitorSmartSchedule,
  updateChannelMonitorSmartScheduleConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import type { ChannelMonitorItem } from '../types'
import { ChannelMonitorSmartScheduleCell } from './channel-monitor-smart-schedule-cell'

type ChannelMonitorSmartScheduleChannelPanelProps = {
  channels: ChannelMonitorItem[]
  enabled: boolean
  onOpenSettings: () => void
}

function channelScheduleStateBadge(channel: ChannelMonitorItem) {
  if (channel.status !== CHANNEL_STATUS.ENABLED) {
    return <Badge variant='destructive'>渠道禁用</Badge>
  }
  if (channel.smart_schedule_excluded) {
    return <Badge variant='outline'>未参与</Badge>
  }
  if (channel.smart_schedule_stability_state === 'degraded') {
    return <Badge variant='destructive'>低成功率</Badge>
  }
  if (channel.smart_schedule_stability_state === 'probing') {
    return <Badge variant='warning'>稳定性试放</Badge>
  }
  if (channel.last_schedule_status === 'failed') {
    return <Badge variant='destructive'>调度失败</Badge>
  }
  return <Badge variant='secondary'>参与调度</Badge>
}

function channelScheduleResult(channel: ChannelMonitorItem) {
  if (!channel.last_schedule_status) return '尚未执行'
  const labels = {
    succeeded: '执行成功',
    skipped: '已跳过',
    failed: '执行失败',
  }
  return labels[channel.last_schedule_status] ?? channel.last_schedule_status
}

export function ChannelMonitorSmartScheduleChannelPanel(
  props: ChannelMonitorSmartScheduleChannelPanelProps
) {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const invalidateSchedule = () => {
    queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    queryClient.invalidateQueries({ queryKey: ['channels'] })
  }
  const updateMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => toast.success('渠道调度设置已保存'),
    onSettled: invalidateSchedule,
  })
  const clearMutation = useMutation({
    mutationFn: clearChannelMonitorSmartScheduleStability,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.cleared
          ? `已解除稳定性保护，恢复优先级 ${response.data.priority}、权重 ${response.data.weight}`
          : '当前渠道没有需要解除的稳定性保护'
      )
    },
    onSettled: invalidateSchedule,
  })
  const runMutation = useMutation({
    mutationFn: runChannelMonitorSmartSchedule,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        response.data.created
          ? '智能调度任务已创建'
          : '已有智能调度任务正在运行'
      )
    },
    onSettled: invalidateSchedule,
  })

  const normalizedSearch = search.trim().toLocaleLowerCase()
  const filteredChannels = useMemo(() => {
    if (!normalizedSearch) return props.channels
    return props.channels.filter(
      (channel) =>
        channel.name.toLocaleLowerCase().includes(normalizedSearch) ||
        String(channel.id).includes(normalizedSearch) ||
        channel.groups.some((group) =>
          group.toLocaleLowerCase().includes(normalizedSearch)
        )
    )
  }, [normalizedSearch, props.channels])
  const participatingCount = props.channels.filter(
    (channel) =>
      !channel.smart_schedule_excluded &&
      channel.status === CHANNEL_STATUS.ENABLED
  ).length
  const degradedCount = props.channels.filter(
    (channel) => channel.smart_schedule_stability_state === 'degraded'
  ).length
  const probingCount = props.channels.filter(
    (channel) => channel.smart_schedule_stability_state === 'probing'
  ).length
  const excludedCount = props.channels.filter(
    (channel) => channel.smart_schedule_excluded
  ).length

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border bg-muted/30 grid grid-cols-2 border-y px-3 py-3 sm:grid-cols-4 lg:grid-cols-[repeat(4,minmax(0,1fr))_auto] lg:items-center'>
        <div className='flex flex-col gap-0.5 py-1'>
          <span className='text-muted-foreground text-xs'>参与渠道</span>
          <span className='font-mono text-lg font-semibold'>
            {participatingCount}
          </span>
        </div>
        <div className='flex flex-col gap-0.5 py-1'>
          <span className='text-muted-foreground text-xs'>低成功率</span>
          <span className='font-mono text-lg font-semibold'>
            {degradedCount}
          </span>
        </div>
        <div className='flex flex-col gap-0.5 py-1'>
          <span className='text-muted-foreground text-xs'>稳定性试放</span>
          <span className='font-mono text-lg font-semibold'>
            {probingCount}
          </span>
        </div>
        <div className='flex flex-col gap-0.5 py-1'>
          <span className='text-muted-foreground text-xs'>未参与</span>
          <span className='font-mono text-lg font-semibold'>
            {excludedCount}
          </span>
        </div>
        <div className='col-span-2 mt-2 flex flex-wrap items-center gap-2 sm:col-span-4 lg:col-span-1 lg:mt-0 lg:justify-end'>
          <Badge variant={props.enabled ? 'secondary' : 'outline'}>
            {props.enabled ? '调度已启用' : '调度已禁用'}
          </Badge>
          <Badge variant='outline'>按渠道兼容模式</Badge>
        </div>
      </div>

      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <InputGroup className='w-full sm:max-w-sm'>
          <InputGroupAddon>
            <HugeiconsIcon icon={Search01Icon} />
          </InputGroupAddon>
          <InputGroupInput
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder='渠道或分组'
            aria-label='搜索智能调度渠道'
          />
        </InputGroup>
        <div className='flex shrink-0 gap-2'>
          <Button
            type='button'
            variant='outline'
            onClick={props.onOpenSettings}
          >
            <HugeiconsIcon icon={Settings02Icon} data-icon='inline-start' />
            调度设置
          </Button>
          <Button
            type='button'
            onClick={() => runMutation.mutate()}
            disabled={!props.enabled || runMutation.isPending}
          >
            {runMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon icon={Refresh01Icon} data-icon='inline-start' />
            )}
            立即调度
          </Button>
        </div>
      </div>

      {filteredChannels.length === 0 ? (
        <Empty className='min-h-72'>
          <EmptyHeader>
            <EmptyTitle>当前筛选下没有调度渠道</EmptyTitle>
            <EmptyDescription>调整搜索条件</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='w-max min-w-full table-auto [&_td]:py-3 [&_td]:align-top'>
            <TableHeader>
              <TableRow>
                <TableHead>渠道</TableHead>
                <TableHead>关联分组</TableHead>
                <TableHead>最近调度</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>调度配置</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredChannels.map((channel) => (
                <TableRow key={channel.id}>
                  <TableCell>
                    <div className='flex min-w-[140px] flex-col gap-0.5'>
                      <span className='font-medium'>{channel.name}</span>
                      <span className='text-muted-foreground text-xs'>
                        ID {channel.id}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className='max-w-72 whitespace-normal'>
                    <div className='flex flex-wrap gap-1'>
                      {channel.groups.map((group) => (
                        <Badge key={group} variant='outline'>
                          {group}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className='max-w-80 whitespace-normal'>
                    <span className='font-medium'>
                      {channelScheduleResult(channel)}
                    </span>
                    {channel.last_schedule_time > 0 ? (
                      <span className='text-muted-foreground mt-0.5 block text-xs'>
                        {formatTimestampToDate(channel.last_schedule_time)}
                      </span>
                    ) : null}
                    {channel.last_schedule_error ? (
                      <span
                        className='text-muted-foreground mt-1 block text-xs'
                        title={channel.last_schedule_error}
                      >
                        {channel.last_schedule_error}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell>{channelScheduleStateBadge(channel)}</TableCell>
                  <TableCell className='min-w-[260px] whitespace-normal'>
                    <ChannelMonitorSmartScheduleCell
                      channel={channel}
                      pending={
                        updateMutation.isPending &&
                        updateMutation.variables?.channelId === channel.id
                      }
                      clearPending={
                        clearMutation.isPending &&
                        clearMutation.variables === channel.id
                      }
                      onUpdate={(excluded) =>
                        updateMutation.mutate({
                          channelId: channel.id,
                          excluded,
                        })
                      }
                      onClearStability={() => clearMutation.mutate(channel.id)}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
