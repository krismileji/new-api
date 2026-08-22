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
  Activity01Icon,
  ArrangeIcon,
  HistoryIcon,
  MoneyBag02Icon,
  Refresh01Icon,
  Route01Icon,
  Search01Icon,
  Settings02Icon,
  TestTubeIcon,
  WorkflowSquare06Icon,
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
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ChannelTestDialogForChannel } from '@/features/channels/components/dialogs/channel-test-dialog'
import { CHANNEL_STATUS } from '@/features/channels/constants'
import { getChannelGroupMonitorSettings } from '@/features/group-monitor/api'
import { cn } from '@/lib/utils'

import {
  fetchChannelMonitorUpstreamBalance,
  fetchChannelMonitorUpstreamRatio,
  getChannelMonitorCostOverview,
  getChannelModelDetectionOverview,
  getChannelMonitorTodaySuccess,
  updateChannelMonitorSmartScheduleChannelConfig,
  updateMonitoredChannelStatus,
} from './api'
import { ChannelGroupMonitorSettingsSheet } from './components/channel-group-monitor-settings-sheet'
import { ChannelMonitorChannelView } from './components/channel-monitor-channel-view'
import { ChannelMonitorGroupView } from './components/channel-monitor-group-view'
import { ChannelMonitorModelPerformanceView } from './components/channel-monitor-model-performance-view'
import { ChannelMonitorOrderDialog } from './components/channel-monitor-order-dialog'
import { ChannelMonitorPerformanceCoverageAlert } from './components/channel-monitor-performance-coverage-alert'
import { ChannelMonitorPerformanceRangeControl } from './components/channel-monitor-performance-range-control'
import { ChannelMonitorRealtimeStatus } from './components/channel-monitor-realtime-status'
import {
  ChannelMonitorSettingsDialog,
  ChannelMonitorSmartScheduleSettingsSheet,
} from './components/channel-monitor-settings-dialog'
import { ChannelMonitorSmartScheduleBoard } from './components/channel-monitor-smart-schedule-board'
import { ChannelMonitorSuccessDetailDialog } from './components/channel-monitor-success-detail-dialog'
import { ChannelMonitorTodaySuccessCard } from './components/channel-monitor-today-success-card'
import { ChannelMonitorViewTabs } from './components/channel-monitor-view-tabs'
import { ChannelRatioHistoryDialog } from './components/channel-ratio-history-dialog'
import { EditChannelConcurrencyLimitDialog } from './components/edit-channel-concurrency-limit-dialog'
import { EditChannelGroupsDialog } from './components/edit-channel-groups-dialog'
import { EditGroupChannelsDialog } from './components/edit-group-channels-dialog'
import { EditGroupRatioDialog } from './components/edit-group-ratio-dialog'
import { SyncGroupRatioDialog } from './components/sync-group-ratio-dialog'
import { UpstreamConfigDialog } from './components/upstream-config-dialog'
import { DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES } from './lib/email-notification'
import { handleChannelMonitorMutationError } from './lib/error'
import {
  formatChannelMonitorCost,
  formatChannelMonitorResolutionRate,
  formatMonitorRatio,
} from './lib/format'
import { isChannelModelDetectionRunActive } from './lib/model-detection'
import { aggregateChannelMonitorPerformanceByChannel } from './lib/performance'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
  getChannelMonitorActiveRefetchInterval,
  getChannelMonitorConcurrencyQueryOptions,
  getChannelMonitorOverviewQueryOptions,
  getChannelMonitorPerformanceQueryOptions,
  getChannelMonitorSmartScheduleQueryOptions,
  isChannelMonitorPerformanceQueryActive,
  refetchChannelMonitorQueries,
} from './lib/query-options'
import { mergeChannelMonitorRealtimeMetadata } from './lib/realtime-metadata'
import {
  DEFAULT_AUTO_UPDATE_CONSECUTIVE_FAILURE_LIMIT,
  DEFAULT_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_ENABLED,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  DEFAULT_CHANNEL_MONITOR_CLEANUP_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_COST_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_GROUP_MONITOR_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_RATIO_MONITOR_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_PROBE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
  DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS,
  DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS,
  DEFAULT_PROBE_RESPONSE_CACHED_TOKENS,
  DEFAULT_PROBE_RESPONSE_INPUT_TOKENS,
  DEFAULT_PROBE_RESPONSE_MATCH_INPUT,
  DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS,
  DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS,
  DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS,
  DEFAULT_PROBE_RESPONSE_TEXT,
  DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
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
  ChannelMonitorItem,
  ChannelMonitorPerformanceMetric,
  ChannelMonitorPerformanceRangeMinutes,
  ChannelMonitorSettings,
  ChannelMonitorSmartScheduleRoute,
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
const loadChannelMonitorTaskHistoryDialog = () =>
  import('./components/channel-monitor-task-history-dialog').then((module) => ({
    default: module.ChannelMonitorTaskHistoryDialog,
  }))
const LazyChannelMonitorTaskHistoryDialog = lazy(
  loadChannelMonitorTaskHistoryDialog
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
const LazyChannelStatusProbeView = lazy(() =>
  import('./components/channel-status-probe-view').then((module) => ({
    default: module.ChannelStatusProbeView,
  }))
)
const LazyChannelModelDetectionView = lazy(() =>
  import('./components/channel-model-detection-workspace').then((module) => ({
    default: module.ChannelModelDetectionWorkspace,
  }))
)
type MonitorView =
  | 'channels'
  | 'groups'
  | 'models'
  | 'status-probe'
  | 'model-detection'
  | 'smart-schedule'
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
type SmartScheduleDisplaySelection = {
  group: string
  model: string
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
  route_metric_retention_days:
    DEFAULT_CHANNEL_MONITOR_ROUTE_METRIC_RETENTION_DAYS,
  duration_bucket_retention_days:
    DEFAULT_CHANNEL_MONITOR_DURATION_BUCKET_RETENTION_DAYS,
  api_key_metric_retention_days:
    DEFAULT_CHANNEL_MONITOR_API_KEY_METRIC_RETENTION_DAYS,
  execution_detail_retention_days:
    DEFAULT_CHANNEL_MONITOR_EXECUTION_DETAIL_RETENTION_DAYS,
  task_retention_days: DEFAULT_CHANNEL_MONITOR_TASK_RETENTION_DAYS,
  ratio_monitor_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_RATIO_MONITOR_TASK_RETENTION_DAYS,
  smart_schedule_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_TASK_RETENTION_DAYS,
  smart_schedule_probe_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_SMART_SCHEDULE_PROBE_TASK_RETENTION_DAYS,
  cleanup_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_CLEANUP_TASK_RETENTION_DAYS,
  model_detection_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_TASK_RETENTION_DAYS,
  channel_test_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_CHANNEL_TEST_TASK_RETENTION_DAYS,
  model_update_task_retention_days:
    DEFAULT_CHANNEL_MONITOR_MODEL_UPDATE_TASK_RETENTION_DAYS,
  ratio_history_retention_days:
    DEFAULT_CHANNEL_MONITOR_RATIO_HISTORY_RETENTION_DAYS,
  status_probe_history_retention_days:
    DEFAULT_CHANNEL_MONITOR_STATUS_PROBE_HISTORY_RETENTION_DAYS,
  group_monitor_retention_days:
    DEFAULT_CHANNEL_MONITOR_GROUP_MONITOR_RETENTION_DAYS,
  model_detection_retention_days:
    DEFAULT_CHANNEL_MONITOR_MODEL_DETECTION_RETENTION_DAYS,
  cleanup_enabled: DEFAULT_CHANNEL_MONITOR_CLEANUP_ENABLED,
  cleanup_batch_size: DEFAULT_CHANNEL_MONITOR_CLEANUP_BATCH_SIZE,
  cleanup_budget_seconds: DEFAULT_CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS,
  cleanup_continuation_seconds:
    DEFAULT_CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS,
  cleanup_interval_minutes: DEFAULT_CHANNEL_MONITOR_CLEANUP_INTERVAL_MINUTES,
  email_notification_enabled: false,
  notification_email: '',
  email_notification_types: DEFAULT_CHANNEL_MONITOR_EMAIL_NOTIFICATION_TYPES,
  error_message_mapping: '',
  error_message_keywords: '',
  probe_response_enabled: false,
  probe_response_match_input: DEFAULT_PROBE_RESPONSE_MATCH_INPUT,
  probe_response_text: DEFAULT_PROBE_RESPONSE_TEXT,
  probe_response_min_delay_ms: DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS,
  probe_response_max_delay_ms: DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS,
  probe_response_input_tokens: DEFAULT_PROBE_RESPONSE_INPUT_TOKENS,
  probe_response_cache_write_tokens: DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS,
  probe_response_cached_tokens: DEFAULT_PROBE_RESPONSE_CACHED_TOKENS,
  probe_response_output_tokens: DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS,
  smart_schedule_enabled: false,
  smart_schedule_group_policies: [],
  smart_schedule_performance_window_minutes: 60,
  smart_schedule_realtime_retention_minutes: 60,
  smart_schedule_realtime_sample_limit: 20_000,
  smart_schedule_rate_limit_cooldown_seconds:
    DEFAULT_SMART_SCHEDULE_RATE_LIMIT_COOLDOWN_SECONDS,
  smart_schedule_control_revision: '',
}
const CHANNEL_MONITOR_SORT_STORAGE_KEY = 'channel-monitor:channel-sort'
const CHANNEL_MONITOR_PERFORMANCE_RANGE_STORAGE_KEY =
  'channel-monitor:performance-range:v1'
const CHANNEL_MONITOR_SMART_SCHEDULE_DISPLAY_STORAGE_KEY =
  'channel-monitor:smart-schedule-display:v1'
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
  { value: 'today_cost_desc', label: '今日累计成本：降序' },
  { value: 'today_cost_asc', label: '今日累计成本：升序' },
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
  const [manualPerformanceRangeMinutes, setManualPerformanceRangeMinutes] =
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
    String(manualPerformanceRangeMinutes)
  )
  const [performanceModelFilter, setPerformanceModelFilter] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [groupMonitorSettingsOpen, setGroupMonitorSettingsOpen] =
    useState(false)
  const [smartScheduleSettingsMounted, setSmartScheduleSettingsMounted] =
    useState(false)
  const [smartScheduleSettingsOpen, setSmartScheduleSettingsOpen] =
    useState(false)
  const [taskHistoryOpen, setTaskHistoryOpen] = useState(false)
  const [smartScheduleHistoryOpen, setSmartScheduleHistoryOpen] =
    useState(false)
  const [costHistoryOpen, setCostHistoryOpen] = useState(false)
  const [costHistoryChannel, setCostHistoryChannel] = useState<{
    id: number
    name: string
  } | null>(null)
  const [todaySuccessOpen, setTodaySuccessOpen] = useState(false)
  const [smartScheduleDisplaySelection, setSmartScheduleDisplaySelection] =
    useState<SmartScheduleDisplaySelection>(() => {
      try {
        const stored = localStorage.getItem(
          CHANNEL_MONITOR_SMART_SCHEDULE_DISPLAY_STORAGE_KEY
        )
        if (stored) {
          const parsed: unknown = JSON.parse(stored)
          if (
            parsed &&
            typeof parsed === 'object' &&
            'group' in parsed &&
            'model' in parsed &&
            typeof parsed.group === 'string' &&
            typeof parsed.model === 'string'
          ) {
            return { group: parsed.group, model: parsed.model }
          }
        }
      } catch {}
      return { group: '', model: '' }
    })
  const [batchTestOpen, setBatchTestOpen] = useState(false)
  const [orderDialogOpen, setOrderDialogOpen] = useState(false)
  const [successDetailTarget, setSuccessDetailTarget] =
    useState<ChannelMonitorSuccessDetailTarget | null>(null)
  const [channelSortMode, setChannelSortMode] =
    useState<ChannelMonitorSortMode>(() => {
      let storedSortMode: string | null = null
      try {
        storedSortMode = localStorage.getItem(CHANNEL_MONITOR_SORT_STORAGE_KEY)
      } catch {}
      switch (storedSortMode) {
        case 'custom':
        case 'channel_asc':
        case 'channel_desc':
        case 'ratio_asc':
        case 'ratio_desc':
        case 'today_cost_asc':
        case 'today_cost_desc':
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
  const [manualRefreshPending, setManualRefreshPending] = useState(false)

  const query = useQuery(getChannelMonitorOverviewQueryOptions())
  const overview = query.data?.data
  const concurrencyQuery = useQuery(
    getChannelMonitorConcurrencyQueryOptions(view === 'channels')
  )
  const settings = overview?.settings ?? DEFAULT_CHANNEL_MONITOR_SETTINGS
  const smartSchedulePerformanceRangeActive =
    settings.smart_schedule_enabled &&
    settings.smart_schedule_group_policies.length > 0
  const requestedPerformanceRangeMinutes = smartSchedulePerformanceRangeActive
    ? settings.smart_schedule_performance_window_minutes
    : manualPerformanceRangeMinutes
  const requestedPerformanceRangeSource = smartSchedulePerformanceRangeActive
    ? 'smart_schedule'
    : 'manual'
  const performanceQueryActive =
    isChannelMonitorPerformanceQueryActive(view) && view !== 'model-detection'
  const performanceQuery = useQuery(
    getChannelMonitorPerformanceQueryOptions(
      requestedPerformanceRangeMinutes,
      requestedPerformanceRangeSource,
      performanceQueryActive
    )
  )
  const smartScheduleSummaryQuery = useQuery(
    getChannelMonitorSmartScheduleQueryOptions(false)
  )
  const smartScheduleDetailQuery = useQuery({
    ...getChannelMonitorSmartScheduleQueryOptions(true),
    enabled: view === 'smart-schedule',
  })
  const modelDetectionQuery = useQuery({
    queryKey: ['channel-monitor', 'model-detection', 'overview'],
    queryFn: getChannelModelDetectionOverview,
    enabled: view === 'model-detection',
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
    refetchInterval: (currentQuery) =>
      getChannelMonitorActiveRefetchInterval(
        currentQuery.state.data?.data.channels.some((channel) => {
          const activeRun = channel.active_run
          return (
            activeRun != null &&
            isChannelModelDetectionRunActive(activeRun.status)
          )
        }) ?? false
      ),
  })
  const groupMonitorSettingsQuery = useQuery({
    queryKey: ['channel-monitor', 'group-monitor', 'settings'],
    queryFn: getChannelGroupMonitorSettings,
    enabled: groupMonitorSettingsOpen,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const costQuery = useQuery({
    queryKey: ['channel-monitor', 'cost', 'summary', 2],
    queryFn: () => getChannelMonitorCostOverview(2, undefined, 1, true),
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
  const todaySuccessQuery = useQuery({
    queryKey: ['channel-monitor', 'success', 'today'],
    queryFn: () => getChannelMonitorTodaySuccess(),
    staleTime: Number.POSITIVE_INFINITY,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: false,
  })
  const refreshChannelMonitor = async () => {
    setManualRefreshPending(true)
    try {
      await refetchChannelMonitorQueries(queryClient)
    } finally {
      setManualRefreshPending(false)
    }
  }
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
      queryClient.invalidateQueries({
        queryKey: CHANNEL_MONITOR_SMART_SCHEDULE_QUERY_KEY,
      })
    },
  })
  const overviewChannels = overview?.channels ?? EMPTY_CHANNELS
  const channels = useMemo(() => {
    const concurrencyByChannel = concurrencyQuery.data?.data.channels ?? null
    if (!concurrencyByChannel) return overviewChannels
    return overviewChannels.map((channel) => {
      const status = concurrencyByChannel[String(channel.id)]
      if (!status) return channel
      return {
        ...channel,
        concurrency_active: status.active,
        concurrency_limit: status.limit,
      }
    })
  }, [concurrencyQuery.data?.data.channels, overviewChannels])
  const channelOrder = overview?.channel_order ?? EMPTY_CHANNEL_ORDER
  const groupRatios = overview?.group_ratios ?? EMPTY_GROUP_RATIOS
  const groupCoefficients =
    overview?.group_coefficients ?? EMPTY_GROUP_COEFFICIENTS
  const smartScheduleSummaryResult = smartScheduleSummaryQuery.data?.data
  const smartScheduleResult = smartScheduleDetailQuery.data?.data
  const smartScheduleRoutes =
    smartScheduleSummaryResult?.routes ?? EMPTY_SMART_SCHEDULE_ROUTES
  const effectiveSmartScheduleRoutes = useMemo(
    () =>
      filterChannelMonitorSmartScheduleRoutes(
        smartScheduleRoutes,
        settings.smart_schedule_enabled &&
          smartScheduleSummaryResult?.enabled === true,
        settings.smart_schedule_group_policies
      ),
    [
      settings.smart_schedule_enabled,
      settings.smart_schedule_group_policies,
      smartScheduleSummaryResult?.enabled,
      smartScheduleRoutes,
    ]
  )
  const smartScheduleRoutesByChannel = useMemo(
    () => groupChannelMonitorSmartScheduleRoutesByChannel(smartScheduleRoutes),
    [smartScheduleRoutes]
  )
  const smartScheduleDisplayOptions = useMemo(() => {
    const modelOrderByGroup = new Map<string, readonly string[]>()
    for (const policy of settings.smart_schedule_group_policies) {
      modelOrderByGroup.set(policy.group, policy.model_order ?? [])
    }
    return getChannelMonitorSmartScheduleDisplayOptions(
      effectiveSmartScheduleRoutes,
      groupRatios,
      modelOrderByGroup
    )
  }, [
    effectiveSmartScheduleRoutes,
    groupRatios,
    settings.smart_schedule_group_policies,
  ])
  const smartScheduleDisplayGroups = useMemo(
    () =>
      [
        ...new Set(smartScheduleDisplayOptions.map((option) => option.group)),
      ].map((group) => ({ value: group, label: group })),
    [smartScheduleDisplayOptions]
  )
  const smartScheduleDisplayModelsByGroup = useMemo(() => {
    const modelsByGroup = new Map<string, string[]>()
    for (const option of smartScheduleDisplayOptions) {
      const models = modelsByGroup.get(option.group)
      if (models) models.push(option.model)
      else modelsByGroup.set(option.group, [option.model])
    }
    return modelsByGroup
  }, [smartScheduleDisplayOptions])
  const activeSmartScheduleDisplayGroup = smartScheduleDisplayGroups.some(
    (option) => option.value === smartScheduleDisplaySelection.group
  )
    ? smartScheduleDisplaySelection.group
    : (smartScheduleDisplayGroups[0]?.value ?? '')
  const activeSmartScheduleDisplayModels =
    smartScheduleDisplayModelsByGroup.get(activeSmartScheduleDisplayGroup) ?? []
  const activeSmartScheduleDisplayModel =
    activeSmartScheduleDisplayModels.includes(
      smartScheduleDisplaySelection.model
    )
      ? smartScheduleDisplaySelection.model
      : (activeSmartScheduleDisplayModels[0] ?? '')
  const activeSmartScheduleDisplay =
    smartScheduleDisplayOptions.find(
      (option) =>
        option.group === activeSmartScheduleDisplayGroup &&
        option.model === activeSmartScheduleDisplayModel
    ) ?? null
  const saveSmartScheduleDisplaySelection = (
    selection: SmartScheduleDisplaySelection
  ) => {
    setSmartScheduleDisplaySelection(selection)
    try {
      localStorage.setItem(
        CHANNEL_MONITOR_SMART_SCHEDULE_DISPLAY_STORAGE_KEY,
        JSON.stringify(selection)
      )
    } catch {}
  }
  const smartScheduleSummary = useMemo(
    () =>
      summarizeChannelMonitorSmartScheduleOverview(
        effectiveSmartScheduleRoutes
      ),
    [effectiveSmartScheduleRoutes]
  )
  const smartScheduleHasCriticalIssue =
    Boolean(settings.smart_schedule_config_error) ||
    (settings.smart_schedule_enabled &&
      (smartScheduleSummaryQuery.isError ||
        smartScheduleSummary.degradedCount > 0 ||
        smartScheduleSummary.failedCount > 0))
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
  let smartScheduleLabel = '智能调度：已关闭'
  if (settings.smart_schedule_config_error) {
    smartScheduleLabel = `智能调度：配置错误（${settings.smart_schedule_config_error}）`
  } else if (settings.smart_schedule_enabled) {
    smartScheduleLabel = '智能调度：请求事件投影后异步更新'
  }
  const performanceRangeMinutes =
    performanceQuery.data?.data.range_minutes ??
    requestedPerformanceRangeMinutes
  const performanceRangeSource =
    performanceQuery.data?.data.range_source ?? requestedPerformanceRangeSource
  const performanceRangeLabel = `近${performanceRangeMinutes}分钟`
  const manualPerformanceRangeLabel = `近${manualPerformanceRangeMinutes}分钟`
  const parsedPerformanceRangeMinutes = Number(performanceRangeInput)
  const isPerformanceRangeInputValid =
    Number.isInteger(parsedPerformanceRangeMinutes) &&
    parsedPerformanceRangeMinutes >= MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES &&
    parsedPerformanceRangeMinutes <= MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES

  const applyPerformanceRange = () => {
    if (!isPerformanceRangeInputValid) {
      toast.error('统计范围必须是 1 到 1440 之间的整数分钟')
      setPerformanceRangeInput(String(manualPerformanceRangeMinutes))
      return
    }
    if (parsedPerformanceRangeMinutes === manualPerformanceRangeMinutes) return
    setManualPerformanceRangeMinutes(parsedPerformanceRangeMinutes)
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
  const performanceByChannel = useMemo(
    () => aggregateChannelMonitorPerformanceByChannel(performanceMetrics),
    [performanceMetrics]
  )
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
        cache_read_tokens: 0,
        input_tokens: 0,
        cache_utilization_rate: 0,
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
      summary.cache_read_tokens += metric.cache_read_tokens ?? 0
      summary.input_tokens += metric.input_tokens ?? 0
      summary.cache_utilization_rate =
        summary.input_tokens > 0
          ? summary.cache_read_tokens / summary.input_tokens
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
  const pageRealtimeMetadata = mergeChannelMonitorRealtimeMetadata([
    overview,
    costOverview,
    todaySuccessQuery.data?.data,
    performanceQueryActive ? performanceQuery.data?.data : undefined,
    smartScheduleSummaryResult,
    view === 'smart-schedule' ? smartScheduleResult : undefined,
  ])
  const todayProbeCost = costOverview?.today_probe_cost_cny ?? 0
  const todayGroupProbeCost = costOverview?.today_group_probe_cost_cny ?? 0
  const todayModelDetectionCost =
    costOverview?.today_model_detection_cost_cny ?? 0
  const todayBusinessCost = costOverview
    ? Math.max(
        0,
        costOverview.today_cost_cny - todayProbeCost - todayModelDetectionCost
      )
    : 0
  let costDescription = '按北京时间记录已结算成本'
  let costSecondaryDescription = '详情中可查看成本趋势与解析情况'
  if (costQuery.isError) {
    costDescription = '成本统计加载失败'
    costSecondaryDescription = '请稍后重试或手动刷新'
  } else if (
    costOverview &&
    costOverview.coverage.settled_count +
      costOverview.coverage.unresolved_count ===
      0
  ) {
    costDescription = '暂无已记录的上游请求尝试'
    costSecondaryDescription = '按北京时间统计已结算成本'
  } else if (costOverview) {
    costDescription = `业务 ${formatChannelMonitorCost(todayBusinessCost)} · 探测 ${formatChannelMonitorCost(todayProbeCost)}（分组 ${formatChannelMonitorCost(todayGroupProbeCost)}） · 模型检测 ${formatChannelMonitorCost(todayModelDetectionCost)}`
    costSecondaryDescription = `昨日 ${formatChannelMonitorCost(costOverview.yesterday_cost_cny)} · 解析率 ${formatChannelMonitorResolutionRate(
      costOverview.coverage.settled_count,
      costOverview.coverage.unresolved_count
    )}`
    if (costOverview.coverage.unresolved_count > 0) {
      costSecondaryDescription += ` · 未解析 ${costOverview.coverage.unresolved_count}`
    }
  }
  const enabledChannelCount = channels.filter(
    (channel) => channel.status === CHANNEL_STATUS.ENABLED
  ).length
  const disabledChannelCount = channels.length - enabledChannelCount
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
  } else if (query.isError && !overview) {
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
            description={`启用 ${enabledChannelCount} · 停用 ${disabledChannelCount}`}
            secondaryDescription={`New API ${newAPIChannelCount} · Sub2API ${sub2APIChannelCount} · 自定义 ${customUpstreamChannelCount}`}
            icon={Analytics01Icon}
          />
          <MonitorStatCard
            label='今日已结算成本'
            value={
              costQuery.isLoading ? (
                <Skeleton className='h-7 w-24' />
              ) : (
                formatChannelMonitorCost(costOverview?.today_cost_cny)
              )
            }
            description={costDescription}
            secondaryDescription={costSecondaryDescription}
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
            <ChannelMonitorViewTabs
              channelCount={channels.length}
              groupCount={groups.length}
              performanceModelCount={performanceModelOptions.length}
              smartSchedulePoolCount={smartScheduleSummary.poolCount}
              smartScheduleHasCriticalIssue={smartScheduleHasCriticalIssue}
              smartScheduleHasProbing={smartScheduleHasProbing}
            />

            {view !== 'smart-schedule' &&
            view !== 'status-probe' &&
            view !== 'model-detection' ? (
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
                      items={smartScheduleDisplayGroups}
                      value={activeSmartScheduleDisplayGroup || null}
                      onValueChange={(value) => {
                        if (value === null) return
                        const nextModel =
                          smartScheduleDisplayModelsByGroup.get(value)?.[0] ??
                          ''
                        saveSmartScheduleDisplaySelection({
                          group: value,
                          model: nextModel,
                        })
                      }}
                    >
                      <SelectTrigger
                        className='w-full sm:w-48'
                        aria-label='选择智能调度分组'
                        disabled={smartScheduleDisplayGroups.length === 0}
                      >
                        <SelectValue placeholder='选择分组' />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {smartScheduleDisplayGroups.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <Select
                      items={activeSmartScheduleDisplayModels.map((model) => ({
                        value: model,
                        label: model,
                      }))}
                      value={activeSmartScheduleDisplayModel || null}
                      onValueChange={(value) => {
                        if (value === null) return
                        saveSmartScheduleDisplaySelection({
                          group: activeSmartScheduleDisplayGroup,
                          model: value,
                        })
                      }}
                    >
                      <SelectTrigger
                        className='w-full sm:w-60'
                        aria-label='选择智能调度模型'
                        disabled={activeSmartScheduleDisplayModels.length === 0}
                      >
                        <SelectValue placeholder='选择模型' />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {activeSmartScheduleDisplayModels.map((model) => (
                            <SelectItem key={model} value={model}>
                              {model}
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
                        try {
                          localStorage.setItem(
                            CHANNEL_MONITOR_SORT_STORAGE_KEY,
                            value
                          )
                        } catch {}
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

                <ChannelMonitorPerformanceRangeControl
                  source={performanceRangeSource}
                  rangeMinutes={performanceRangeMinutes}
                  inputValue={performanceRangeInput}
                  inputValid={isPerformanceRangeInputValid}
                  minMinutes={MIN_CHANNEL_MONITOR_PERFORMANCE_MINUTES}
                  maxMinutes={MAX_CHANNEL_MONITOR_PERFORMANCE_MINUTES}
                  onInputChange={setPerformanceRangeInput}
                  onApply={applyPerformanceRange}
                />

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

          {view !== 'smart-schedule' &&
          view !== 'status-probe' &&
          view !== 'model-detection' ? (
            <ChannelMonitorPerformanceCoverageAlert
              coverage={performanceQuery.data?.data.metric_coverage}
              metadata={performanceQuery.data?.data}
              rangeLabel={performanceRangeLabel}
            />
          ) : null}

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
                performanceError={
                  performanceQuery.isError && performanceQuery.data == null
                }
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
              successError={
                performanceQuery.isError && performanceQuery.data == null
              }
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
              isError={
                performanceQuery.isError && performanceQuery.data == null
              }
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
          <TabsContent value='status-probe'>
            {view === 'status-probe' && (
              <Suspense
                fallback={
                  <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
                    {Array.from({ length: 6 }, (_, index) => (
                      <Skeleton key={index} className='h-[25rem] rounded-lg' />
                    ))}
                  </div>
                }
              >
                <LazyChannelStatusProbeView />
              </Suspense>
            )}
          </TabsContent>
          <TabsContent value='model-detection'>
            {view === 'model-detection' && (
              <Suspense
                fallback={
                  <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
                    {Array.from({ length: 6 }, (_, index) => (
                      <Skeleton key={index} className='h-[25rem] rounded-lg' />
                    ))}
                  </div>
                }
              >
                <LazyChannelModelDetectionView
                  overview={modelDetectionQuery.data?.data}
                  loading={modelDetectionQuery.isLoading}
                  refreshing={modelDetectionQuery.isFetching}
                  error={
                    modelDetectionQuery.isError
                      ? '模型检测数据加载失败，请稍后重试'
                      : null
                  }
                  onRefresh={() => {
                    void modelDetectionQuery.refetch()
                  }}
                />
              </Suspense>
            )}
          </TabsContent>
          <TabsContent value='smart-schedule'>
            <ChannelMonitorSmartScheduleBoard
              active={view === 'smart-schedule'}
              result={smartScheduleResult}
              channels={channels}
              groupPolicies={settings.smart_schedule_group_policies}
              groupRatios={groupRatios}
              isLoading={smartScheduleDetailQuery.isLoading}
              isError={smartScheduleDetailQuery.isError}
              selection={{
                group: activeSmartScheduleDisplayGroup,
                model: activeSmartScheduleDisplayModel,
              }}
              onSelectionChange={saveSmartScheduleDisplaySelection}
              onOpenHistory={() => {
                setSmartScheduleHistoryOpen(true)
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
        <SectionPageLayout.Title>
          <span className='flex min-w-0 flex-wrap items-center gap-2'>
            <span className='truncate'>渠道监控</span>
            <ChannelMonitorRealtimeStatus metadata={pageRealtimeMetadata} />
          </span>
        </SectionPageLayout.Title>
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
                  onMouseEnter={() => {
                    void loadChannelMonitorTaskHistoryDialog()
                  }}
                  onFocus={() => {
                    void loadChannelMonitorTaskHistoryDialog()
                  }}
                  onClick={() => setTaskHistoryOpen(true)}
                  aria-label='倍率与余额更新记录'
                >
                  <HugeiconsIcon icon={HistoryIcon} />
                </Button>
              }
            />
            <TooltipContent>倍率与余额更新记录</TooltipContent>
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
                  onClick={() => setSmartScheduleHistoryOpen(true)}
                  aria-label='智能调度执行记录'
                >
                  <HugeiconsIcon icon={WorkflowSquare06Icon} />
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
                  onClick={() => setGroupMonitorSettingsOpen(true)}
                  aria-label='分组监控设置'
                >
                  <HugeiconsIcon icon={Activity01Icon} />
                </Button>
              }
            />
            <TooltipContent>分组监控设置</TooltipContent>
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
                  onClick={() => void refreshChannelMonitor()}
                  disabled={
                    manualRefreshPending ||
                    query.isFetching ||
                    performanceQuery.isFetching ||
                    costQuery.isFetching ||
                    todaySuccessQuery.isFetching ||
                    smartScheduleSummaryQuery.isFetching ||
                    (view === 'smart-schedule' &&
                      smartScheduleDetailQuery.isFetching)
                  }
                  aria-label='刷新'
                >
                  <HugeiconsIcon
                    icon={Refresh01Icon}
                    className={
                      manualRefreshPending ? 'animate-spin' : undefined
                    }
                  />
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
          key={`${settings.auto_update_interval_minutes}:${settings.auto_update_retry_count}:${settings.upstream_request_timeout_seconds ?? DEFAULT_CHANNEL_MONITOR_UPSTREAM_REQUEST_TIMEOUT_SECONDS}:${autoUpdateConsecutiveFailureLimit}:${settings.auto_disable_on_update_failure}:${settings.auto_enable_on_cost_ratio_recovery}:${settings.auto_enable_on_balance_recovery}:${settings.cost_retention_days}:${settings.route_metric_retention_days}:${settings.duration_bucket_retention_days}:${settings.execution_detail_retention_days}:${settings.task_retention_days}:${settings.ratio_monitor_task_retention_days}:${settings.smart_schedule_task_retention_days}:${settings.smart_schedule_probe_task_retention_days}:${settings.cleanup_task_retention_days}:${settings.model_detection_task_retention_days}:${settings.channel_test_task_retention_days}:${settings.model_update_task_retention_days}:${settings.task_keep_latest_count}:${settings.ratio_history_retention_days}:${settings.status_probe_history_retention_days}:${settings.group_monitor_retention_days}:${settings.model_detection_retention_days}:${settings.email_notification_enabled}:${settings.notification_email}:${settings.email_notification_types.join(',')}:${settings.error_message_mapping}:${settings.error_message_keywords}:${settings.probe_response_enabled}:${settings.probe_response_match_input ?? DEFAULT_PROBE_RESPONSE_MATCH_INPUT}:${settings.probe_response_text ?? DEFAULT_PROBE_RESPONSE_TEXT}:${settings.probe_response_min_delay_ms ?? DEFAULT_PROBE_RESPONSE_MIN_DELAY_MS}:${settings.probe_response_max_delay_ms ?? DEFAULT_PROBE_RESPONSE_MAX_DELAY_MS}:${settings.probe_response_input_tokens ?? DEFAULT_PROBE_RESPONSE_INPUT_TOKENS}:${settings.probe_response_cache_write_tokens ?? DEFAULT_PROBE_RESPONSE_CACHE_WRITE_TOKENS}:${settings.probe_response_cached_tokens ?? DEFAULT_PROBE_RESPONSE_CACHED_TOKENS}:${settings.probe_response_output_tokens ?? DEFAULT_PROBE_RESPONSE_OUTPUT_TOKENS}`}
          settings={settings}
          open
          onOpenChange={setSettingsOpen}
        />
      )}
      {groupMonitorSettingsOpen && (
        <ChannelGroupMonitorSettingsSheet
          data={groupMonitorSettingsQuery.data?.data}
          open
          onOpenChange={setGroupMonitorSettingsOpen}
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
        <Suspense fallback={null}>
          <LazyChannelMonitorTaskHistoryDialog
            open
            onOpenChange={setTaskHistoryOpen}
          />
        </Suspense>
      )}
      {smartScheduleHistoryOpen && (
        <Suspense fallback={null}>
          <LazyChannelMonitorSmartScheduleExecutionDialog
            open
            onOpenChange={setSmartScheduleHistoryOpen}
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
          rangeMinutes={manualPerformanceRangeMinutes}
          rangeLabel={manualPerformanceRangeLabel}
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
  secondaryDescription?: string
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
        'h-full min-h-0 gap-0 py-0 sm:h-32',
        interactive &&
          'cursor-pointer transition-colors hover:ring-2 hover:ring-ring/30 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none'
      )}
    >
      <CardHeader className='h-full !grid-rows-[auto_auto_1fr] gap-1 py-3'>
        <CardDescription>{props.label}</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{props.value}</CardTitle>
        <CardAction>
          <span className='bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-md'>
            <HugeiconsIcon icon={props.icon} />
          </span>
        </CardAction>
        <CardDescription className='col-span-full min-w-0 self-end text-xs leading-4'>
          <span className='block truncate' title={props.description}>
            {props.description}
          </span>
          {props.secondaryDescription ? (
            <span className='block truncate' title={props.secondaryDescription}>
              {props.secondaryDescription}
            </span>
          ) : null}
        </CardDescription>
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
