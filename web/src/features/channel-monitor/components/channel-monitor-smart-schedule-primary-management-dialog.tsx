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
import { Cancel01Icon, PinIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
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
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'

import {
  getChannelMonitorSmartSchedulePrimaryManagement,
  updateChannelMonitorSmartScheduleChannelPrimaryRule,
  updateChannelMonitorSmartScheduleRoutePrimary,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  channelMonitorSmartScheduleUntilIsPermanent,
  formatChannelMonitorSmartScheduleUntil,
} from '../lib/smart-schedule-display'
import type {
  ChannelMonitorSmartScheduleChannelPrimaryRule,
  ChannelMonitorSmartScheduleManualPrimary,
  ChannelMonitorSmartSchedulePrimaryRuleRoute,
} from '../types'

type Removal =
  | { kind: 'channel'; rule: ChannelMonitorSmartScheduleChannelPrimaryRule }
  | {
      kind: 'channel-update'
      rule: ChannelMonitorSmartScheduleChannelPrimaryRule
      durationMinutes: number | null
      allowStabilityDegrade: boolean
    }
  | { kind: 'route'; primary: ChannelMonitorSmartScheduleManualPrimary }

type ChannelMonitorSmartSchedulePrimaryManagementDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onActionComplete: () => Promise<void>
}

function routeStatusLabel(route: ChannelMonitorSmartSchedulePrimaryRuleRoute) {
  switch (route.status) {
    case 'active':
      return '已生效'
    case 'not_eligible':
      return '未参与或已禁用'
    case 'single_model_override':
      return `单模型固定至 ${route.current_primary_channel_name}`
    case 'channel_rule_conflict':
      return `优先应用 ${route.current_primary_channel_name} 的更新规则`
    case 'stability_protected':
      return '稳定性保护中，等待确认'
    default:
      return '等待路由参与调度'
  }
}

function FixedUntil(props: { until: number }) {
  if (channelMonitorSmartScheduleUntilIsPermanent(props.until)) {
    return <span>永久</span>
  }

  const date = new Date(props.until * 1000)
  if (!Number.isFinite(props.until) || Number.isNaN(date.getTime())) {
    return <span>未知</span>
  }

  return (
    <time dateTime={date.toISOString()}>
      {formatChannelMonitorSmartScheduleUntil(props.until)}
    </time>
  )
}

function defaultDurationInput(until: number) {
  if (channelMonitorSmartScheduleUntilIsPermanent(until)) return ''
  return String(Math.max(1, Math.ceil((until - Date.now() / 1000) / 60)))
}

export function ChannelMonitorSmartSchedulePrimaryManagementDialog(
  props: ChannelMonitorSmartSchedulePrimaryManagementDialogProps
) {
  const queryClient = useQueryClient()
  const [durationByChannel, setDurationByChannel] = useState<
    Record<number, string>
  >({})
  const [degradeByChannel, setDegradeByChannel] = useState<
    Record<number, boolean>
  >({})
  const [search, setSearch] = useState('')
  const query = useQuery({
    queryKey: ['channel-monitor', 'smart-schedule', 'primary-management'],
    queryFn: getChannelMonitorSmartSchedulePrimaryManagement,
    enabled: props.open,
    staleTime: 0,
  })
  const mutation = useMutation({
    mutationFn: async (removal: Removal) => {
      if (removal.kind === 'channel') {
        await updateChannelMonitorSmartScheduleChannelPrimaryRule({
          channelId: removal.rule.channel_id,
          durationMinutes: 0,
          allowStabilityDegrade: false,
        })
        return
      }
      if (removal.kind === 'channel-update') {
        await updateChannelMonitorSmartScheduleChannelPrimaryRule({
          channelId: removal.rule.channel_id,
          durationMinutes: removal.durationMinutes,
          allowStabilityDegrade: removal.allowStabilityDegrade,
        })
        return
      }
      await updateChannelMonitorSmartScheduleRoutePrimary({
        channelId: removal.primary.channel_id,
        group: removal.primary.group,
        model: removal.primary.model,
        durationMinutes: 0,
        allowStabilityDegrade: false,
      })
    },
    onError: handleChannelMonitorMutationError,
    onSuccess: async (_, removal) => {
      let successMessage = '已解除渠道全部模型固定'
      if (removal.kind === 'route') {
        successMessage = '已解除单模型固定'
      } else if (removal.kind === 'channel-update') {
        successMessage = '渠道固定规则已更新'
      }
      toast.success(successMessage)
      await queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'smart-schedule', 'primary-management'],
      })
      await props.onActionComplete()
    },
  })
  const management = query.data?.data
  const channelRules = management?.channel_rules ?? []
  const routePrimaries = management?.route_primaries ?? []
  const hasEntries = channelRules.length > 0 || routePrimaries.length > 0
  const normalizedSearch = search.trim().toLowerCase()
  const filteredChannelRules = channelRules.filter((rule) =>
    [
      rule.channel_name,
      ...rule.routes.flatMap((route) => [
        route.group,
        route.model,
        routeStatusLabel(route),
      ]),
    ].some((value) => value.toLowerCase().includes(normalizedSearch))
  )
  const filteredRoutePrimaries = routePrimaries.filter((primary) =>
    [primary.channel_name, primary.group, primary.model].some((value) =>
      value.toLowerCase().includes(normalizedSearch)
    )
  )
  const hasFilteredEntries =
    filteredChannelRules.length > 0 || filteredRoutePrimaries.length > 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && mutation.isPending) return
        props.onOpenChange(open)
      }}
    >
      <DialogContent className='flex max-h-[88vh] flex-col overflow-hidden sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>固定项管理</DialogTitle>
          <DialogDescription>
            查看渠道规则覆盖的模型路由，以及单独固定的模型路由。
          </DialogDescription>
        </DialogHeader>
        {query.isLoading ? (
          <div className='text-muted-foreground flex min-h-40 items-center justify-center gap-2 text-sm'>
            <Spinner aria-hidden='true' />
            正在加载固定项
          </div>
        ) : null}
        {query.isError ? (
          <Empty className='min-h-40'>
            <EmptyHeader>
              <EmptyTitle>固定项加载失败</EmptyTitle>
              <EmptyDescription>请重试或刷新页面</EmptyDescription>
            </EmptyHeader>
            <Button
              type='button'
              variant='outline'
              onClick={() => void query.refetch()}
            >
              重试
            </Button>
          </Empty>
        ) : null}
        {!query.isLoading && !query.isError && !hasEntries ? (
          <Empty className='min-h-40'>
            <EmptyHeader>
              <EmptyTitle>暂无固定项</EmptyTitle>
              <EmptyDescription>
                固定的渠道规则和模型路由会显示在这里
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : null}
        {!query.isLoading && !query.isError && hasEntries ? (
          <>
            <Input
              type='search'
              aria-label='搜索固定项'
              placeholder='搜索渠道、分组或模型'
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
            {!hasFilteredEntries ? (
              <Empty className='min-h-32'>
                <EmptyHeader>
                  <EmptyTitle>没有匹配的固定项</EmptyTitle>
                  <EmptyDescription>调整搜索内容后重试</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ScrollArea className='min-h-0 flex-1 border-y'>
                <div className='divide-y'>
                  {filteredChannelRules.length > 0 ? (
                    <section aria-labelledby='channel-primary-rules-title'>
                      <h3
                        id='channel-primary-rules-title'
                        className='bg-muted/30 sticky top-0 z-10 px-3 py-2 text-xs font-semibold'
                      >
                        渠道固定规则（{filteredChannelRules.length}）
                      </h3>
                      {filteredChannelRules.map((rule) => {
                        const activeRoutes = rule.routes.filter(
                          (route) => route.status === 'active'
                        ).length
                        const durationValue =
                          durationByChannel[rule.channel_id] ??
                          defaultDurationInput(rule.manual_primary_until)
                        const allowDegrade =
                          degradeByChannel[rule.channel_id] ??
                          rule.allow_stability_degrade
                        const parsedDuration =
                          durationValue.trim() === ''
                            ? null
                            : Number(durationValue)
                        const validDuration =
                          parsedDuration === null ||
                          (Number.isInteger(parsedDuration) &&
                            parsedDuration >= 1 &&
                            parsedDuration <= 525600)
                        return (
                          <article key={rule.channel_id} className='px-3 py-3'>
                            <div className='flex flex-wrap items-start justify-between gap-3'>
                              <div className='min-w-0'>
                                <div className='flex items-center gap-2 text-sm font-medium'>
                                  <HugeiconsIcon
                                    icon={PinIcon}
                                    className='text-primary size-4 shrink-0'
                                    aria-hidden='true'
                                  />
                                  <span className='break-words'>
                                    {rule.channel_name}
                                  </span>
                                </div>
                                <div className='text-muted-foreground mt-1 text-xs'>
                                  固定至{' '}
                                  <FixedUntil
                                    until={rule.manual_primary_until}
                                  />
                                  <span className='mx-1'>·</span>
                                  {activeRoutes}/{rule.routes.length}{' '}
                                  个路由已生效
                                </div>
                              </div>
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                disabled={mutation.isPending}
                                onClick={() =>
                                  mutation.mutate({ kind: 'channel', rule })
                                }
                              >
                                {mutation.isPending &&
                                mutation.variables?.kind === 'channel' &&
                                mutation.variables.rule.channel_id ===
                                  rule.channel_id ? (
                                  <Spinner data-icon='inline-start' />
                                ) : (
                                  <HugeiconsIcon
                                    icon={Cancel01Icon}
                                    data-icon='inline-start'
                                  />
                                )}
                                解除规则
                              </Button>
                            </div>
                            <div className='mt-3 flex flex-wrap items-end gap-3'>
                              <label className='grid gap-1 text-xs'>
                                <span className='text-muted-foreground'>
                                  固定时长（分钟，留空永久）
                                </span>
                                <Input
                                  type='number'
                                  min={1}
                                  max={525600}
                                  step={1}
                                  value={durationValue}
                                  aria-label={`${rule.channel_name} 固定时长（分钟）`}
                                  onChange={(event) =>
                                    setDurationByChannel((current) => ({
                                      ...current,
                                      [rule.channel_id]: event.target.value,
                                    }))
                                  }
                                />
                              </label>
                              <label className='flex min-h-9 items-center gap-2 text-xs'>
                                <Checkbox
                                  checked={allowDegrade}
                                  aria-label={`${rule.channel_name} 允许稳定性降级`}
                                  onCheckedChange={(checked) =>
                                    setDegradeByChannel((current) => ({
                                      ...current,
                                      [rule.channel_id]: checked,
                                    }))
                                  }
                                />
                                允许稳定性降级
                              </label>
                              <Button
                                type='button'
                                variant='outline'
                                size='sm'
                                disabled={mutation.isPending || !validDuration}
                                onClick={() =>
                                  mutation.mutate({
                                    kind: 'channel-update',
                                    rule,
                                    durationMinutes: parsedDuration,
                                    allowStabilityDegrade: allowDegrade,
                                  })
                                }
                              >
                                {mutation.isPending &&
                                mutation.variables?.kind === 'channel-update' &&
                                mutation.variables.rule.channel_id ===
                                  rule.channel_id ? (
                                  <Spinner data-icon='inline-start' />
                                ) : null}
                                更新规则
                              </Button>
                            </div>
                            {rule.routes.length > 0 ? (
                              <ul className='mt-2 grid gap-1 pl-6 text-xs sm:grid-cols-2'>
                                {rule.routes.map((route) => (
                                  <li
                                    key={`${rule.channel_id}:${route.group}:${route.model}`}
                                    className='min-w-0 break-words'
                                  >
                                    <span className='font-medium'>
                                      {route.group}
                                    </span>
                                    <span className='text-muted-foreground'>
                                      {' '}
                                      / {route.model} ·{' '}
                                    </span>
                                    <span className='text-muted-foreground'>
                                      {routeStatusLabel(route)}
                                    </span>
                                  </li>
                                ))}
                              </ul>
                            ) : (
                              <p className='text-muted-foreground mt-2 pl-6 text-xs'>
                                当前没有模型路由；以后新增并参与调度的模型会自动纳入规则。
                              </p>
                            )}
                          </article>
                        )
                      })}
                    </section>
                  ) : null}
                  {filteredRoutePrimaries.length > 0 ? (
                    <section aria-labelledby='route-primary-title'>
                      <h3
                        id='route-primary-title'
                        className='bg-muted/30 sticky top-0 z-10 px-3 py-2 text-xs font-semibold'
                      >
                        单模型固定（{filteredRoutePrimaries.length}）
                      </h3>
                      {filteredRoutePrimaries.map((primary) => (
                        <article
                          key={`${primary.channel_id}:${primary.group}:${primary.model}`}
                          className='flex flex-wrap items-center justify-between gap-3 px-3 py-3'
                        >
                          <div className='min-w-0'>
                            <div className='text-sm font-medium break-words'>
                              {primary.channel_name}
                            </div>
                            <div className='text-muted-foreground mt-1 text-xs break-words'>
                              {primary.group} / {primary.model}
                              <span className='mx-1'>·</span>
                              固定至{' '}
                              <FixedUntil
                                until={primary.manual_primary_until}
                              />
                            </div>
                          </div>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            disabled={mutation.isPending}
                            onClick={() =>
                              mutation.mutate({ kind: 'route', primary })
                            }
                          >
                            {mutation.isPending &&
                            mutation.variables?.kind === 'route' &&
                            mutation.variables.primary.channel_id ===
                              primary.channel_id &&
                            mutation.variables.primary.group ===
                              primary.group &&
                            mutation.variables.primary.model ===
                              primary.model ? (
                              <Spinner data-icon='inline-start' />
                            ) : (
                              <HugeiconsIcon
                                icon={Cancel01Icon}
                                data-icon='inline-start'
                              />
                            )}
                            解除固定
                          </Button>
                        </article>
                      ))}
                    </section>
                  ) : null}
                </div>
              </ScrollArea>
            )}
          </>
        ) : null}
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
