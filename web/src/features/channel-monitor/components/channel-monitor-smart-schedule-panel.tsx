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
import { Refresh01Icon, Settings02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
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
  clearChannelMonitorSmartScheduleRouteStability,
  runChannelMonitorSmartSchedule,
  updateChannelMonitorSmartScheduleRouteConfig,
} from '../api'
import { handleChannelMonitorMutationError } from '../lib/error'
import {
  CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
  getChannelMonitorSmartScheduleQueryOptions,
} from '../lib/query-options'
import { channelMonitorSmartScheduleRouteKey } from '../lib/smart-schedule-summary'
import type {
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorSmartScheduleRoutePerformance,
  ChannelMonitorSmartScheduleRouteStability,
} from '../types'
import { ChannelMonitorSmartScheduleRouteState } from './channel-monitor-smart-schedule-route-state'

type ChannelMonitorSmartSchedulePanelProps = {
  active: boolean
  onOpenSettings: () => void
}

const ALL_FILTER_VALUE = '__all__'

function formatPerformance(
  metric: ChannelMonitorSmartScheduleRoutePerformance | undefined
) {
  if (!metric) return '-'
  const values: string[] = []
  if (metric.average_first_token_ms != null) {
    values.push(`首字 ${Math.round(metric.average_first_token_ms)} ms`)
  }
  if (metric.average_tps != null) {
    values.push(`TPS ${metric.average_tps.toFixed(1)}`)
  }
  return values.length > 0 ? values.join(' · ') : '-'
}

function formatSuccess(
  metric: ChannelMonitorSmartScheduleRouteStability | undefined,
  available: boolean
) {
  if (!available) return '未启用统计'
  if (!metric || metric.sample_count === 0) return '无样本'
  return `${(metric.success_rate * 100).toFixed(1)}% · ${metric.sample_count} 次`
}

export function ChannelMonitorSmartSchedulePanel(
  props: ChannelMonitorSmartSchedulePanelProps
) {
  const queryClient = useQueryClient()
  const [groupFilter, setGroupFilter] = useState(ALL_FILTER_VALUE)
  const [modelFilter, setModelFilter] = useState(ALL_FILTER_VALUE)
  const [search, setSearch] = useState('')
  const [clearTarget, setClearTarget] =
    useState<ChannelMonitorSmartScheduleRoute | null>(null)
  const query = useQuery({
    ...getChannelMonitorSmartScheduleQueryOptions(),
    enabled: props.active,
  })
  const invalidateSchedule = () => {
    queryClient.invalidateQueries({
      queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
    })
    queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
  }
  const updateMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleRouteConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: () => toast.success('路由调度设置已保存'),
    onSettled: invalidateSchedule,
  })
  const clearMutation = useMutation({
    mutationFn: clearChannelMonitorSmartScheduleRouteStability,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      setClearTarget(null)
      toast.success(
        response.data.cleared
          ? `已解除稳定性保护，恢复优先级 ${response.data.priority}、权重 ${response.data.weight}`
          : '当前路由没有需要解除的稳定性保护'
      )
    },
    onSettled: () => {
      invalidateSchedule()
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
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

  const data = query.data?.data
  const routes = useMemo(() => data?.routes ?? [], [data?.routes])
  const groups = useMemo(
    () => [...new Set(routes.map((route) => route.group))].sort(),
    [routes]
  )
  const models = useMemo(
    () => [...new Set(routes.map((route) => route.model))].sort(),
    [routes]
  )
  const performanceByRoute = useMemo(
    () =>
      new Map(
        (data?.performance_items ?? []).map((metric) => [
          channelMonitorSmartScheduleRouteKey(metric),
          metric,
        ])
      ),
    [data?.performance_items]
  )
  const stabilityByRoute = useMemo(
    () =>
      new Map(
        (data?.stability_items ?? []).map((metric) => [
          channelMonitorSmartScheduleRouteKey(metric),
          metric,
        ])
      ),
    [data?.stability_items]
  )
  const normalizedSearch = search.trim().toLocaleLowerCase()
  const filteredRoutes = useMemo(
    () =>
      routes.filter((route) => {
        if (groupFilter !== ALL_FILTER_VALUE && route.group !== groupFilter) {
          return false
        }
        if (modelFilter !== ALL_FILTER_VALUE && route.model !== modelFilter) {
          return false
        }
        if (!normalizedSearch) return true
        return (
          route.channel_name.toLocaleLowerCase().includes(normalizedSearch) ||
          String(route.channel_id).includes(normalizedSearch) ||
          route.group.toLocaleLowerCase().includes(normalizedSearch) ||
          route.model.toLocaleLowerCase().includes(normalizedSearch)
        )
      }),
    [groupFilter, modelFilter, normalizedSearch, routes]
  )
  const participatingCount = routes.filter(
    (route) =>
      !route.state.excluded &&
      route.enabled &&
      route.channel_status === CHANNEL_STATUS.ENABLED
  ).length
  const degradedCount = routes.filter(
    (route) => route.state.stability_state === 'degraded'
  ).length
  const probingCount = routes.filter(
    (route) => route.state.stability_state === 'probing'
  ).length
  const excludedCount = routes.filter((route) => route.state.excluded).length

  if (query.isLoading) {
    return (
      <div className='flex flex-col gap-3'>
        <Skeleton className='h-10 w-full' />
        <Skeleton className='h-96 w-full' />
      </div>
    )
  }
  if (query.isError) {
    return (
      <Empty className='min-h-72'>
        <EmptyHeader>
          <EmptyTitle>智能调度加载失败</EmptyTitle>
          <EmptyDescription>请刷新后重试</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border bg-muted/30 grid grid-cols-2 border-y px-3 py-3 sm:grid-cols-4 lg:grid-cols-[repeat(4,minmax(0,1fr))_auto] lg:items-center'>
        <div className='flex flex-col gap-0.5 py-1'>
          <span className='text-muted-foreground text-xs'>参与路由</span>
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
          <Badge variant={data?.enabled ? 'secondary' : 'outline'}>
            {data?.enabled ? '调度已启用' : '调度已禁用'}
          </Badge>
          <Badge variant='outline'>按分组和模型</Badge>
        </div>
      </div>

      <div className='flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between'>
        <div className='grid min-w-0 gap-2 sm:grid-cols-3 lg:max-w-3xl lg:flex-1'>
          <Select
            items={[
              { value: ALL_FILTER_VALUE, label: '全部分组' },
              ...groups.map((group) => ({ value: group, label: group })),
            ]}
            value={groupFilter}
            onValueChange={(value) => value !== null && setGroupFilter(value)}
          >
            <SelectTrigger className='w-full' aria-label='筛选调度分组'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={ALL_FILTER_VALUE}>全部分组</SelectItem>
                {groups.map((group) => (
                  <SelectItem key={group} value={group}>
                    {group}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            items={[
              { value: ALL_FILTER_VALUE, label: '全部模型' },
              ...models.map((model) => ({ value: model, label: model })),
            ]}
            value={modelFilter}
            onValueChange={(value) => value !== null && setModelFilter(value)}
          >
            <SelectTrigger className='w-full' aria-label='筛选调度模型'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={ALL_FILTER_VALUE}>全部模型</SelectItem>
                {models.map((model) => (
                  <SelectItem key={model} value={model}>
                    {model}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <InputGroup>
            <InputGroupAddon>搜索</InputGroupAddon>
            <InputGroupInput
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder='渠道、分组或模型'
              aria-label='搜索智能调度路由'
            />
          </InputGroup>
        </div>
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
            disabled={!data?.enabled || runMutation.isPending}
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

      {filteredRoutes.length === 0 ? (
        <Empty className='min-h-72'>
          <EmptyHeader>
            <EmptyTitle>当前筛选下没有调度路由</EmptyTitle>
            <EmptyDescription>调整分组、模型或搜索条件</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='w-max min-w-full table-auto [&_td]:py-3 [&_td]:align-top'>
            <TableHeader>
              <TableRow>
                <TableHead>渠道</TableHead>
                <TableHead>分组</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>实际优先级 / 权重</TableHead>
                <TableHead>调度得分</TableHead>
                <TableHead>性能（近 {data?.range_minutes} 分钟）</TableHead>
                <TableHead>成功率</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>参与调度</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredRoutes.map((route) => {
                const key = channelMonitorSmartScheduleRouteKey(route)
                const updatePending =
                  updateMutation.isPending &&
                  updateMutation.variables != null &&
                  channelMonitorSmartScheduleRouteKey({
                    channel_id: updateMutation.variables.channelId,
                    group: updateMutation.variables.group,
                    model: updateMutation.variables.model,
                  }) === key
                return (
                  <TableRow key={key}>
                    <TableCell>
                      <div className='flex min-w-[140px] flex-col gap-0.5'>
                        <span className='font-medium'>
                          {route.channel_name}
                        </span>
                        <span className='text-muted-foreground text-xs'>
                          ID {route.channel_id}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>{route.group}</TableCell>
                    <TableCell className='max-w-72 whitespace-normal'>
                      <span title={route.model}>{route.model}</span>
                    </TableCell>
                    <TableCell>
                      <span className='font-mono font-medium'>
                        P{route.priority} / W{route.weight}
                      </span>
                      <span className='text-muted-foreground mt-0.5 block text-xs'>
                        渠道默认 P{route.channel_priority} / W
                        {route.channel_weight}
                      </span>
                    </TableCell>
                    <TableCell>
                      {route.state.last_schedule_score == null ? (
                        <span className='text-muted-foreground'>-</span>
                      ) : (
                        <span className='font-mono font-medium'>
                          {(route.state.last_schedule_score * 100).toFixed(1)}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      {formatPerformance(performanceByRoute.get(key))}
                    </TableCell>
                    <TableCell>
                      {formatSuccess(
                        stabilityByRoute.get(key),
                        data?.stability_metrics_available ?? false
                      )}
                    </TableCell>
                    <TableCell>
                      <div
                        className='flex min-w-[128px] flex-col items-start gap-1'
                        title={route.state.last_schedule_error || undefined}
                      >
                        <ChannelMonitorSmartScheduleRouteState
                          route={route}
                          onProtectedStatusClick={() => setClearTarget(route)}
                        />
                        {route.state.stability_until > 0 &&
                          route.state.stability_state === 'degraded' && (
                            <span className='text-muted-foreground text-xs'>
                              至{' '}
                              {formatTimestampToDate(
                                route.state.stability_until
                              )}
                            </span>
                          )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-2'>
                        {updatePending && <Spinner className='size-4' />}
                        <Switch
                          checked={!route.state.excluded}
                          disabled={updateMutation.isPending}
                          onCheckedChange={(checked) =>
                            updateMutation.mutate({
                              channelId: route.channel_id,
                              group: route.group,
                              model: route.model,
                              excluded: !checked,
                            })
                          }
                          aria-label={`${route.channel_name} ${route.group} ${route.model} 参与智能调度`}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <AlertDialog
        open={clearTarget != null}
        onOpenChange={(open) => {
          if (!open) setClearTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认手动解除稳定性保护？</AlertDialogTitle>
            <AlertDialogDescription>
              将立即清除“{clearTarget?.channel_name} / {clearTarget?.group} /{' '}
              {clearTarget?.model}”的
              {clearTarget?.state.stability_state === 'degraded'
                ? '低成功率降级'
                : '稳定性试放'}
              状态，并恢复进入保护前保存的优先级和完整权重。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearMutation.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={clearMutation.isPending || clearTarget == null}
              onClick={() => {
                if (!clearTarget) return
                clearMutation.mutate({
                  channelId: clearTarget.channel_id,
                  group: clearTarget.group,
                  model: clearTarget.model,
                })
              }}
            >
              {clearMutation.isPending ? '解除中...' : '确认解除'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
