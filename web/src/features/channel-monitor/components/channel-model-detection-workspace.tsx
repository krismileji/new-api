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
  Cancel01Icon,
  FingerPrintScanIcon,
  PauseIcon,
  PlayIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'
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
import { Spinner } from '@/components/ui/spinner'
import { channelsQueryKeys } from '@/features/channels/lib/channel-actions'

import { runChannelMonitorBatchExecution } from '../lib/batch-execution'
import {
  channelModelDetectionPresetLabel,
  isChannelModelDetectionRunActive,
} from '../lib/model-detection'
import {
  cancelChannelModelDetectionRun,
  getChannelModelDetectionRun,
  getChannelModelDetectionRuns,
  isChannelModelDetectionConfigConflict,
  isChannelModelDetectionInfrastructureConflict,
  startChannelModelDetectionRun,
  updateChannelModelDetectionConfig,
} from '../lib/model-detection-channel-api'
import { channelModelDetectionRequestErrorMessage } from '../lib/model-detection-settings-api'
import {
  CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
  getChannelMonitorActiveRefetchInterval,
} from '../lib/query-options'
import type {
  ChannelModelDetectionChannel,
  ChannelModelDetectionHistoryQuery,
  ChannelModelDetectionPreset,
  ChannelModelDetectionRunStatus,
} from '../types-model-detection'
import { ChannelModelDetectionConfigSheet } from './channel-model-detection-config-sheet'
import { ChannelModelDetectionHistorySheet } from './channel-model-detection-history-sheet'
import { ChannelModelDetectionRunDetailSheet } from './channel-model-detection-run-detail-sheet'
import { ChannelModelDetectionRunDialog } from './channel-model-detection-run-dialog'
import { ChannelModelDetectionSettingsSheet } from './channel-model-detection-settings-sheet'
import {
  ChannelModelDetectionView,
  type ChannelModelDetectionViewProps,
} from './channel-model-detection-view'

const OVERVIEW_QUERY_KEY = [
  'channel-monitor',
  'model-detection',
  'overview',
] as const

const DEFAULT_HISTORY_QUERY: ChannelModelDetectionHistoryQuery = {
  page: 1,
  page_size: 20,
  trigger: '',
  status: '',
  model: '',
  outcome: '',
}

export type ChannelModelDetectionWorkspaceProps = ChannelModelDetectionViewProps

export function ChannelModelDetectionWorkspace(
  props: ChannelModelDetectionWorkspaceProps
) {
  const queryClient = useQueryClient()
  const onActionComplete = props.onActionComplete
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [configChannelId, setConfigChannelId] = useState<number | null>(null)
  const [runChannelId, setRunChannelId] = useState<number | null>(null)
  const [historyChannelId, setHistoryChannelId] = useState<number | null>(null)
  const [cancelChannelId, setCancelChannelId] = useState<number | null>(null)
  const [bulkAction, setBulkAction] = useState<
    'run' | 'enable' | 'pause' | null
  >(null)
  const [detailRunId, setDetailRunId] = useState<string | null>(null)
  const [historyQueryInput, setHistoryQueryInput] = useState(
    DEFAULT_HISTORY_QUERY
  )

  const channelsById = useMemo(
    () =>
      new Map(
        (props.overview?.channels ?? []).map((channel) => [channel.id, channel])
      ),
    [props.overview?.channels]
  )
  const configChannel = configChannelId
    ? (channelsById.get(configChannelId) ?? null)
    : null
  const runChannel = runChannelId
    ? (channelsById.get(runChannelId) ?? null)
    : null
  const historyChannel = historyChannelId
    ? (channelsById.get(historyChannelId) ?? null)
    : null
  const cancelChannel = cancelChannelId
    ? (channelsById.get(cancelChannelId) ?? null)
    : null
  const scheduledChannels = useMemo(
    () =>
      (props.overview?.channels ?? []).filter(
        (channel) => channel.config?.schedule_enabled === true
      ),
    [props.overview?.channels]
  )
  const runnableScheduledChannels = useMemo(
    () => scheduledChannels.filter((channel) => !channel.active_run),
    [scheduledChannels]
  )
  const pausedChannels = useMemo(
    () =>
      (props.overview?.channels ?? []).filter(
        (channel) =>
          channel.config?.schedule_enabled === false &&
          channel.targets.some((target) => target.enabled)
      ),
    [props.overview?.channels]
  )

  const refreshOverview = useCallback(() => {
    if (onActionComplete) return onActionComplete()
    return queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
  }, [onActionComplete, queryClient])
  const refreshChannelStatus = useCallback(
    () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() }),
        queryClient.invalidateQueries({
          queryKey: ['channel-monitor', 'status-probe'],
        }),
        refreshOverview(),
      ]).then(() => undefined),
    [queryClient, refreshOverview]
  )
  const refreshChannelHistory = useCallback(
    (channelId: number) => {
      void queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'history', channelId],
      })
    },
    [queryClient]
  )

  const historyQuery = useQuery({
    queryKey: [
      'channel-monitor',
      'model-detection',
      'history',
      historyChannelId,
      historyQueryInput,
    ],
    queryFn: () =>
      getChannelModelDetectionRuns(historyChannelId ?? 0, historyQueryInput),
    enabled: historyChannelId != null,
    staleTime: 0,
    ...CHANNEL_MONITOR_MANUAL_REFRESH_QUERY_OPTIONS,
    refetchOnMount: 'always',
    refetchInterval: () =>
      getChannelMonitorActiveRefetchInterval(
        historyChannel?.active_run != null &&
          isChannelModelDetectionRunActive(historyChannel.active_run.status)
      ),
  })

  const scheduleMutation = useMutation({
    mutationFn: (channel: ChannelModelDetectionChannel) => {
      if (!channel.config) throw new Error('渠道尚未配置模型检测目标')
      return updateChannelModelDetectionConfig(channel.id, {
        schedule_enabled: !channel.config.schedule_enabled,
        targets: [...channel.targets]
          .filter((target) => target.enabled)
          .sort((left, right) => left.position - right.position)
          .map((target) => ({
            target_key: target.target_key,
            request_model: target.request_model,
            claimed_model: target.claimed_model,
          })),
        revision: channel.config.revision,
      })
    },
    onSuccess: (saved, channel) => {
      toast.success(
        saved.schedule_enabled
          ? '该渠道已参加统一定时检测'
          : '该渠道已退出统一定时检测'
      )
      refreshOverview()
      refreshChannelHistory(channel.id)
    },
    onError: (error) => {
      const prefix = isChannelModelDetectionConfigConflict(error)
        ? '渠道配置已变化，请刷新后重试：'
        : '更新渠道定时检测失败：'
      toast.error(`${prefix}${channelModelDetectionRequestErrorMessage(error)}`)
      refreshOverview()
    },
  })

  const bulkMutation = useMutation({
    mutationFn: (variables: {
      channels: ChannelModelDetectionChannel[]
      enabled: boolean
    }) =>
      Promise.allSettled(
        variables.channels.map((channel) => {
          if (!channel.config) {
            throw new Error('渠道尚未配置模型检测目标')
          }
          return updateChannelModelDetectionConfig(channel.id, {
            schedule_enabled: variables.enabled,
            targets: [...channel.targets]
              .filter((target) => target.enabled)
              .sort((left, right) => left.position - right.position)
              .map((target) => ({
                target_key: target.target_key,
                request_model: target.request_model,
                claimed_model: target.claimed_model,
              })),
            revision: channel.config.revision,
          })
        })
      ),
    onSuccess: (results, variables) => {
      const updatedCount = results.filter(
        (result) => result.status === 'fulfilled'
      ).length
      const failedCount = results.length - updatedCount
      const actionLabel = variables.enabled ? '启用' : '暂停'
      setBulkAction(null)
      refreshOverview()
      if (failedCount === 0) {
        toast.success(`已${actionLabel} ${updatedCount} 个渠道的模型定时检测`)
      } else if (updatedCount === 0) {
        toast.error(
          `${actionLabel}失败，${failedCount} 个渠道未更新，请刷新后重试`
        )
      } else {
        toast.error(
          `已${actionLabel} ${updatedCount} 个渠道，${failedCount} 个渠道失败，请刷新后重试`
        )
      }
    },
  })
  const bulkRunMutation = useMutation({
    mutationFn: (variables: {
      channels: ChannelModelDetectionChannel[]
      preset: ChannelModelDetectionPreset
      skippedCount: number
    }) =>
      runChannelMonitorBatchExecution(variables.channels, (channel) =>
        startChannelModelDetectionRun(channel.id, {
          preset: variables.preset,
          confirm_high_cost: variables.preset === 'high',
        })
      ),
    onSuccess: (results, variables) => {
      const submittedResults = results.filter(
        (result) => result.status === 'fulfilled'
      )
      const submittedCount = submittedResults.length
      const failedCount = results.length - submittedCount
      setBulkAction(null)
      refreshOverview()
      for (const result of submittedResults) {
        refreshChannelHistory(result.item.id)
      }

      const skippedMessage = variables.skippedCount
        ? `，跳过 ${variables.skippedCount} 个已有活动轮次的渠道`
        : ''
      if (failedCount === 0) {
        toast.success(
          `已按${channelModelDetectionPresetLabel(variables.preset)}为 ${submittedCount} 个渠道提交模型检测${skippedMessage}`
        )
      } else if (submittedCount === 0) {
        toast.error(`批量提交失败，${failedCount} 个渠道未进入模型检测队列`)
      } else {
        toast.error(
          `已提交 ${submittedCount} 个渠道，${failedCount} 个渠道失败${skippedMessage}`
        )
      }
    },
  })

  const cancelMutation = useMutation({
    mutationFn: (variables: { channelId: number; runId: string }) =>
      cancelChannelModelDetectionRun(variables.runId),
    onSuccess: (result, variables) => {
      toast.success(
        result.status === 'canceled' ? '模型检测任务已取消' : '取消请求已提交'
      )
      setCancelChannelId(null)
      refreshOverview()
      refreshChannelHistory(variables.channelId)
      void queryClient.invalidateQueries({
        queryKey: ['channel-monitor', 'model-detection', 'run', result.run_id],
      })
    },
    onError: (error) => {
      const prefix = isChannelModelDetectionInfrastructureConflict(error)
        ? '取消状态发生冲突：'
        : '取消模型检测失败：'
      toast.error(`${prefix}${channelModelDetectionRequestErrorMessage(error)}`)
      if (isChannelModelDetectionInfrastructureConflict(error)) {
        setCancelChannelId(null)
      }
      refreshOverview()
    },
  })

  let actionPendingChannelId: number | null = null
  if (scheduleMutation.isPending) {
    actionPendingChannelId = scheduleMutation.variables?.id ?? null
  } else if (cancelMutation.isPending) {
    actionPendingChannelId = cancelMutation.variables?.channelId ?? null
  }
  const scheduledPreset = props.overview?.settings.scheduled_preset ?? 'medium'
  const bulkActionIsRun = bulkAction === 'run'
  const bulkActionEnabled = bulkAction === 'enable'
  let bulkActionChannels = scheduledChannels
  if (bulkActionIsRun) {
    bulkActionChannels = runnableScheduledChannels
  } else if (bulkActionEnabled) {
    bulkActionChannels = pausedChannels
  }
  const bulkActionMutationPending = bulkActionIsRun
    ? bulkRunMutation.isPending
    : bulkMutation.isPending
  let bulkDialogTitle = '暂停全部模型定时检测？'
  let bulkDialogDescription = `将让全部 ${bulkActionChannels.length} 个已参加统一定时检测的渠道退出定时检测，不受当前筛选条件影响。`
  let bulkDialogNote = '已经运行的检测轮次不会被取消。'
  let bulkConfirmLabel = '确认暂停'
  if (bulkActionIsRun) {
    bulkDialogTitle = '批量执行已启用模型检测？'
    bulkDialogDescription = `将按统一设置的${channelModelDetectionPresetLabel(scheduledPreset)}，为全部 ${bulkActionChannels.length} 个已启用且当前空闲的渠道创建手动检测轮次，不受当前筛选条件影响。`
    const skippedCount = scheduledChannels.length - bulkActionChannels.length
    bulkDialogNote = skippedCount
      ? `另有 ${skippedCount} 个渠道已有活动轮次，将自动跳过。`
      : ''
    if (scheduledPreset === 'high') {
      bulkDialogNote = `${bulkDialogNote ? `${bulkDialogNote} ` : ''}高档请求量和成本更高，本次确认将同时作为高成本确认。`
    }
    bulkConfirmLabel = '确认执行'
  } else if (bulkActionEnabled) {
    bulkDialogTitle = '启用全部模型定时检测？'
    bulkDialogDescription = `将让全部 ${bulkActionChannels.length} 个已配置但暂停的渠道参加统一定时检测，不受当前筛选条件影响。`
    bulkDialogNote = ''
    bulkConfirmLabel = '确认启用'
  }
  let bulkActionIcon = PauseIcon
  if (bulkActionIsRun) {
    bulkActionIcon = FingerPrintScanIcon
  } else if (bulkActionEnabled) {
    bulkActionIcon = PlayIcon
  }
  let pendingBulkAction: 'run' | 'enable' | 'pause' | null = null
  if (bulkRunMutation.isPending) {
    pendingBulkAction = 'run'
  } else if (bulkMutation.isPending) {
    pendingBulkAction = bulkAction
  }

  function handleSurfaceClose(setOpen: (value: null) => void) {
    setOpen(null)
    refreshOverview()
  }

  function handleTerminalRun(
    runId: string,
    _status: ChannelModelDetectionRunStatus
  ) {
    refreshOverview()
    if (historyChannelId != null) refreshChannelHistory(historyChannelId)
    void queryClient.invalidateQueries({
      queryKey: ['channel-monitor', 'model-detection', 'run', runId],
      refetchType: 'none',
    })
  }

  const settingsSurface = (
    <ChannelModelDetectionSettingsSheet
      open={settingsOpen}
      onOpenChange={(open) => {
        setSettingsOpen(open)
        if (!open) refreshOverview()
      }}
      onSaved={refreshOverview}
    />
  )

  const channelSurface = (
    <>
      <ChannelModelDetectionConfigSheet
        channel={configChannel}
        detectorURLConfigured={Boolean(
          props.overview?.settings.detector_url_configured
        )}
        open={configChannelId != null}
        onOpenChange={(open) => {
          if (!open) handleSurfaceClose(setConfigChannelId)
        }}
        onSaved={() => {
          refreshOverview()
          if (configChannelId != null) refreshChannelHistory(configChannelId)
        }}
        onRefreshChannel={refreshOverview}
      />
      <ChannelModelDetectionRunDialog
        channel={runChannel}
        open={runChannelId != null}
        onOpenChange={(open) => {
          if (!open) handleSurfaceClose(setRunChannelId)
        }}
        onRunAccepted={(run, channelId) => {
          setDetailRunId(run.run_id)
          refreshOverview()
          refreshChannelHistory(channelId)
        }}
        onRefreshRequested={refreshOverview}
      />
      <AlertDialog
        open={bulkAction != null}
        onOpenChange={(open) => {
          if (!open && !bulkActionMutationPending) setBulkAction(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{bulkDialogTitle}</AlertDialogTitle>
            <AlertDialogDescription className='space-y-2'>
              <span className='block'>{bulkDialogDescription}</span>
              {bulkDialogNote && (
                <span className='block'>{bulkDialogNote}</span>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkActionMutationPending}>
              返回
            </AlertDialogCancel>
            <AlertDialogAction
              variant={bulkAction === 'pause' ? 'destructive' : 'default'}
              disabled={
                bulkActionChannels.length === 0 || bulkActionMutationPending
              }
              onClick={() => {
                if (bulkActionIsRun) {
                  bulkRunMutation.mutate({
                    channels: bulkActionChannels,
                    preset: scheduledPreset,
                    skippedCount:
                      scheduledChannels.length - bulkActionChannels.length,
                  })
                  return
                }
                bulkMutation.mutate({
                  channels: bulkActionChannels,
                  enabled: bulkActionEnabled,
                })
              }}
            >
              {bulkActionMutationPending ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={bulkActionIcon} data-icon='inline-start' />
              )}
              {bulkConfirmLabel}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog
        open={cancelChannelId != null}
        onOpenChange={(open) => {
          if (!open && !cancelMutation.isPending) {
            setCancelChannelId(null)
            refreshOverview()
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>取消当前模型检测？</AlertDialogTitle>
            <AlertDialogDescription>
              {cancelChannel?.active_run
                ? `将请求取消 ${cancelChannel.name} 的轮次 ${cancelChannel.active_run.run_id}。已经发出的上游请求仍会按实际 Usage 记录成本。`
                : '当前活动轮次已变化，请关闭后刷新状态。'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancelMutation.isPending}>
              返回
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={!cancelChannel?.active_run || cancelMutation.isPending}
              onClick={() => {
                if (!cancelChannel?.active_run) return
                cancelMutation.mutate({
                  channelId: cancelChannel.id,
                  runId: cancelChannel.active_run.run_id,
                })
              }}
            >
              {cancelMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon icon={Cancel01Icon} data-icon='inline-start' />
              )}
              确认取消
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )

  const historySurface = (
    <>
      {historyChannel ? (
        <ChannelModelDetectionHistorySheet
          channel={historyChannel}
          open={historyChannelId != null}
          query={historyQueryInput}
          data={historyQuery.data}
          loading={historyQuery.isPending}
          refreshing={historyQuery.isFetching}
          error={
            historyQuery.isError
              ? channelModelDetectionRequestErrorMessage(historyQuery.error)
              : null
          }
          onOpenChange={(open) => {
            if (!open) {
              setHistoryChannelId(null)
              setDetailRunId(null)
              refreshOverview()
            }
          }}
          onQueryChange={setHistoryQueryInput}
          onRefresh={() => void historyQuery.refetch()}
          onLoadRunDetail={getChannelModelDetectionRun}
          onOpenRun={(run) => setDetailRunId(run.run_id)}
        />
      ) : null}
      <ChannelModelDetectionRunDetailSheet
        runId={detailRunId}
        open={detailRunId != null}
        onOpenChange={(open) => {
          if (!open) {
            setDetailRunId(null)
            refreshOverview()
          }
        }}
        onTerminal={handleTerminalRun}
      />
    </>
  )

  return (
    <ChannelModelDetectionView
      {...props}
      channelQueryClient={queryClient}
      actionPendingChannelId={actionPendingChannelId}
      actionPendingAll={bulkMutation.isPending || bulkRunMutation.isPending}
      bulkActionPending={pendingBulkAction}
      bulkActionDisabled={
        scheduleMutation.isPending ||
        cancelMutation.isPending ||
        bulkRunMutation.isPending
      }
      onRunEnabled={() => setBulkAction('run')}
      onEnableAll={() => setBulkAction('enable')}
      onPauseAll={() => setBulkAction('pause')}
      onOpenSettings={() => setSettingsOpen(true)}
      onOpenHistory={(channel) => {
        setHistoryQueryInput(DEFAULT_HISTORY_QUERY)
        setHistoryChannelId(channel.id)
      }}
      onOpenConfig={(channel) => setConfigChannelId(channel.id)}
      onOpenManualRun={(channel) => setRunChannelId(channel.id)}
      onCancelRun={(channel) => setCancelChannelId(channel.id)}
      onToggleSchedule={(channel) => scheduleMutation.mutate(channel)}
      onChannelStatusChanged={refreshChannelStatus}
      settingsSurface={settingsSurface}
      channelSurface={channelSurface}
      historySurface={historySurface}
    />
  )
}
