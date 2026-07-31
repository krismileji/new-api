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
  Analytics01Icon,
  ArrangeIcon,
  ChartLineData01Icon,
  HistoryIcon,
  Layers01Icon,
  MoneyBag02Icon,
  Refresh01Icon,
  Route01Icon,
  Search01Icon,
  Settings02Icon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { lazy, Suspense, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ChannelTestDialogForChannel } from '@/features/channels/components/dialogs/channel-test-dialog'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { cn } from '@/lib/utils'

import {
  fetchChannelMonitorUpstreamBalance,
  fetchChannelMonitorUpstreamRatio,
  getChannelMonitorCostOverview,
  getChannelMonitorTodaySuccess,
  updateChannelMonitorSmartScheduleChannelConfig,
  updateMonitoredChannelStatus,
} from './api'
import { ChannelMonitorChannelView } from './components/channel-monitor-channel-view'
import { ChannelMonitorGroupView } from './components/channel-monitor-group-view'
import { ChannelMonitorModelPerformanceView } from './components/channel-monitor-model-performance-view'
import { ChannelMonitorOrderDialog } from './components/channel-monitor-order-dialog'
import {
  ChannelMonitorSettingsDialog,
  ChannelMonitorSmartScheduleSettingsSheet,
} from './components/channel-monitor-settings-dialog'
import { ChannelMonitorSmartScheduleBoard } from './components/channel-monitor-smart-schedule-board'
import { ChannelMonitorSuccessDetailDialog } from './components/channel-monitor-success-detail-dialog'
import { ChannelMonitorTaskHistoryDialog } from './components/channel-monitor-task-history-dialog'
import { ChannelMonitorTodaySuccessCard } from './components/channel-monitor-today-success-card'
import { ChannelRatioHistoryDialog } from './components/channel-ratio-history-dialog'
import { EditChannelConcurrencyLimitDialog } from './components/edit-channel-concurrency-limit-dialog'
import { EditChannelGroupsDialog } from './components/edit-channel-groups-dialog'
import { EditGroupChannelsDialog } from './components/edit-group-channels-dialog'
import { EditGroupRatioDialog } from './components/edit-group-ratio-dialog'
import { SyncGroupRatioDialog } from './components/sync-group-ratio-dialog'
import { UpstreamConfigDialog } from './components/upstream-config-dialog'
import { handleChannelMonitorMutationError } from './lib/error'
import { formatChannelMonitorCost, formatMonitorRatio } from './lib/format'
import {
  getChannelMonitorOverviewQueryOptions,
  getChannelMonitorPerformanceQueryOptions,
  getChannelMonitorSmartScheduleQueryOptions,
} from './lib/query-options'
import {
  DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
} from './lib/schema'
import { getChannelMonitorSmartScheduleModelOptionsByGroup } from './lib/smart-schedule-model-order'
import {
  groupChannelMonitorSmartScheduleRoutesByChannel,
  filterChannelMonitorSmartScheduleRoutes,
  getChannelMonitorSmartScheduleDisplayOptions,
  summarizeChannelMonitorSmartScheduleOverview,
} from './lib/smart-schedule-summary'
import { sortChannelMonitorItems } from './lib/sort'
import type {
  ChannelMonitorChannelPerformance,
  ChannelMonitorItem,
  ChannelMonitorPerformanceMetric,
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorSettings,
  ChannelMonitorSmartScheduleRoute,
  ChannelMonitorTaskKind,
  ChannelMonitorSortMode,
  ChannelMonitorGroupSuccessMetric,
  ChannelMonitorSuccessDetailTarget,
  ChannelMonitorSuccessMetric,
  ChannelMonitorSuccessSummary,
  ChannelMonitorUpstreamType,
  GroupMonitorItem,
} from './types'

const LazyChannelMonitorCostHistoryDialog = lazy(() =>
  import('./components/channel-monitor-cost-history-dialog').then((module) => ({
    default: module.ChannelMonitorCostHistoryDialog,
  }))
)
const LazyChannelMonitorTodaySuccessDialog = lazy(() =>
  import('./components/channel-monitor-today-success-dialog').then(
    (module) => ({
      default: module.ChannelMonitorTodaySuccessDialog,
    })
  )
)
const LazyChannelBatchTestDialog = lazy(() =>
  import('@/features/channels/components/dialogs/channel-batch-test-dialog').then(
    (module) => ({ default: module.ChannelBatchTestDialog })
  )
)
const loadChannelMonitorSmartScheduleExecutionDialog = () =>
  import('./components/channel-monitor-smart-schedule-execution-dialog').then(
    (module) => ({
      default: module.ChannelMonitorSmartScheduleExecutionDialog,
    })
  )
const LazyChannelMonitorSmartScheduleExecutionDialog = lazy(
  loadChannelMonitorSmartScheduleExecutionDialog
)
type MonitorView = 'channels' | 'groups' | 'models' | 'smart-schedule'
type ChannelUpstreamFilter = 'all' | ChannelMonitorUpstreamType
type ChannelDialogType =
  | 'concurrency'
  | 'groups'
  | 'upstream'
  | 'history'
  | 'connection_test'
type ChannelDialogState = {
  channelId: number
  type: ChannelDialogType
}

const EMPTY_CHANNELS: ChannelMonitorItem[] = []
const EMPTY_CHANNEL_ORDER: number[] = []
const EMPTY_GROUP_RATIOS: Record<string, number> = {}
const EMPTY_GROUP_COEFFICIENTS: Record<string, number> = {}
const EMPTY_PERFORMANCE_METRICS: ChannelMonitorPerformanceMetric[] = []
const EMPTY_SUCCESS_METRICS: ChannelMonitorSuccessMetric[] = []
const EMPTY_GROUP_SUCCESS_METRICS: ChannelMonitorGroupSuccessMetric[] = []
const EMPTY_SMART_SCHEDULE_ROUTES: ChannelMonitorSmartScheduleRoute[] = []
const DEFAULT_CHANNEL_MONITOR_SETTINGS: ChannelMonitorSettings = {
  auto_update_interval_minutes: 0,
  auto_update_retry_count: 2,
  upstream_request_timeout_seconds:
    DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  auto_update_consecutive_failure_limit:
    DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  auto_disable_on_update_failure: false,
  auto_enable_on_cost_ratio_recovery: false,
  auto_enable_on_balance_recovery: false,
  cost_retention_days: DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  email_notification_enabled: false,
  notification_email: '',
  probe_response_enabled: false,
  smart_schedule_enabled: false,
  smart_schedule_group_policies: [],
  smart_schedule_interval_minutes: 10,
  smart_schedule_performance_minutes: 60,
}
const CHANNEL_MONITOR_SORT_STORAGE_KEY = 'channel-monitor:channel-sort'
const CHANNEL_MONITOR_PERFORMANCE_RANGE_STORAGE_KEY =
  'channel-monitor:performance-range:v1'
const DEFAULT_CHANNEL_MONITOR_PERFORMANCE_MINUTES = 15
const MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES = 1
const MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES = 1440
const CHANNEL_MONITOR_SORT_OPTIONS: Array<{
  value: ChannelMonitorSortMode
  label: string
}> = [
  { value: 'custom', label: '自定义顺序' },
  { value: 'channel_asc', label: '渠道名称：升序' },
  { value: 'channel_desc', label: '渠道名称：降序' },
  { value: 'ratio_desc', label: '成本倍率：从高到低' },
  { value: 'ratio_asc', label: '成本倍率：从低到高' },
  { value: 'first_token_asc', label: '首字：从低到高' },
  { value: 'first_token_desc', label: '首字：从高到低' },
  { value: 'tps_desc', label: 'TPS：从高到低' },
  { value: 'tps_asc', label: 'TPS：从低到高' },
]
export function ChannelMonitor() {
  const queryClient = useQueryClient()
  const [view, setView] = useState<MonitorView>('channels')
  const [upstreamFilter, setUpstreamFilter] =
    useState<ChannelUpstreamFilter>('all')
  const [search, setSearch] = useState('')
  const [performanceRangeMinutes, setPerformanceRangeMinutes] =
    useState<ChannelMonitorPerformanceRangeMinutes>(() => {
      try {
        const storedMinutes = Number(
          localStorage.getItem(CHANNEL_MONITOR_PERFORMANCE_RANGE_STORAGE_KEY)
        )
        if (
          Number.isInteger(storedMinutes) &&
          storedMinutes >= MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES &&
          storedMinutes <= MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES
        ) {
          return storedMinutes
        }
      } catch {}
      return DEFAULT_CHANNEL_MONITOR_PERFORMANCE_MINUTES
    })
  const [performanceRangeInput, setPerformanceRangeInput] = useState(() =>
    String(performanceRangeMinutes)
  )
  const [performanceModelFilter, setPerformanceModelFilter] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [smartScheduleSettingsMounted, setSmartScheduleSettingsMounted] =
    useState(false)
  const [smartScheduleSettingsOpen, setSmartScheduleSettingsOpen] =
    useState(false)
  const [taskHistoryOpen, setTaskHistoryOpen] = useState(false)
  const [taskHistoryKind, setTaskHistoryKind] =
    useState<ChannelMonitorTaskKind>('ratio')
  const [smartScheduleExecutionOpen, setSmartScheduleExecutionOpen] =
    useState(false)
  const [costHistoryOpen, setCostHistoryOpen] = useState(false)
  const [costHistoryChannel, setCostHistoryChannel] = useState<{
    id: number
    name: string
  } | null>(null)
  const [todaySuccessOpen, setTodaySuccessOpen] = useState(false)
  const [smartScheduleDisplayKey, setSmartScheduleDisplayKey] = useState('')
  const [batchTestOpen, setBatchTestOpen] = useState(false)
  const [orderDialogOpen, setOrderDialogOpen] = useState(false)
  const [successDetailTarget, setSuccessDetailTarget] =
    useState<ChannelMonitorSuccessDetailTarget | null>(null)
  const [channelSortMode, setChannelSortMode] =
    useState<ChannelMonitorSortMode>(() => {
      const storedSortMode = localStorage.getItem(
        CHANNEL_MONITOR_SORT_STORAGE_KEY
      )
      switch (storedSortMode) {
        case 'custom':
        case 'channel_asc':
        case 'channel_desc':
        case 'ratio_asc':
        case 'ratio_desc':
        case 'first_token_asc':
        case 'first_token_desc':
        case 'tps_asc':
        case 'tps_desc':
          return storedSortMode
        default:
          return 'ratio_asc'
      }
    })
  const [channelDialog, setChannelDialog] = useState<ChannelDialogState | null>(
    null
  )
  const [editingGroup, setEditingGroup] = useState<GroupMonitorItem | null>(
    null
  )
  const [editingGroupChannels, setEditingGroupChannels] =
    useState<GroupMonitorItem | null>(null)
  const [syncingGroup, setSyncingGroup] = useState<GroupMonitorItem | null>(
    null
  )

  const query = useQuery(getChannelMonitorOverviewQueryOptions())
  const performanceQuery = useQuery(
    getChannelMonitorPerformanceQueryOptions(performanceRangeMinutes)
  )
  const smartScheduleQuery = useQuery(
    getChannelMonitorSmartScheduleQueryOptions()
  )
  const costQuery = useQuery({
    queryKey: ['channel-monitor', 'cost', 'summary', 2],
    queryFn: () => getChannelMonitorCostOverview(2, undefined, 1, true),
    refetchInterval: 60_000,
  })
  const todaySuccessQuery = useQuery({
    queryKey: ['channel-monitor', 'success', 'today'],
    queryFn: () => getChannelMonitorTodaySuccess(),
    refetchInterval: 60_000,
  })
  const ratioFetchMutation = useMutation({
    mutationFn: fetchChannelMonitorUpstreamRatio,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response, channelId) => {
      toast.success(
        `已获取上游倍率 ${formatMonitorRatio(response.data.result.ratio)}，成本倍率 ${formatMonitorRatio(response.data.result.cost_ratio)}`
      )
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor-history', channelId],
      })
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    },
  })
  const balanceFetchMutation = useMutation({
    mutationFn: fetchChannelMonitorUpstreamBalance,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response, channelId) => {
      const balance = response.data.amount
      toast.success(
        balance == null
          ? '上游未返回余额'
          : `已更新上游余额：${balance.toLocaleString(undefined, {
              minimumFractionDigits: 0,
              maximumFractionDigits: 4,
            })}`
      )
      queryClient.invalidateQueries({
        queryKey: ['channel-monitor-history', channelId],
      })
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
    },
  })
  const statusMutation = useMutation({
    mutationFn: updateMonitoredChannelStatus,
    onError: handleChannelMonitorMutationError,
    onSuccess: (_response, request) => {
      toast.success(
        request.status === CHANNEL_STATUS.ENABLED ? '渠道已启用' : '渠道已禁用'
      )
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['channel-monitor'] })
      queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })
  const smartScheduleChannelMutation = useMutation({
    mutationFn: updateChannelMonitorSmartScheduleChannelConfig,
    onError: handleChannelMonitorMutationError,
    onSuccess: (response) => {
      toast.success(
        `已更新 ${response.data.updated}/${response.data.total} 条路由的智能调度参与设置`
      )
    },
    onSettled: () => {
      smartScheduleQuery.refetch()
    },
  })
  const overview = query.data?.data
  const channels = overview?.channels ?? EMPTY_CHANNELS
  const channelOrder = overview?.channel_order ?? EMPTY_CHANNEL_ORDER
  const groupRatios = overview?.group_ratios ?? EMPTY_GROUP_RATIOS
  const groupCoefficients =
    overview?.group_coefficients ?? EMPTY_GROUP_COEFFICIENTS
  const settings = overview?.settings ?? DEFAULT_CHANNEL_MONITOR_SETTINGS
  const smartScheduleResult = smartScheduleQuery.data?.data
  const smartScheduleRoutes =
    smartScheduleResult?.routes ?? EMPTY_SMART_SCHEDULE_ROUTES
  const effectiveSmartScheduleRoutes = useMemo(
    () =>
      filterChannelMonitorSmartScheduleRoutes(
        smartScheduleRoutes,
        settings.smart_schedule_enabled &&
          smartScheduleResult?.enabled === true,
        settings.smart_schedule_group_policies
      ),
    [
      settings.smart_schedule_enabled,
      settings.smart_schedule_group_policies,
      smartScheduleResult?.enabled,
      smartScheduleRoutes,
    ]
  )
  const smartScheduleRoutesByChannel = useMemo(
    () => groupChannelMonitorSmartScheduleRoutesByChannel(smartScheduleRoutes),
    [smartScheduleRoutes]
  )
  const smartScheduleDisplayOptions = useMemo(
    () =>
      getChannelMonitorSmartScheduleDisplayOptions(
        effectiveSmartScheduleRoutes,
        groupRatios
      ),
    [effectiveSmartScheduleRoutes, groupRatios]
  )
  const activeSmartScheduleDisplay =
    smartScheduleDisplayOptions.find(
      (option) => option.value === smartScheduleDisplayKey
    ) ??
    smartScheduleDisplayOptions[0] ??
    null
  const smartScheduleSummary = useMemo(
    () =>
      summarizeChannelMonitorSmartScheduleOverview(
        effectiveSmartScheduleRoutes
      ),
    [effectiveSmartScheduleRoutes]
  )
  const smartScheduleHasCriticalIssue =
    settings.smart_schedule_enabled &&
    (smartScheduleQuery.isError ||
      smartScheduleSummary.degradedCount > 0 ||
      smartScheduleSummary.failedCount > 0)
  const smartScheduleHasProbing =
    !smartScheduleHasCriticalIssue && smartScheduleSummary.probingCount > 0
  const performanceMetrics =
    performanceQuery.data?.data.items ?? EMPTY_PERFORMANCE_METRICS
  const successMetrics =
    performanceQuery.data?.data.success_items ?? EMPTY_SUCCESS_METRICS
  const groupSuccessMetrics =
    performanceQuery.data?.data.group_success_items ??
    EMPTY_GROUP_SUCCESS_METRICS
  const successMetricsAvailable =
    performanceQuery.data?.data.success_metrics_available ?? false
  const dialogChannel =
    channels.find((channel) => channel.id === channelDialog?.channelId) ?? null
  const autoUpdateIntervalMinutes = settings.auto_update_interval_minutes
  const autoUpdateConsecutiveFailureLimit =
    settings.auto_update_consecutive_failure_limit ??
    DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT
  const autoUpdateLabel =
    autoUpdateIntervalMinutes > 0
      ? `自动更新：每 ${autoUpdateIntervalMinutes} 分钟 · 失败重试 ${settings.auto_update_retry_count} 次 · 连续失败 ${autoUpdateConsecutiveFailureLimit} 次后停止`
      : '自动更新：已关闭'
  const smartScheduleLabel = settings.smart_schedule_enabled
    ? `智能调度：每 ${settings.smart_schedule_interval_minutes} 分钟`
    : '智能调度：已关闭'
  const performanceRangeLabel = `近${performanceRangeMinutes}分钟`
  const parsedPerformanceRangeMinutes = Number(performanceRangeInput)
  const isPerformanceRangeInputValid =
    Number.isInteger(parsedPerformanceRangeMinutes) &&
    parsedPerformanceRangeMinutes >= MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES &&
    parsedPerformanceRangeMinutes <= MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES

  const applyPerformanceRange = () => {
    if (!isPerformanceRangeInputValid) {
      toast.error('统计范围必须是 1 到 1440 之间的整数分钟')
      setPerformanceRangeInput(String(performanceRangeMinutes))
      return
    }
    if (parsedPerformanceRangeMinutes === performanceRangeMinutes) return
    setPerformanceRangeMinutes(parsedPerformanceRangeMinutes)
    try {
      localStorage.setItem(
        CHANNEL_MONITOR_PERFORMANCE_RANGE_STORAGE_KEY,
        String(parsedPerformanceRangeMinutes)
      )
    } catch {}
  }

  const groups = useMemo<GroupMonitorItem[]>(() => {
    const groupNames = new Set(Object.keys(groupRatios))
    for (const channel of channels) {
      for (const group of channel.groups) groupNames.add(group)
    }
    return [...groupNames]
      .sort((a, b) => a.localeCompare(b))
      .map((name) => ({
        name,
        ratio: groupRatios[name] ?? 1,
        coefficient: groupCoefficients[name] ?? 1,
        channels: channels.filter((channel) => channel.groups.includes(name)),
      }))
  }, [channels, groupCoefficients, groupRatios])

  const normalizedSearch = search.trim().toLocaleLowerCase()
  const matchingChannels = useMemo(
    () =>
      channels.filter((channel) => {
        if (
          upstreamFilter !== 'all' &&
          channel.upstream?.type !== upstreamFilter
        ) {
          return false
        }
        if (!normalizedSearch) return true
        return (
          channel.name.toLocaleLowerCase().includes(normalizedSearch) ||
          String(channel.id).includes(normalizedSearch) ||
          channel.groups.some((group) =>
            group.toLocaleLowerCase().includes(normalizedSearch)
          )
        )
      }),
    [channels, normalizedSearch, upstreamFilter]
  )
  const filteredGroups = useMemo(() => {
    if (!normalizedSearch) return groups
    return groups.filter(
      (group) =>
        group.name.toLocaleLowerCase().includes(normalizedSearch) ||
        group.channels.some((channel) =>
          channel.name.toLocaleLowerCase().includes(normalizedSearch)
        )
    )
  }, [groups, normalizedSearch])
  const performanceByChannel = useMemo(() => {
    type PerformanceAggregate = {
      sampleCount: number
      firstTokenSampleCount: number
      tpsSampleCount: number
      firstTokenTotalMs: number
      tpsTotal: number
      lastUsedTime: number
    }
    const aggregates = new Map<number, PerformanceAggregate>()
    for (const metric of performanceMetrics) {
      const aggregate = aggregates.get(metric.channel_id) ?? {
        sampleCount: 0,
        firstTokenSampleCount: 0,
        tpsSampleCount: 0,
        firstTokenTotalMs: 0,
        tpsTotal: 0,
        lastUsedTime: 0,
      }
      aggregate.sampleCount += metric.sample_count
      if (
        metric.average_first_token_ms != null &&
        metric.first_token_sample_count > 0
      ) {
        aggregate.firstTokenSampleCount += metric.first_token_sample_count
        aggregate.firstTokenTotalMs +=
          metric.average_first_token_ms * metric.first_token_sample_count
      }
      if (metric.average_tps != null && metric.tps_sample_count > 0) {
        aggregate.tpsSampleCount += metric.tps_sample_count
        aggregate.tpsTotal += metric.average_tps * metric.tps_sample_count
      }
      aggregate.lastUsedTime = Math.max(
        aggregate.lastUsedTime,
        metric.last_used_time
      )
      aggregates.set(metric.channel_id, aggregate)
    }

    const result = new Map<number, ChannelMonitorChannelPerformance>()
    for (const [channelId, aggregate] of aggregates) {
      result.set(channelId, {
        sample_count: aggregate.sampleCount,
        first_token_sample_count: aggregate.firstTokenSampleCount,
        tps_sample_count: aggregate.tpsSampleCount,
        average_first_token_ms:
          aggregate.firstTokenSampleCount > 0
            ? aggregate.firstTokenTotalMs / aggregate.firstTokenSampleCount
            : null,
        average_tps:
          aggregate.tpsSampleCount > 0
            ? aggregate.tpsTotal / aggregate.tpsSampleCount
            : null,
        last_used_time: aggregate.lastUsedTime,
      })
    }
    return result
  }, [performanceMetrics])
  const successByChannel = useMemo(() => {
    const result = new Map<number, ChannelMonitorSuccessSummary>()
    for (const metric of successMetrics) {
      const summary = result.get(metric.channel_id) ?? {
        actual_success_count: 0,
        actual_failure_count: 0,
        actual_sample_count: 0,
        actual_success_rate: 0,
        final_success_count: 0,
        final_failure_count: 0,
        final_sample_count: 0,
        final_success_rate: 0,
        cache_hit_count: 0,
        cache_sample_count: 0,
        cache_hit_rate: 0,
      }
      summary.actual_success_count += metric.actual_success_count
      summary.actual_failure_count += metric.actual_failure_count
      summary.actual_sample_count =
        summary.actual_success_count + summary.actual_failure_count
      summary.actual_success_rate =
        summary.actual_sample_count > 0
          ? summary.actual_success_count / summary.actual_sample_count
          : 0
      summary.final_success_count += metric.final_success_count
      summary.final_failure_count += metric.final_failure_count
      summary.final_sample_count =
        summary.final_success_count + summary.final_failure_count
      summary.final_success_rate =
        summary.final_sample_count > 0
          ? summary.final_success_count / summary.final_sample_count
          : 0
      summary.cache_hit_count += metric.cache_hit_count ?? 0
      summary.cache_sample_count += metric.cache_sample_count ?? 0
      summary.cache_hit_rate =
        summary.cache_sample_count > 0
          ? summary.cache_hit_count / summary.cache_sample_count
          : 0
      result.set(metric.channel_id, summary)
    }
    return result
  }, [successMetrics])
  const successByGroup = useMemo(
    () => new Map(groupSuccessMetrics.map((metric) => [metric.group, metric])),
    [groupSuccessMetrics]
  )
  const filteredChannels = useMemo(
    () =>
      sortChannelMonitorItems(
        matchingChannels,
        channelSortMode,
        channelOrder,
        performanceByChannel
      ),
    [channelOrder, channelSortMode, matchingChannels, performanceByChannel]
  )
  const performanceModelOptions = useMemo(
    () =>
      [
        ...new Set([
          ...performanceMetrics.map((metric) => metric.model_name),
          ...successMetrics.map((metric) => metric.model_name),
        ]),
      ]
        .sort((first, second) => first.localeCompare(second))
        .map((modelName) => ({ value: modelName, label: modelName })),
    [performanceMetrics, successMetrics]
  )
  const smartScheduleModelOptionsByGroup = useMemo(
    () =>
      getChannelMonitorSmartScheduleModelOptionsByGroup(
        smartScheduleRoutes,
        channels,
        settings.smart_schedule_group_policies
      ),
    [channels, settings.smart_schedule_group_policies, smartScheduleRoutes]
  )
  const activePerformanceModel = performanceModelOptions.some(
    (option) => option.value === performanceModelFilter
  )
    ? performanceModelFilter
    : (performanceModelOptions[0]?.value ?? '')

  const costOverview = costQuery.data?.data
  let costDescription = costOverview
    ? `昨日 ${formatChannelMonitorCost(costOverview.yesterday_cost_cny)} · 结算时固化`
    : '按北京时间记录结算成本'
  if (costQuery.isError) {
    costDescription = '成本统计加载失败'
  } else if (costOverview?.coverage.included_channel_count === 0) {
    costDescription = '暂无已记录的成本'
  }
  const newAPIChannelCount = channels.filter(
    (channel) => channel.upstream?.type === 'new_api'
  ).length
  const sub2APIChannelCount = channels.filter(
    (channel) => channel.upstream?.type === 'sub2api'
  ).length
  const customUpstreamChannelCount = channels.filter(
    (channel) => channel.upstream?.type === 'custom'
  ).length

  const openCostHistory = (channel?: ChannelMonitorItem) => {
    setCostHistoryChannel(
      channel ? { id: channel.id, name: channel.name } : null
    )
    setCostHistoryOpen(true)
  }

  const openSmartScheduleSettings = () => {
    setSmartScheduleSettingsMounted(true)
    setSmartScheduleSettingsOpen(true)
  }

  let pageContent: ReactNode
  if (query.isLoading) {
    pageContent = <ChannelMonitorSkeleton />
  } else if (query.isError) {
    pageContent = (
      <Empty className='min-h-80'>
        <EmptyHeader>
          <EmptyTitle>渠道监控加载失败</EmptyTitle>
          <EmptyDescription>请刷新后重试</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    pageContent = (
      <div className='flex flex-col gap-4'>
        <div className='grid gap-3 sm:grid-cols-3'>
          <MonitorStatCard
            label='全部渠道'
            value={channels.length}
            description='包含启用和禁用渠道'
            icon={Analytics01Icon}
          />
          <MonitorStatCard
            label='今日累计成本'
            value={
              costQuery.isLoading ? (
                <Skeleton className='h-7 w-24' />
              ) : (
                formatChannelMonitorCost(costOverview?.today_cost_cny)
              )
            }
            description={costDescription}
            icon={MoneyBag02Icon}
            ariaLabel='查看每日成本'
            onClick={openCostHistory}
          />
          <ChannelMonitorTodaySuccessCard
            result={todaySuccessQuery.data?.data}
            isLoading={todaySuccessQuery.isLoading}
            isError={todaySuccessQuery.isError}
            onOpen={() => setTodaySuccessOpen(true)}
          />
        </div>
        <Tabs
          value={view}
          onValueChange={(value) => setView(value as MonitorView)}
          className='gap-4'
        >
          <div className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
            <TabsList className='grid h-auto w-full grid-cols-2 sm:inline-flex sm:h-8 sm:w-fit'>
              <TabsTrigger value='channels'>
                <HugeiconsIcon
                  icon={Analytics01Icon}
                  data-icon='inline-start'
                />
                渠道 {channels.length}
              </TabsTrigger>
              <TabsTrigger value='groups'>
                <HugeiconsIcon icon={Layers01Icon} data-icon='inline-start' />
                分组 {groups.length}
              </TabsTrigger>
              <TabsTrigger value='models'>
                <HugeiconsIcon
                  icon={ChartLineData01Icon}
                  data-icon='inline-start'
                />
                模型性能 {performanceModelOptions.length}
              </TabsTrigger>
              <TabsTrigger value='smart-schedule'>
                <HugeiconsIcon icon={Route01Icon} data-icon='inline-start' />
                智能调度 {smartScheduleSummary.poolCount}
                {smartScheduleHasCriticalIssue ? (
                  <>
                    <span
                      className='bg-destructive size-1.5 shrink-0 rounded-full'
                      aria-hidden='true'
                    />
                    <span className='sr-only'>存在需要关注的调度状态</span>
                  </>
                ) : null}
                {smartScheduleHasProbing ? (
                  <>
                    <span
                      className='bg-warning size-1.5 shrink-0 rounded-full'
                      aria-hidden='true'
                    />
                    <span className='sr-only'>存在稳定性试放中的调度状态</span>
                  </>
                ) : null}
              </TabsTrigger>
            </TabsList>

            {view !== 'smart-schedule' ? (
              <div className='flex w-full flex-col gap-2 sm:flex-row sm:flex-wrap lg:max-w-5xl lg:justify-end'>
                {view === 'channels' && (
                  <ToggleGroup
                    value={[upstreamFilter]}
                    onValueChange={(values) => {
                      const nextValue = values.find(
                        (value) => value !== upstreamFilter
                      )
                      if (
                        nextValue !== 'all' &&
                        nextValue !== 'new_api' &&
                        nextValue !== 'sub2api' &&
                        nextValue !== 'custom'
                      ) {
                        return
                      }
                      setUpstreamFilter(nextValue)
                    }}
                    variant='outline'
                    size='sm'
                    spacing={0}
                    aria-label='按上游类型筛选渠道'
                    className='grid w-full grid-cols-2 sm:w-auto sm:grid-cols-4'
                  >
                    <ToggleGroupItem value='all' className='w-full'>
                      全部 {channels.length}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='new_api' className='w-full'>
                      New API {newAPIChannelCount}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='sub2api' className='w-full'>
                      Sub2API {sub2APIChannelCount}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='custom' className='w-full'>
                      自定义 {customUpstreamChannelCount}
                    </ToggleGroupItem>
                  </ToggleGroup>
                )}

                {view === 'channels' && (
                  <div className='flex w-full flex-col gap-2 sm:w-auto sm:flex-row'>
                    <Select
                      items={smartScheduleDisplayOptions}
                      value={activeSmartScheduleDisplay?.value ?? null}
                      onValueChange={(value) => {
                        if (value !== null) setSmartScheduleDisplayKey(value)
                      }}
                    >
                      <SelectTrigger
                        className='w-full sm:w-60'
                        aria-label='选择智能调度展示路由'
                        disabled={smartScheduleDisplayOptions.length === 0}
                      >
                        <SelectValue placeholder='选择分组 / 模型' />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {smartScheduleDisplayOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <Select
                      items={CHANNEL_MONITOR_SORT_OPTIONS}
                      value={channelSortMode}
                      onValueChange={(value) => {
                        if (value === null) return
                        setChannelSortMode(value)
                        localStorage.setItem(
                          CHANNEL_MONITOR_SORT_STORAGE_KEY,
                          value
                        )
                      }}
                    >
                      <SelectTrigger
                        className='w-full sm:w-48'
                        aria-label='渠道排序方式'
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {CHANNEL_MONITOR_SORT_OPTIONS.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    {channelSortMode === 'custom' && (
                      <Button
                        type='button'
                        variant='outline'
                        onClick={() => setOrderDialogOpen(true)}
                        className='shrink-0'
                      >
                        <HugeiconsIcon
                          icon={ArrangeIcon}
                          data-icon='inline-start'
                        />
                        调整顺序
                      </Button>
                    )}
                  </div>
                )}

                {view === 'models' && (
                  <div className='flex w-full sm:w-56'>
                    <Select
                      items={performanceModelOptions}
                      value={activePerformanceModel || null}
                      onValueChange={(value) => {
                        if (value !== null) setPerformanceModelFilter(value)
                      }}
                    >
                      <SelectTrigger
                        className='min-w-0 flex-1 sm:w-56'
                        aria-label='选择性能模型'
                        disabled={performanceModelOptions.length === 0}
                      >
                        <SelectValue placeholder='选择模型' />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {performanceModelOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                )}

                <InputGroup className='w-full sm:w-36'>
                  <InputGroupAddon>近</InputGroupAddon>
                  <InputGroupInput
                    type='number'
                    min={MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES}
                    max={MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES}
                    step={1}
                    value={performanceRangeInput}
                    onChange={(event) =>
                      setPerformanceRangeInput(event.target.value)
                    }
                    onBlur={applyPerformanceRange}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') event.currentTarget.blur()
                    }}
                    aria-label='性能与成功率统计范围（分钟）'
                    aria-invalid={!isPerformanceRangeInputValid}
                    className='min-w-0 text-right font-mono'
                  />
                  <InputGroupAddon align='inline-end'>分钟</InputGroupAddon>
                </InputGroup>

                <InputGroup className='w-full sm:max-w-sm'>
                  <InputGroupAddon>
                    <HugeiconsIcon icon={Search01Icon} />
                  </InputGroupAddon>
                  <InputGroupInput
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={
                      view === 'models' ? '搜索渠道' : '搜索渠道或分组'
                    }
                    aria-label={
                      view === 'models' ? '搜索渠道' : '搜索渠道或分组'
                    }
                  />
                </InputGroup>
              </div>
            ) : null}
          </div>

          <TabsContent value='channels'>
            <div className='flex flex-col gap-4'>
              <ChannelMonitorChannelView
                channels={filteredChannels}
                groupRatios={groupRatios}
                groupCoefficients={groupCoefficients}
                performanceByChannel={performanceByChannel}
                successByChannel={successByChannel}
                successMetricsAvailable={successMetricsAvailable}
                performanceRangeLabel={performanceRangeLabel}
                performanceLoading={performanceQuery.isLoading}
                performanceError={performanceQuery.isError}
                smartScheduleRoutesByChannel={smartScheduleRoutesByChannel}
                smartScheduleSelectedGroupModel={activeSmartScheduleDisplay}
                smartScheduleUpdatePending={
                  smartScheduleChannelMutation.isPending
                }
                onUpdateSmartSchedule={(channelId, excluded) => {
                  if (smartScheduleChannelMutation.isPending) return
                  smartScheduleChannelMutation.mutate({ channelId, excluded })
                }}
                onFetchUpstreamBalance={(channel) =>
                  balanceFetchMutation.mutate(channel.id)
                }
                onFetchUpstreamRatio={(channel) =>
                  ratioFetchMutation.mutate(channel.id)
                }
                onTestConnection={(channel) =>
                  setChannelDialog({
                    channelId: channel.id,
                    type: 'connection_test',
                  })
                }
                onToggleStatus={(channel) =>
                  statusMutation.mutate({
                    channelId: channel.id,
                    status:
                      channel.status === CHANNEL_STATUS.ENABLED
                        ? CHANNEL_STATUS.MANUAL_DISABLED
                        : CHANNEL_STATUS.ENABLED,
                  })
                }
                onEditConcurrency={(channel) =>
                  setChannelDialog({
                    channelId: channel.id,
                    type: 'concurrency',
                  })
                }
                onEditGroups={(channel) =>
                  setChannelDialog({ channelId: channel.id, type: 'groups' })
                }
                onConfigureUpstream={(channel) =>
                  setChannelDialog({ channelId: channel.id, type: 'upstream' })
                }
                onViewHistory={(channel) =>
                  setChannelDialog({ channelId: channel.id, type: 'history' })
                }
                onOpenCostHistory={openCostHistory}
                onOpenSuccessDetail={(channel) =>
                  setSuccessDetailTarget({
                    scope: 'channel',
                    mode: 'actual',
                    channelId: channel.id,
                    channelName: channel.name,
                  })
                }
                fetchingBalanceChannelId={
                  balanceFetchMutation.isPending
                    ? balanceFetchMutation.variables
                    : null
                }
                fetchingRatioChannelId={
                  ratioFetchMutation.isPending
                    ? ratioFetchMutation.variables
                    : null
                }
                updatingStatusChannelId={
                  statusMutation.isPending
                    ? (statusMutation.variables?.channelId ?? null)
                    : null
                }
              />
            </div>
          </TabsContent>
          <TabsContent value='groups'>
            <ChannelMonitorGroupView
              groups={filteredGroups}
              successByGroup={successByGroup}
              successMetricsAvailable={successMetricsAvailable}
              successLoading={performanceQuery.isLoading}
              successError={performanceQuery.isError}
              successRangeLabel={performanceRangeLabel}
              onOpenSuccessDetail={(group, mode) =>
                setSuccessDetailTarget({
                  scope: 'group',
                  mode,
                  groupName: group.name,
                })
              }
              onOpenScheduleSettings={openSmartScheduleSettings}
              onEditChannels={setEditingGroupChannels}
              onEditGroup={setEditingGroup}
              onSyncGroup={setSyncingGroup}
            />
          </TabsContent>
          <TabsContent value='models'>
            <ChannelMonitorModelPerformanceView
              key={activePerformanceModel}
              channels={channels}
              metrics={performanceMetrics}
              successMetrics={successMetrics}
              successMetricsAvailable={successMetricsAvailable}
              selectedModel={activePerformanceModel}
              search={search}
              isLoading={performanceQuery.isLoading}
              isError={performanceQuery.isError}
              onOpenSuccessDetail={(channel, modelName) =>
                setSuccessDetailTarget({
                  scope: 'channel',
                  mode: 'actual',
                  channelId: channel.id,
                  channelName: channel.name,
                  modelName,
                })
              }
            />
          </TabsContent>
          <TabsContent value='smart-schedule'>
            <ChannelMonitorSmartScheduleBoard
              active={view === 'smart-schedule'}
              result={smartScheduleResult}
              channels={channels}
              groupPolicies={settings.smart_schedule_group_policies}
              groupRatios={groupRatios}
              intervalMinutes={settings.smart_schedule_interval_minutes}
              isLoading={smartScheduleQuery.isLoading}
              isError={smartScheduleQuery.isError}
              onOpenHistory={() => {
                setSmartScheduleExecutionOpen(true)
              }}
              onOpenSettings={openSmartScheduleSettings}
            />
          </TabsContent>
        </Tabs>
      </div>
    )
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>渠道监控</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => setBatchTestOpen(true)}
                  aria-label='渠道连通性测试'
                >
                  <HugeiconsIcon icon={TestTubeIcon} />
                </Button>
              }
            />
            <TooltipContent>
              批量测试渠道，或对单个渠道和模型进行并发循环测试
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => {
                    setTaskHistoryKind('ratio')
                    setTaskHistoryOpen(true)
                  }}
                  aria-label='定时任务记录'
                >
                  <HugeiconsIcon icon={HistoryIcon} />
                </Button>
              }
            />
            <TooltipContent>定时任务记录</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onMouseEnter={() => {
                    void loadChannelMonitorSmartScheduleExecutionDialog()
                  }}
                  onFocus={() => {
                    void loadChannelMonitorSmartScheduleExecutionDialog()
                  }}
                  onClick={() => setSmartScheduleExecutionOpen(true)}
                  aria-label='智能调度执行记录'
                >
                  <HugeiconsIcon icon={ChartLineData01Icon} />
                </Button>
              }
            />
            <TooltipContent>智能调度执行记录</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => setSettingsOpen(true)}
                  aria-label='渠道监控设置'
                >
                  <HugeiconsIcon icon={Settings02Icon} />
                </Button>
              }
            />
            <TooltipContent>{autoUpdateLabel}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={openSmartScheduleSettings}
                  aria-label='智能调度设置'
                >
                  <HugeiconsIcon icon={Route01Icon} />
                </Button>
              }
            />
            <TooltipContent>{smartScheduleLabel}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  size='icon'
                  onClick={() => {
                    query.refetch()
                    performanceQuery.refetch()
                    costQuery.refetch()
                    todaySuccessQuery.refetch()
                    smartScheduleQuery.refetch()
                  }}
                  disabled={
                    query.isFetching ||
                    performanceQuery.isFetching ||
                    costQuery.isFetching ||
                    todaySuccessQuery.isFetching ||
                    smartScheduleQuery.isFetching
                  }
                  aria-label='刷新'
                >
                  <HugeiconsIcon icon={Refresh01Icon} />
                </Button>
              }
            />
            <TooltipContent>刷新</TooltipContent>
          </Tooltip>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>{pageContent}</SectionPageLayout.Content>
      </SectionPageLayout>

      {dialogChannel && channelDialog?.type === 'concurrency' && (
        <EditChannelConcurrencyLimitDialog
          key={dialogChannel.id}
          channel={dialogChannel}
          open
          onOpenChange={(open) => {
            if (!open) setChannelDialog(null)
          }}
        />
      )}
      {dialogChannel && channelDialog?.type === 'groups' && (
        <EditChannelGroupsDialog
          key={dialogChannel.id}
          channel={dialogChannel}
          open
          onOpenChange={(open) => {
            if (!open) setChannelDialog(null)
          }}
        />
      )}
      {dialogChannel && channelDialog?.type === 'upstream' && (
        <UpstreamConfigDialog
          key={dialogChannel.id}
          channel={dialogChannel}
          open
          onOpenChange={(open) => {
            if (!open) setChannelDialog(null)
          }}
        />
      )}
      {dialogChannel && channelDialog?.type === 'history' && (
        <ChannelRatioHistoryDialog
          key={dialogChannel.id}
          channel={dialogChannel}
          open
          onOpenChange={(open) => {
            if (!open) setChannelDialog(null)
          }}
        />
      )}
      {dialogChannel && channelDialog?.type === 'connection_test' && (
        <ChannelTestDialogForChannel
          channel={dialogChannel}
          open
          onOpenChange={(open) => {
            if (!open) setChannelDialog(null)
          }}
        />
      )}
      {editingGroup && (
        <EditGroupRatioDialog
          key={editingGroup.name}
          group={editingGroup}
          open
          onOpenChange={(open) => {
            if (!open) setEditingGroup(null)
          }}
        />
      )}
      {editingGroupChannels && (
        <EditGroupChannelsDialog
          key={editingGroupChannels.name}
          group={editingGroupChannels}
          channels={channels}
          open
          onOpenChange={(open) => {
            if (!open) setEditingGroupChannels(null)
          }}
        />
      )}
      {syncingGroup && (
        <SyncGroupRatioDialog
          key={`${syncingGroup.name}:${syncingGroup.coefficient}`}
          group={syncingGroup}
          open
          onOpenChange={(open) => {
            if (!open) setSyncingGroup(null)
          }}
        />
      )}
      {settingsOpen && (
        <ChannelMonitorSettingsDialog
          key={`${settings.auto_update_interval_minutes}:${settings.auto_update_retry_count}:${settings.upstream_request_timeout_seconds ?? DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS}:${autoUpdateConsecutiveFailureLimit}:${settings.auto_disable_on_update_failure}:${settings.auto_enable_on_cost_ratio_recovery}:${settings.auto_enable_on_balance_recovery}:${settings.cost_retention_days}:${settings.email_notification_enabled}:${settings.notification_email}:${settings.probe_response_enabled}`}
          settings={settings}
          open
          onOpenChange={setSettingsOpen}
        />
      )}
      {smartScheduleSettingsMounted && (
        <ChannelMonitorSmartScheduleSettingsSheet
          settings={settings}
          modelOptionsByGroup={smartScheduleModelOptionsByGroup}
          groupOptions={groups.map((group) => group.name)}
          open={smartScheduleSettingsOpen}
          onOpenChange={setSmartScheduleSettingsOpen}
          onOpenChangeComplete={(open) => {
            if (!open) setSmartScheduleSettingsMounted(false)
          }}
        />
      )}
      {taskHistoryOpen && (
        <ChannelMonitorTaskHistoryDialog
          initialKind={taskHistoryKind}
          open
          onOpenChange={setTaskHistoryOpen}
        />
      )}
      {smartScheduleExecutionOpen && (
        <Suspense fallback={null}>
          <LazyChannelMonitorSmartScheduleExecutionDialog
            open
            onOpenChange={setSmartScheduleExecutionOpen}
          />
        </Suspense>
      )}
      {costHistoryOpen && (
        <Suspense fallback={null}>
          <LazyChannelMonitorCostHistoryDialog
            open
            channelId={costHistoryChannel?.id}
            channelName={costHistoryChannel?.name}
            onOpenChange={(open) => {
              setCostHistoryOpen(open)
              if (!open) {
                setCostHistoryChannel(null)
              }
            }}
          />
        </Suspense>
      )}
      {todaySuccessOpen && (
        <Suspense fallback={null}>
          <LazyChannelMonitorTodaySuccessDialog
            channels={channels}
            open
            onOpenChange={setTodaySuccessOpen}
          />
        </Suspense>
      )}
      {batchTestOpen && (
        <Suspense fallback={null}>
          <LazyChannelBatchTestDialog
            open
            channels={channels}
            modelSelectionMode='single'
            selectAllMode='all'
            enableRepeatMode
            onOpenChange={setBatchTestOpen}
          />
        </Suspense>
      )}
      {orderDialogOpen && (
        <ChannelMonitorOrderDialog
          key={`${channels.length}:${channelOrder.join(',')}`}
          channels={channels}
          channelOrder={channelOrder}
          open
          onOpenChange={setOrderDialogOpen}
        />
      )}
      {successDetailTarget && (
        <ChannelMonitorSuccessDetailDialog
          target={successDetailTarget}
          channels={channels}
          rangeMinutes={performanceRangeMinutes}
          rangeLabel={performanceRangeLabel}
          open
          onOpenChange={(open) => {
            if (!open) setSuccessDetailTarget(null)
          }}
        />
      )}
    </>
  )
}

type MonitorStatCardProps = {
  label: string
  value: ReactNode
  description: string
  icon: React.ComponentProps<typeof HugeiconsIcon>['icon']
  ariaLabel?: string
  onClick?: () => void
}

export function MonitorStatCard(props: MonitorStatCardProps) {
  const interactive = props.onClick != null

  return (
    <Card
      size='sm'
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? (props.ariaLabel ?? props.label) : undefined}
      onClick={props.onClick}
      onKeyDown={
        interactive
          ? (event) => {
              if (event.key !== 'Enter' && event.key !== ' ') return
              event.preventDefault()
              props.onClick?.()
            }
          : undefined
      }
      className={cn(
        interactive &&
          'cursor-pointer transition-colors hover:ring-2 hover:ring-ring/30 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none'
      )}
    >
      <CardHeader>
        <CardDescription>{props.label}</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{props.value}</CardTitle>
        <CardAction>
          <span className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={props.icon} />
          </span>
        </CardAction>
        <CardDescription>{props.description}</CardDescription>
      </CardHeader>
    </Card>
  )
}

function ChannelMonitorSkeleton() {
  return (
    <div className='flex flex-col gap-4'>
      <Skeleton className='h-9 w-full max-w-lg' />
      <Skeleton className='h-96 w-full rounded-lg' />
    </div>
  )
}
